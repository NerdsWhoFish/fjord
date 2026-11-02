package navigation

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	"golang.org/x/mod/sumdb/dirhash"
	"golang.org/x/sync/errgroup"
)

const (
	maxGoModules       = 256
	maxGoArtifactBytes = 64 << 20
	maxGoDownloadBytes = 256 << 20
	maxGoExpandedBytes = 512 << 20
)

type goModule struct {
	Path    string
	Version string
}

type goModuleFile struct {
	Require []goModule
	Replace []struct {
		Old goModule
		New goModule
	}
}

// Preparation uses a file proxy and a synthetic module: repository directives
// never run in a process with network access or access to host credentials.
func prepareGoDependencies(ctx context.Context, dir, root, goBin, goRoot string, env []string) (cache string, err error) {
	ctx, span := otel.Tracer("fjord/navigation").Start(ctx, "navigation.go.dependencies")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, "Go dependency preparation failed")
		}
		span.End()
	}()
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	metadata, err := inspectGoModule(ctx, dir, root, goBin, goRoot, env)
	if err != nil {
		return "", err
	}
	modules, err := requiredGoModules(metadata, filepath.Join(dir, "source"), root)
	if err != nil {
		return "", err
	}
	if len(modules) == 0 {
		return "", nil
	}
	if info, err := os.Stat(filepath.Join(root, "vendor", "modules.txt")); err == nil && info.Mode().IsRegular() {
		return "", nil
	}
	if len(modules) > maxGoModules {
		return "", errors.New("Go dependency preparation exceeds the 256-module limit; use vendored dependencies")
	}
	sum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil || len(sum) > 2<<20 {
		return "", errors.New("Go dependencies require a committed go.sum or vendored dependencies; Fjord does not resolve unpinned modules")
	}
	pinned := pinnedGoModules(string(sum))
	for _, module := range modules {
		if pinned[module.Path+"@"+module.Version+"/go.mod"] == "" {
			return "", errors.New("Go dependencies are missing pinned checksums; commit the go.mod and go.sum produced by go mod tidy, or use vendored dependencies")
		}
	}
	key := sha256.Sum256(append([]byte(root), sum...))
	cache = filepath.Join(dir, "modules-"+hex.EncodeToString(key[:8]))
	if info, statErr := os.Stat(cache); statErr == nil && info.IsDir() {
		return cache, nil
	}
	stage, err := os.MkdirTemp(filepath.Join(dir, "go"), "prepare-")
	if err != nil {
		return "", errors.New("cannot create Go dependency preparation directory")
	}
	defer removeGoPreparation(stage)
	proxy := filepath.Join(stage, "proxy")
	moduleCache := filepath.Join(stage, "modules")
	if err := os.MkdirAll(proxy, 0700); err != nil {
		return "", errors.New("cannot create Go module proxy cache")
	}
	if err := os.WriteFile(filepath.Join(stage, "go.mod"), []byte("module fjord.invalid/dependency-preparation\n\ngo 1.24.0\n"), 0600); err != nil {
		return "", errors.New("cannot create Go dependency manifest")
	}
	if err := os.WriteFile(filepath.Join(stage, "go.sum"), sum, 0600); err != nil {
		return "", errors.New("cannot create Go dependency checksums")
	}
	existingBytes, existingFiles, err := goWorkspaceCacheUsage(dir)
	if err != nil {
		return "", err
	}
	fetcher := goArtifactFetcher{
		client: goProxyClient(), operatorCache: os.Getenv("FJORD_GO_MODULE_CACHE"),
		expanded: existingBytes, files: existingFiles,
	}
	defer fetcher.client.CloseIdleConnections()
	args := []string{"mod", "download"}
	group, fetchCtx := errgroup.WithContext(ctx)
	group.SetLimit(3)
	for _, module := range modules {
		key := module.Path + "@" + module.Version
		extensions := []string{".info", ".mod"}
		if pinned[key] != "" {
			extensions = append(extensions, ".zip")
			args = append(args, key)
		}
		group.Go(func() error {
			for _, extension := range extensions {
				version, _ := moduleVersionPath(module.Version)
				artifact := escapeGoModule(module.Path) + "/@v/" + version + extension
				body, err := fetcher.fetch(fetchCtx, artifact, extension == ".zip")
				if err != nil {
					return err
				}
				if extension == ".mod" {
					hash, err := dirhash.Hash1([]string{"go.mod"}, func(string) (io.ReadCloser, error) {
						return io.NopCloser(bytes.NewReader(body)), nil
					})
					if err != nil || hash != pinned[key+"/go.mod"] {
						return goPackageError("checksum mismatch")
					}
				}
				if extension == ".info" {
					var info struct{ Version string }
					if json.Unmarshal(body, &info) != nil || info.Version != module.Version {
						return errors.New("Go module proxy returned inconsistent version metadata")
					}
				}
				name := filepath.Join(proxy, filepath.FromSlash(artifact))
				if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
					return errors.New("cannot create Go module proxy directory")
				}
				if err := os.WriteFile(name, body, 0600); err != nil {
					return errors.New("cannot store Go module artifact")
				}
				if extension != ".zip" {
					cached := filepath.Join(moduleCache, "cache", "download", filepath.FromSlash(artifact))
					if err := os.MkdirAll(filepath.Dir(cached), 0700); err != nil {
						return errors.New("cannot create verified Go metadata cache")
					}
					if err := os.WriteFile(cached, body, 0600); err != nil {
						return errors.New("cannot store verified Go metadata")
					}
				}
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return "", err
	}
	span.SetAttributes(attribute.Int("dependency.count", len(modules)), attribute.Int64("dependency.download_bytes", fetcher.transferred))
	downloadEnv := replaceGoEnv(env, map[string]string{
		"GOMODCACHE": moduleCache,
		"GOPROXY":    (&url.URL{Scheme: "file", Path: proxy}).String(),
		"GOSUMDB":    "off",
		"GOPRIVATE":  "",
		"GONOSUMDB":  "",
		"GONOPROXY":  "",
	})
	if len(args) > 2 {
		if _, err := runGoPreparation(ctx, dir, stage, goBin, goRoot, downloadEnv, args...); err != nil {
			return "", err
		}
	}
	// Go marks extracted module directories read-only. The sandbox supplies the
	// read-only boundary; writable host directories allow workspace expiry cleanup.
	if err := writableGoDirectories(moduleCache); err != nil {
		return "", errors.New("cannot finalize Go module directory permissions")
	}
	cacheBytes, cacheFiles, err := goCacheUsage([]string{moduleCache})
	if err != nil || cacheBytes+existingBytes > maxGoExpandedBytes || cacheFiles+existingFiles > 100000 {
		return "", errors.New("Go dependency caches exceed this revision's storage limit; use vendored dependencies")
	}
	if err := os.Rename(moduleCache, cache); err != nil {
		return "", errors.New("cannot finalize verified Go module cache")
	}
	return cache, nil
}

func goWorkspaceCacheUsage(dir string) (uint64, int, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "modules-*"))
	if err != nil {
		return 0, 0, err
	}
	return goCacheUsage(paths)
}

func goCacheUsage(paths []string) (uint64, int, error) {
	var size uint64
	files := 0
	for _, path := range paths {
		err := filepath.WalkDir(path, func(name string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			size += uint64(info.Size())
			files++
			if size > maxGoExpandedBytes || files > 100000 {
				return errors.New("Go dependency caches exceed this revision's storage limit; use vendored dependencies")
			}
			return nil
		})
		if err != nil {
			return 0, 0, err
		}
	}
	return size, files, nil
}

func writableGoDirectories(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.Chmod(path, 0700)
		}
		return nil
	})
}

func removeGoPreparation(root string) {
	_ = writableGoDirectories(root)
	_ = os.RemoveAll(root)
}

func inspectGoModule(ctx context.Context, dir, root, goBin, goRoot string, env []string) (goModuleFile, error) {
	var module goModuleFile
	out, err := runGoPreparation(ctx, dir, root, goBin, goRoot, env, "mod", "edit", "-json")
	if err != nil {
		return module, err
	}
	if json.Unmarshal(out, &module) != nil {
		return module, errors.New("invalid Go module metadata")
	}
	return module, nil
}

func requiredGoModules(metadata goModuleFile, source, root string) ([]goModule, error) {
	for _, replacement := range metadata.Replace {
		if replacement.New.Version != "" {
			continue
		}
		path := replacement.New.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, filepath.FromSlash(path))
		}
		relative, err := filepath.Rel(source, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, errors.New("Go local replacement escapes the PR source; keep local replacements inside the repository")
		}
	}
	unique := make(map[string]goModule)
	for _, module := range metadata.Require {
		selected := module
		for _, replacement := range metadata.Replace {
			if replacement.Old.Path == module.Path && replacement.Old.Version == "" {
				selected = replacement.New
				break
			}
		}
		// Go gives an exact-version replacement priority over the wildcard,
		// independently of their declaration order.
		for _, replacement := range metadata.Replace {
			if replacement.Old.Path == module.Path && replacement.Old.Version == module.Version {
				selected = replacement.New
				break
			}
		}
		module = selected
		if module.Version == "" {
			continue
		}
		if !safeGoModule(module) {
			return nil, errors.New("Go module contains an unsupported dependency path or version")
		}
		unique[module.Path+"@"+module.Version] = module
	}
	modules := make([]goModule, 0, len(unique))
	for _, module := range unique {
		modules = append(modules, module)
	}
	sort.Slice(modules, func(i, j int) bool {
		return modules[i].Path+"@"+modules[i].Version < modules[j].Path+"@"+modules[j].Version
	})
	return modules, nil
}

func safeGoModule(dep goModule) bool {
	return len(dep.Path) <= 1024 && module.CheckPath(dep.Path) == nil && semver.IsValid(dep.Version)
}

func moduleVersionPath(version string) (string, error) { return module.EscapeVersion(version) }

func escapeGoModule(value string) string {
	escaped, _ := module.EscapePath(value)
	return escaped
}

func pinnedGoModules(sum string) map[string]string {
	pinned := make(map[string]string)
	for _, line := range strings.Split(sum, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && safeGoModule(goModule{fields[0], strings.TrimSuffix(fields[1], "/go.mod")}) && strings.HasPrefix(fields[2], "h1:") && len(fields[2]) == 47 {
			pinned[fields[0]+"@"+fields[1]] = fields[2]
		}
	}
	return pinned
}

func replaceGoEnv(env []string, values map[string]string) []string {
	result := make([]string, 0, len(env)+len(values))
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if _, replaced := values[key]; !replaced {
			result = append(result, entry)
		}
	}
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	return result
}

func runGoPreparation(ctx context.Context, dir, root, goBin, goRoot string, env []string, args ...string) ([]byte, error) {
	cmd, cancel, err := sandboxCommand(dir, root, goBin, goRoot, env, args...)
	if err != nil {
		return nil, err
	}
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	var output, stderr boundedBuffer
	cmd.Stdout, cmd.Stderr = &output, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, errors.New("Go dependency preparation timed out; vendor dependencies or provide FJORD_GO_MODULE_CACHE")
		}
		return nil, goPackageError(stderr.String())
	}
	return output.Bytes(), nil
}

type goArtifactFetcher struct {
	mu            sync.Mutex
	client        *http.Client
	operatorCache string
	transferred   int64
	expanded      uint64
	files         int
}

var errGoDownloadLimit = errors.New("Go dependency downloads reach the 256 MiB limit; use vendored dependencies")

type goBudgetReader struct {
	reader io.Reader
	budget *goArtifactFetcher
}

func (r goBudgetReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	r.budget.mu.Lock()
	remaining := maxGoDownloadBytes - r.budget.transferred
	if remaining <= 0 {
		r.budget.mu.Unlock()
		return 0, errGoDownloadLimit
	}
	reserved := min(int64(len(p)), remaining, 32<<10)
	r.budget.transferred += reserved
	r.budget.mu.Unlock()
	n, err := r.reader.Read(p[:reserved])
	r.budget.mu.Lock()
	r.budget.transferred -= reserved - int64(n)
	r.budget.mu.Unlock()
	return n, err
}

func goProxyClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			Proxy: nil, TLSHandshakeTimeout: 5 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second, MaxConnsPerHost: 2,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 3 || req.URL.Scheme != "https" || req.URL.User != nil || req.URL.Port() != "" ||
				(req.URL.Host != "proxy.golang.org" && req.URL.Host != "storage.googleapis.com") {
				return errors.New("unapproved Go module proxy redirect")
			}
			return nil
		},
	}
}

func (f *goArtifactFetcher) fetch(ctx context.Context, artifact string, isZip bool) (result []byte, resultErr error) {
	var reader io.ReadCloser
	if f.operatorCache != "" {
		cache, err := filepath.EvalSymlinks(f.operatorCache)
		if err != nil {
			return nil, errors.New("FJORD_GO_MODULE_CACHE is unavailable")
		}
		name, err := filepath.EvalSymlinks(filepath.Join(cache, "cache", "download", filepath.FromSlash(artifact)))
		if err == nil {
			rel, err := filepath.Rel(cache, name)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return nil, errors.New("operator Go module artifact escapes FJORD_GO_MODULE_CACHE")
			}
			info, err := os.Stat(name)
			if err != nil || !info.Mode().IsRegular() {
				return nil, errors.New("operator Go module artifact must be a regular file")
			}
			file, err := os.Open(name)
			if err != nil {
				return nil, errors.New("cannot read operator Go module artifact")
			}
			reader = file
		} else {
			return nil, errors.New("a pinned Go module artifact is missing from FJORD_GO_MODULE_CACHE; populate its cache/download directory or vendor dependencies")
		}
	}
	if reader == nil {
		fetchCtx, span := otel.Tracer("fjord/navigation").Start(ctx, "navigation.go.proxy.fetch", trace.WithSpanKind(trace.SpanKindClient))
		span.SetAttributes(attribute.String("server.address", "proxy.golang.org"), attribute.String("http.request.method", "GET"))
		defer func() {
			if resultErr != nil {
				span.SetStatus(codes.Error, "Go module artifact fetch failed")
			}
			span.End()
		}()
		request, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, "https://proxy.golang.org/"+artifact, nil)
		if err != nil {
			return nil, errors.New("invalid Go module artifact")
		}
		response, err := f.client.Do(request)
		if err != nil {
			return nil, errors.New("Go module proxy is unavailable or preparation timed out; provide FJORD_GO_MODULE_CACHE or vendor dependencies")
		}
		span.SetAttributes(attribute.Int("http.response.status_code", response.StatusCode))
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return nil, errors.New("a pinned Go dependency is unavailable from the public proxy; private modules require FJORD_GO_MODULE_CACHE or vendored dependencies")
		}
		reader = response.Body
	}
	defer reader.Close()
	limit := int64(1 << 20)
	if isZip {
		limit = maxGoArtifactBytes
	}
	body, err := io.ReadAll(io.LimitReader(goBudgetReader{reader: reader, budget: f}, limit+1))
	if errors.Is(err, errGoDownloadLimit) {
		return nil, err
	}
	if err != nil || int64(len(body)) > limit {
		return nil, errors.New("Go module artifact exceeds preparation limits")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if isZip {
		archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		if err != nil {
			return nil, errors.New("Go module proxy returned an invalid archive")
		}
		for _, entry := range archive.File {
			if entry.UncompressedSize64 > maxGoExpandedBytes-f.expanded {
				return nil, errors.New("Go dependency expansion exceeds the 512 MiB limit; use vendored dependencies")
			}
			f.expanded += entry.UncompressedSize64
			f.files++
			if f.files > 100000 {
				return nil, errors.New("Go dependency expansion exceeds the file-count limit")
			}
		}
	}
	return body, nil
}

var (
	errGoModuleMetadata          = errors.New("Go module metadata is incomplete; commit the go.mod and go.sum produced by go mod tidy")
	errGoDependenciesUnavailable = errors.New("Go dependencies are unavailable in the isolated revision; commit complete go.mod/go.sum, provide FJORD_GO_MODULE_CACHE for private modules, or use vendored dependencies")
)

func goGraphError(root string, err error) error {
	if !errors.Is(err, errGoModuleMetadata) && !errors.Is(err, errGoDependenciesUnavailable) {
		return err
	}
	body, readErr := os.ReadFile(filepath.Join(root, "go.mod"))
	if readErr != nil {
		return err
	}
	metadata, parseErr := modfile.Parse("go.mod", body, nil)
	if parseErr == nil && (metadata.Go == nil || semver.Compare("v"+metadata.Go.Version, "v1.17") < 0) {
		return errors.New("this revision uses a legacy Go module graph that Fjord cannot fully prepare; vendor dependencies, or upgrade the go directive to Go 1.17 or newer and commit go mod tidy output")
	}
	return err
}

func goPackageError(detail string) error {
	switch {
	case strings.Contains(detail, "requires go >="), strings.Contains(detail, "maximum version supported"),
		strings.Contains(detail, "requires go1."), strings.Contains(detail, "invalid go version"):
		return errors.New("this module requires a different Go toolchain; upgrade the Go version installed with Fjord (automatic toolchain downloads are disabled)")
	case strings.Contains(detail, "checksum mismatch"), strings.Contains(detail, "SECURITY ERROR"):
		return errors.New("Go dependency checksum verification failed; verify and commit the correct go.sum before reviewing")
	case strings.Contains(detail, "inconsistent vendoring"):
		return errors.New("Go vendor contents disagree with go.mod; regenerate and commit vendor with go mod vendor")
	case strings.Contains(detail, "missing go.sum"), strings.Contains(detail, "updates to go.mod needed"):
		return errGoModuleMetadata
	case strings.Contains(detail, "cannot find module"), strings.Contains(detail, "no required module"),
		strings.Contains(detail, "module lookup disabled"), strings.Contains(detail, "import lookup disabled"):
		return errGoDependenciesUnavailable
	default:
		return errors.New("Go packages could not be loaded; check this revision for package errors, unsupported build constraints, or missing local replacements")
	}
}

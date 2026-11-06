package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"syscall"
	"time"

	"github.com/NerdsWhoFish/fjord"
	"github.com/NerdsWhoFish/fjord/internal/github"
	"github.com/NerdsWhoFish/fjord/internal/lspconfig"
	"github.com/NerdsWhoFish/fjord/internal/navigation"
	"github.com/NerdsWhoFish/fjord/internal/server"
	"github.com/NerdsWhoFish/fjord/internal/telemetry"
)

var version = "development"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fjord:", err)
		os.Exit(1)
	}
}
func run() error {
	publicDefault, err := publicModeFromEnv()
	if err != nil {
		return err
	}
	addr := flag.String("addr", "127.0.0.1:4727", "listen address")
	browserHost := flag.String("host", "", "browser hostname or IP (required for wildcard listeners; no scheme or port)")
	publicURL := flag.String("public-url", os.Getenv("FJORD_PUBLIC_URL"), "public browser origin for a reverse proxy, such as https://fjord.stout.zone")
	configPath := flag.String("config", os.Getenv("FJORD_CONFIG"), "operator language-server YAML config (or FJORD_CONFIG)")
	publicMode := flag.Bool("public-mode", publicDefault, "require each browser to supply its own GitHub token")
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return nil
	}
	if err := validatePublicRuntime(*publicMode, *configPath); err != nil {
		return err
	}
	profiles, err := lspconfig.Load(*configPath)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdown, err := telemetry.Setup(ctx, version)
	if err != nil {
		return err
	}
	defer func() {
		flush, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutdown(flush) != nil {
			slog.Error("telemetry shutdown failed")
		}
	}()
	faro := os.Getenv("FJORD_FARO_URL")
	if err = server.ValidateFaro(faro); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return errors.New("cannot bind listen address; choose an available address and port")
	}
	defer ln.Close()
	host, origin, err := browserAddress(ln.Addr().String(), *browserHost, *publicURL)
	if err != nil {
		return err
	}
	if err := validatePublicOrigin(*publicMode, origin); err != nil {
		return err
	}
	config := server.Config{Host: host, Origin: origin, Assets: fjord.Assets(), FaroURL: faro, Version: version, PublicMode: *publicMode}
	if *publicMode {
		config.PublicNavigation = func() server.ManagedNavigation { return navigation.NewWithLimit(profiles, 2) }
	} else {
		nav := navigation.NewWithProfiles(profiles)
		defer nav.Close()
		config.GitHub, config.Navigation = github.New(nil), nav
	}
	handler := server.New(config)
	if closer, ok := handler.(interface{ Close() error }); ok {
		defer closer.Close()
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 150 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	fmt.Printf("Fjord %s\nOpen %s\n", version, origin)
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			return errors.New("HTTP server stopped unexpectedly")
		}
	case <-ctx.Done():
	}
	finish, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(finish)
}

func publicModeFromEnv() (bool, error) {
	value := os.Getenv("FJORD_PUBLIC_MODE")
	if value == "" {
		return false, nil
	}
	enabled, err := strconv.ParseBool(value)
	if err != nil {
		return false, errors.New("FJORD_PUBLIC_MODE must be true or false")
	}
	return enabled, nil
}

func validatePublicRuntime(enabled bool, configPath string) error {
	if !enabled {
		return nil
	}
	if configPath != "" {
		return errors.New("public mode uses bundled language servers; custom runtime mounts are not allowed")
	}
	for _, key := range []string{"FJORD_GO_MODULE_CACHE", "FJORD_LANGUAGE_BUNDLE", "FJORD_NODE_RUNTIME_PATHS"} {
		if os.Getenv(key) != "" {
			return fmt.Errorf("%s is not allowed in public mode; shared runtime paths can expose private files", key)
		}
	}
	return nil
}

func validatePublicOrigin(enabled bool, origin string) error {
	if !enabled {
		return nil
	}
	u, err := url.Parse(origin)
	if err == nil && (u.Scheme == "https" || u.Hostname() == "localhost" || net.ParseIP(u.Hostname()).IsLoopback()) {
		return nil
	}
	return errors.New("public mode requires an HTTPS public URL, except on loopback for development")
}

func browserAddress(listenAddr, browserHost, publicURL string) (host, origin string, err error) {
	if publicURL != "" {
		if browserHost != "" {
			return "", "", errors.New("use either -public-url or -host, not both")
		}
		origin, host, err = server.PublicAddress(publicURL)
		return host, origin, err
	}
	host, err = requestHost(listenAddr, browserHost)
	if err != nil {
		return "", "", err
	}
	origin, host, err = server.PublicAddress("http://" + host)
	return host, origin, err
}

func requestHost(listenAddr, browserHost string) (string, error) {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return "", err
	}
	if browserHost != "" {
		host = browserHost
	}
	ip := net.ParseIP(host)
	if host == "" || (ip != nil && ip.IsUnspecified()) {
		return "", errors.New("wildcard listeners require -host with the hostname or IP used in your browser")
	}
	if ip == nil && !regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9.-]*[a-zA-Z0-9])?$`).MatchString(host) {
		return "", errors.New("-host must be a hostname or IP without scheme, port, or path")
	}
	authority := net.JoinHostPort(host, port)
	if port == "80" {
		// Browsers omit the default HTTP port from Host and Origin.
		authority = authority[:len(authority)-3]
	}
	return authority, nil
}

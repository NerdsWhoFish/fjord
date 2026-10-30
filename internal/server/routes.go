package server

import (
	"bytes"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var ownerRoute = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)
var repoRoute = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)
var numberRoute = regexp.MustCompile(`^[0-9]+$`)
var hostnameLabel = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

func validPRRoute(path string) bool {
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(path, "/"), "/"), "/")
	if len(parts) != 3 || !ownerRoute.MatchString(parts[0]) || !repoRoute.MatchString(parts[1]) ||
		parts[1] == "." || parts[1] == ".." || !numberRoute.MatchString(parts[2]) {
		return false
	}
	switch strings.ToLower(parts[0]) {
	case "api", "assets", "mcp":
		return false
	}
	number, err := strconv.ParseUint(parts[2], 10, 53)
	return err == nil && number > 0
}

func staticHandler(assets fs.FS) http.Handler {
	files := http.FileServerFS(assets)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawPath == "" && validPRRoute(r.URL.Path) {
			index, err := fs.ReadFile(assets, "index.html")
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
			return
		}
		files.ServeHTTP(w, r)
	})
}

// PublicAddress pins browser trust independently of proxy-supplied headers.
func PublicAddress(raw string) (origin, host string, err error) {
	invalid := errors.New("public URL must be an http or https origin with a valid host and no credentials, path, query, or fragment")
	u, parseErr := url.Parse(raw)
	if parseErr != nil || strings.TrimSpace(raw) != raw || u == nil ||
		(u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		u.Opaque != "" || (u.Path != "" && u.Path != "/") || u.RawPath != "" ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") {
		return "", "", invalid
	}
	name := strings.ToLower(u.Hostname())
	ip := net.ParseIP(name)
	if ip != nil {
		if ip.IsUnspecified() {
			return "", "", invalid
		}
		name = ip.String()
	} else {
		if len(name) > 253 {
			return "", "", invalid
		}
		for _, label := range strings.Split(name, ".") {
			if !hostnameLabel.MatchString(label) {
				return "", "", invalid
			}
		}
	}
	port := u.Port()
	if strings.HasSuffix(u.Host, ":") {
		return "", "", invalid
	}
	if port != "" {
		n, parseErr := strconv.Atoi(port)
		if parseErr != nil || n < 1 || n > 65535 {
			return "", "", invalid
		}
		port = strconv.Itoa(n)
	}
	if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		port = ""
	}
	host = name
	if strings.Contains(name, ":") {
		host = "[" + name + "]"
	}
	if port != "" {
		host = net.JoinHostPort(name, port)
	}
	return u.Scheme + "://" + host, host, nil
}

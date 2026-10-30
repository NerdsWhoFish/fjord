package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"github.com/NerdsWhoFish/fjord"
	"github.com/NerdsWhoFish/fjord/internal/assist"
	"github.com/NerdsWhoFish/fjord/internal/github"
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
	addr := flag.String("addr", "127.0.0.1:4727", "listen address")
	browserHost := flag.String("host", "", "browser hostname or IP (required for wildcard listeners; no scheme or port)")
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return nil
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
	ai, err := assist.New(os.Getenv("FJORD_AI_ENDPOINT"), os.Getenv("FJORD_AI_MODEL"), os.Getenv("FJORD_AI_API_KEY"))
	if err != nil {
		return err
	}
	faro := os.Getenv("FJORD_FARO_URL")
	if err = server.ValidateFaro(faro); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return errors.New("cannot bind listen address; choose an available address and port")
	}
	defer ln.Close()
	host, err := requestHost(ln.Addr().String(), *browserHost)
	if err != nil {
		return err
	}
	nav := navigation.New()
	defer nav.Close()
	srv := &http.Server{Handler: server.New(server.Config{Host: host, Assets: fjord.Assets(), GitHub: github.New(nil), Navigation: nav, AI: ai, FaroURL: faro, Version: version}), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 150 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	fmt.Printf("Fjord %s\nOpen http://%s\n", version, host)
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

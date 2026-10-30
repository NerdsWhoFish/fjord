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
	addr := flag.String("addr", "127.0.0.1:4727", "loopback listen address")
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return nil
	}
	host, _, err := net.SplitHostPort(*addr)
	if err != nil || (host != "127.0.0.1" && host != "::1") {
		return errors.New("listen address must use 127.0.0.1 or [::1]; remote hosting is not supported")
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
		return errors.New("cannot bind loopback address; choose an available port")
	}
	defer ln.Close()
	nav := navigation.New()
	defer nav.Close()
	srv := &http.Server{Handler: server.New(server.Config{Host: ln.Addr().String(), Assets: fjord.Assets(), GitHub: github.New(nil), Navigation: nav, AI: ai, FaroURL: faro, Version: version}), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 150 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	fmt.Printf("Fjord %s\nOpen http://%s\n", version, ln.Addr())
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

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"note/internal/config"
	"note/internal/store"
	"note/internal/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, warnings, err := config.Load(os.Args[1:])
	if err != nil {
		return err
	}
	for _, warning := range warnings {
		slog.Warn(warning)
	}
	slog.Info("starting note", "addr", cfg.Addr, "db", cfg.DBPath, "allow_remote", cfg.AllowRemote)

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := st.Close(); err != nil {
			slog.Error("close database", "error", err)
		}
	}()

	dist, err := web.DistFS()
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           web.New(cfg, st, dist),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Addr, err)
	}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(ln)
	}()
	slog.Info("listening", "addr", cfg.Addr)
	fmt.Println("note 已启动，在浏览器打开：")
	for _, url := range config.StartupURLs(cfg.Addr, ln.Addr().String()) {
		fmt.Println(url)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)

	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case sig := <-stop:
		slog.Info("shutting down", "signal", sig.String())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		server.Close()
		return err
	}

	return nil
}

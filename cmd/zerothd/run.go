package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/avivl/zeroth/internal/logging"
	"github.com/avivl/zeroth/internal/resilience"
	"github.com/avivl/zeroth/internal/server"
	"github.com/avivl/zeroth/internal/store/sqlite"
	"go.uber.org/zap"
)

type deps struct {
	probe  func(context.Context, string) error
	writer io.Writer
	serve  func(ctx context.Context, addr string, h http.Handler) error
}

func runDaemon(ctx context.Context, cfg Config, d deps) error {
	log, err := logging.New(logging.Options{
		Level:    cfg.LogLevel,
		Encoding: cfg.LogEncoding,
		Writer:   d.writer,
	})
	if err != nil {
		return fmt.Errorf("zerothd logger: %w", err)
	}
	defer func() { _ = log.Sync() }()

	log.Info("zerothd starting",
		zap.String("addr", cfg.Addr),
		zap.String("db_path", cfg.DBPath),
		zap.String("docker_socket", cfg.DockerSocket),
		zap.String("log_level", cfg.LogLevel),
		zap.String("log_encoding", cfg.LogEncoding),
		zap.String("config_file", cfg.ConfigFile),
	)

	probe := d.probe
	if probe == nil {
		probe = func(ctx context.Context, socket string) error {
			return resilience.DialUnix(ctx, socket, resilience.Defaults())
		}
	}
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := probe(probeCtx, cfg.DockerSocket); err != nil {
		log.Warn("docker socket probe failed", zap.String("socket", cfg.DockerSocket), zap.Error(err))
	} else {
		log.Info("docker socket reachable", zap.String("socket", cfg.DockerSocket))
	}

	st, err := sqlite.New(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("zerothd store: %w", err)
	}
	defer func() { _ = st.Close() }()

	srv, err := server.New(server.Config{Store: st, Log: log})
	if err != nil {
		return fmt.Errorf("zerothd server: %w", err)
	}
	defer srv.Close()

	log.Info("zerothd listening", zap.String("addr", cfg.Addr))
	serve := d.serve
	if serve == nil {
		serve = listenAndServe
	}
	if err := serve(ctx, cfg.Addr, srv.Handler()); err != nil {
		return fmt.Errorf("zerothd listen: %w", err)
	}
	return nil
}

func listenAndServe(ctx context.Context, addr string, h http.Handler) error {
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	errc := make(chan error, 1)
	go func() {
		errc <- httpSrv.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shctx)
		err := <-errc
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	case err := <-errc:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

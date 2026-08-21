package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/avivl/zeroth/internal/logging"
	"github.com/avivl/zeroth/internal/resilience"
	"go.uber.org/zap"
)

type deps struct {
	probe  func(context.Context, string) error
	writer io.Writer
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

	log.Info("skeleton stub (would bind)", zap.String("addr", cfg.Addr))
	return nil
}

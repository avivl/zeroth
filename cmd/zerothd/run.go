package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/avivl/zeroth/internal/harness/claudecode"
	"github.com/avivl/zeroth/internal/logging"
	"github.com/avivl/zeroth/internal/plan"
	"github.com/avivl/zeroth/internal/resilience"
	"github.com/avivl/zeroth/internal/sandbox/docker"
	"github.com/avivl/zeroth/internal/server"
	"github.com/avivl/zeroth/internal/signer"
	"github.com/avivl/zeroth/internal/store/sqlite"
	"github.com/avivl/zeroth/internal/tracker"
	"github.com/avivl/zeroth/internal/tracker/linear"
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

	sg, backend, err := openSigner(cfg)
	if err != nil {
		return fmt.Errorf("zerothd signer: %w", err)
	}

	var tr tracker.Provider
	webhook := false
	if cfg.LinearAPIKey != "" {
		p, err := linear.New(linear.Config{
			APIKey:        cfg.LinearAPIKey,
			Endpoint:      cfg.LinearEndpoint,
			AgentUserID:   cfg.LinearAgentUser,
			TeamID:        cfg.LinearTeamID,
			ProjectID:     cfg.LinearProjectID,
			PollInterval:  cfg.LinearPollInterval,
			WebhookSecret: cfg.LinearWebhookSecret,
			AuthStyle:     linear.AuthStyle(cfg.LinearAuthStyle),
			Log:           log,
		})
		if err != nil {
			return fmt.Errorf("zerothd linear: %w", err)
		}
		tr = p
		webhook = cfg.LinearWebhookSecret != ""
		log.Info("linear tracker enabled")
	}

	h := claudecode.New()
	if err := claudecode.APIKeyConfigured(); err != nil {
		log.Warn("harness will fail until ANTHROPIC_API_KEY is set", zap.String("harness", h.Name()))
	} else {
		log.Info("harness enabled", zap.String("harness", h.Name()))
	}

	workspaceRoot := detectWorkspaceRoot()
	if workspaceRoot != "" {
		log.Info("workspace root", zap.String("dir", workspaceRoot))
	}

	reviewer, reviewerModel, err := openReviewer(cfg, log)
	if err != nil {
		return fmt.Errorf("zerothd reviewer: %w", err)
	}

	srv, err := server.New(server.Config{
		Store:                st,
		Signer:               sg,
		Log:                  log,
		Tracker:              tr,
		Sandbox:              docker.New(),
		Harness:              h,
		TrackerWebhook:       webhook,
		WorkspaceRoot:        workspaceRoot,
		Reviewer:             reviewer,
		DefaultReviewerModel: reviewerModel,
	})
	if err != nil {
		return fmt.Errorf("zerothd server: %w", err)
	}
	defer srv.Close()

	log.Info("zerothd listening", zap.String("addr", cfg.Addr), zap.String("signer", backend))
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

func openReviewer(cfg Config, log *zap.Logger) (plan.Reviewer, string, error) {
	key := strings.TrimSpace(cfg.ReviewerAPIKey)
	if key == "" {
		key = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	}
	if key == "" {
		log.Warn("cross-exam using pass-through reviewer; set ZEROTH_REVIEWER_API_KEY or OPENAI_API_KEY for an independent OpenAI reviewer. Human approval remains the gate")
		return nil, "", nil
	}
	rev, err := server.NewChatReviewer(server.ChatReviewerConfig{
		Model:   cfg.ReviewerModel,
		BaseURL: cfg.ReviewerBaseURL,
		APIKey:  key,
	})
	if err != nil {
		return nil, "", err
	}
	log.Info("cross-exam reviewer enabled",
		zap.String("vendor", server.ChatReviewerVendor),
		zap.String("model", rev.Model()),
	)
	return rev, rev.Model(), nil
}

func detectWorkspaceRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return cwd
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return cwd
	}
	return root
}

func openSigner(cfg Config) (signer.Service, string, error) {
	pass := os.Getenv("ZEROTH_SIGNING_PASSPHRASE")
	if path := strings.TrimSpace(cfg.SigningKey); path != "" {
		sg, err := signer.NewFile(path, pass)
		return sg, "file", err
	}
	if sg, err := signer.NewKeyring("zeroth"); err == nil {
		if _, err := sg.PublicKey(context.Background(), "__zeroth_probe__"); errors.Is(err, signer.ErrNotFound) {
			return sg, "keyring", nil
		}
	}
	dir := filepath.Dir(cfg.DBPath)
	path := filepath.Join(dir, "zeroth.keys")
	sg, err := signer.NewFile(path, pass)
	return sg, "file", err
}

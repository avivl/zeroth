package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

func writeTar(ctx context.Context, dir string, w io.Writer) error {
	cmd := exec.CommandContext(ctx, "tar", "-C", dir, "--numeric-owner", "-cf", "-", ".")
	cmd.Stdout = w
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tar write %s: %w: %s", dir, err, stderr.String())
	}
	return nil
}

func readTar(ctx context.Context, dir string, r io.Reader) error {
	cmd := exec.CommandContext(ctx, "tar", "-C", dir, "--numeric-owner", "-xf", "-")
	cmd.Stdin = r
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tar read %s: %w: %s", dir, err, stderr.String())
	}
	return nil
}

func extractTarFile(ctx context.Context, tarPath, dest string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return fmt.Errorf("open tar: %w", err)
	}
	defer f.Close()
	return readTar(ctx, dest, f)
}

func clearDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("clear dir: %w", err)
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return fmt.Errorf("clear dir %s: %w", e.Name(), err)
		}
	}
	return nil
}

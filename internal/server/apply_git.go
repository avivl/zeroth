package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/avivl/zeroth/internal/resilience"
	"github.com/failsafe-go/failsafe-go"
)

type gitPublisher struct {
	git    func(ctx context.Context, dir string, args ...string) (string, error)
	gh     func(ctx context.Context, args ...string) (string, error)
	execer failsafe.Executor[string]
}

func newGitPublisher() *gitPublisher {
	opts := resilience.Defaults()
	opts.Timeout = 30 * time.Second
	return &gitPublisher{
		git:    runGit,
		gh:     runGH,
		execer: resilience.NewExecutor[string](opts),
	}
}

func (p *gitPublisher) Publish(ctx context.Context, req ApplyPublish) (ApplyRef, error) {
	if p.git == nil {
		p.git = runGit
	}
	if p.gh == nil {
		p.gh = runGH
	}
	if p.execer == nil {
		p.execer = newGitPublisher().execer
	}
	repo := strings.TrimSpace(req.Repo)
	if repo == "" {
		repo = gitRoot(ctx, p.git, req.Workspace)
	}
	if repo == "" {
		return ApplyRef{}, fmt.Errorf("no git repository to publish from (set the daemon working directory to a checkout)")
	}
	branch := strings.TrimSpace(req.Branch)
	if branch == "" {
		branch = gitBranchName(req.IssueKey, req.Title)
	}
	work, err := os.MkdirTemp("", "zeroth-apply-")
	if err != nil {
		return ApplyRef{}, fmt.Errorf("worktree temp: %w", err)
	}
	// git worktree add requires the path not to exist.
	if err := os.RemoveAll(work); err != nil {
		return ApplyRef{}, fmt.Errorf("worktree temp: %w", err)
	}
	branch, err = p.addWorktree(ctx, repo, branch, work)
	if err != nil {
		return ApplyRef{}, err
	}
	defer func() {
		_, _ = p.git(context.WithoutCancel(ctx), repo, "worktree", "remove", "--force", work)
		_ = os.RemoveAll(work)
	}()

	if err := copyTargets(req.Workspace, work, req.Targets); err != nil {
		return ApplyRef{}, err
	}
	if _, err := p.git(ctx, work, "add", "-A"); err != nil {
		return ApplyRef{}, fmt.Errorf("git add: %w", err)
	}
	status, err := p.git(ctx, work, "status", "--porcelain")
	if err != nil {
		return ApplyRef{}, fmt.Errorf("git status: %w", err)
	}
	if strings.TrimSpace(status) == "" {
		return ApplyRef{}, fmt.Errorf("nothing to commit after applying plan files")
	}
	msg := applyCommitMessage(req)
	if _, err := p.git(ctx, work, "-c", "user.name=zeroth", "-c", "user.email=zeroth@local", "-c", "commit.gpgsign=false", "commit", "-m", msg); err != nil {
		return ApplyRef{}, fmt.Errorf("git commit: %w", err)
	}
	sha, err := p.git(ctx, work, "rev-parse", "HEAD")
	if err != nil {
		return ApplyRef{}, fmt.Errorf("git rev-parse: %w", err)
	}
	sha = strings.TrimSpace(sha)

	if _, err := resilience.Get(ctx, p.execer, func(ctx context.Context) (string, error) {
		return p.git(ctx, work, "push", "-u", "origin", branch)
	}); err != nil {
		return ApplyRef{}, fmt.Errorf("git push: %w", err)
	}

	pr, err := p.openPR(ctx, repo, branch, req)
	if err != nil {
		return ApplyRef{}, err
	}
	return ApplyRef{Branch: branch, Commit: sha, PullRequest: pr}, nil
}

func (p *gitPublisher) ClosePullRequest(ctx context.Context, url, comment string) error {
	if p.gh == nil {
		p.gh = runGH
	}
	if p.execer == nil {
		p.execer = newGitPublisher().execer
	}
	url = strings.TrimSpace(url)
	if url == "" {
		return fmt.Errorf("gh pr close: empty url")
	}
	state, err := p.prState(ctx, url)
	if err != nil {
		return err
	}
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case "CLOSED":
		return nil
	case "MERGED":
		return fmt.Errorf("%w: %s", errPRMerged, url)
	}
	args := []string{"pr", "close", url}
	if c := strings.TrimSpace(comment); c != "" {
		args = append(args, "--comment", c)
	}
	_, err = resilience.Get(ctx, p.execer, func(ctx context.Context) (string, error) {
		return p.gh(ctx, args...)
	})
	if err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "already closed") {
			return nil
		}
		if strings.Contains(msg, "already merged") || strings.Contains(msg, "was merged") {
			return fmt.Errorf("%w: %s", errPRMerged, url)
		}
		return fmt.Errorf("gh pr close: %w", err)
	}
	return nil
}

func (p *gitPublisher) prState(ctx context.Context, url string) (string, error) {
	out, err := resilience.Get(ctx, p.execer, func(ctx context.Context) (string, error) {
		return p.gh(ctx, "pr", "view", url, "--json", "state")
	})
	if err != nil {
		return "", fmt.Errorf("gh pr view: %w", err)
	}
	var parsed struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &parsed); err != nil {
		return "", nil
	}
	return parsed.State, nil
}

func (p *gitPublisher) addWorktree(ctx context.Context, repo, branch, dir string) (string, error) {
	if _, err := p.git(ctx, repo, "worktree", "add", "-b", branch, dir, "HEAD"); err == nil {
		return branch, nil
	}
	alt := branch + "-" + strconvBase36(time.Now().Unix())
	if _, err := p.git(ctx, repo, "worktree", "add", "-b", alt, dir, "HEAD"); err != nil {
		return "", fmt.Errorf("git worktree add: %w", err)
	}
	return alt, nil
}

func (p *gitPublisher) openPR(ctx context.Context, repo, branch string, req ApplyPublish) (string, error) {
	base := detectBaseBranch(ctx, p.git, repo)
	args := []string{"pr", "create", "--title", req.Title, "--body", req.Body, "--base", base, "--head", branch}
	if remote, err := p.git(ctx, repo, "remote", "get-url", "origin"); err == nil {
		if slug, ok := githubRepoSlug(remote); ok {
			args = append(args, "--repo", slug)
		}
	}
	out, err := resilience.Get(ctx, p.execer, func(ctx context.Context) (string, error) {
		return p.gh(ctx, args...)
	})
	if err != nil {
		return "", fmt.Errorf("gh pr create: %w", err)
	}
	if url := firstHTTPURL(out); url != "" {
		return url, nil
	}
	return "", fmt.Errorf("gh pr create: no pull request URL in output")
}

func copyTargets(src, dest string, targets []string) error {
	for _, target := range targets {
		if skipGitTarget(target) {
			continue
		}
		from, err := safeJoin(src, target)
		if err != nil {
			return err
		}
		to, err := safeJoin(dest, target)
		if err != nil {
			return err
		}
		info, err := os.Stat(from)
		if os.IsNotExist(err) {
			if rmErr := os.Remove(to); rmErr != nil && !os.IsNotExist(rmErr) {
				return fmt.Errorf("apply publish remove %s: %w", target, rmErr)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("apply publish %s: %w", target, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("apply publish %s: not a regular file", target)
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return fmt.Errorf("apply publish %s: %w", target, err)
		}
		if err := copyFile(from, to, info.Mode()); err != nil {
			return fmt.Errorf("apply publish %s: %w", target, err)
		}
	}
	return nil
}

func gitBranchName(issueKey, summary string) string {
	key := strings.ToLower(strings.TrimSpace(issueKey))
	slug := slugify(summary)
	if key != "" {
		slug = strings.TrimPrefix(slug, key+"-")
		if slug == "" {
			slug = "apply"
		}
		return "zeroth/" + key + "-" + truncateSlug(slug, 48)
	}
	if slug == "" {
		slug = "apply"
	}
	return "zeroth/" + truncateSlug(slug, 60)
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := true
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func truncateSlug(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	s = s[:n]
	return strings.Trim(s, "-")
}

func applyCommitMessage(req ApplyPublish) string {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Apply approved plan"
	}
	var b strings.Builder
	if k := strings.TrimSpace(req.IssueKey); k != "" {
		fmt.Fprintf(&b, "%s: %s\n", k, title)
	} else {
		fmt.Fprintf(&b, "%s\n", title)
	}
	if h := strings.TrimSpace(req.PlanHash); h != "" {
		fmt.Fprintf(&b, "\nApproved plan %s.\n", h)
	}
	return b.String()
}

func gitRoot(ctx context.Context, git func(context.Context, string, ...string) (string, error), dir string) string {
	if strings.TrimSpace(dir) == "" || git == nil {
		return ""
	}
	out, err := git(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func detectBaseBranch(ctx context.Context, git func(context.Context, string, ...string) (string, error), repo string) string {
	out, err := git(ctx, repo, "rev-parse", "--abbrev-ref", "origin/HEAD")
	if err == nil {
		ref := strings.TrimSpace(out)
		if _, name, ok := strings.Cut(ref, "/"); ok && name != "" {
			return name
		}
		if ref != "" && ref != "HEAD" {
			return ref
		}
	}
	for _, name := range []string{"main", "master"} {
		if _, err := git(ctx, repo, "rev-parse", "--verify", "refs/heads/"+name); err == nil {
			return name
		}
	}
	return "main"
}

func githubRepoSlug(remote string) (string, bool) {
	remote = strings.TrimSpace(remote)
	remote = strings.TrimSuffix(remote, ".git")
	prefixes := []string{
		"git@github.com:",
		"https://github.com/",
		"http://github.com/",
		"ssh://git@github.com/",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(remote, p) {
			slug := strings.TrimPrefix(remote, p)
			slug = strings.TrimPrefix(slug, "/")
			if slug != "" && strings.Count(slug, "/") == 1 {
				return slug, true
			}
		}
	}
	return "", false
}

func firstHTTPURL(s string) string {
	for _, field := range strings.Fields(s) {
		if strings.HasPrefix(field, "https://") || strings.HasPrefix(field, "http://") {
			return field
		}
	}
	return ""
}

func strconvBase36(n int64) string {
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	if n == 0 {
		return "0"
	}
	if n < 0 {
		n = -n
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%36]
		n /= 36
	}
	return string(buf[i:])
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = gitCommandEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
}

func runGH(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return stdout.String(), nil
}

func gitCommandEnv() []string {
	env := os.Environ()
	hasName, hasEmail := false, false
	for _, e := range env {
		if strings.HasPrefix(e, "GIT_AUTHOR_NAME=") || strings.HasPrefix(e, "GIT_COMMITTER_NAME=") {
			hasName = true
		}
		if strings.HasPrefix(e, "GIT_AUTHOR_EMAIL=") || strings.HasPrefix(e, "GIT_COMMITTER_EMAIL=") {
			hasEmail = true
		}
	}
	if !hasName {
		env = append(env, "GIT_AUTHOR_NAME=zeroth", "GIT_COMMITTER_NAME=zeroth")
	}
	if !hasEmail {
		env = append(env, "GIT_AUTHOR_EMAIL=zeroth@local", "GIT_COMMITTER_EMAIL=zeroth@local")
	}
	return env
}

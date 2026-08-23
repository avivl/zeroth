package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/avivl/zeroth/internal/audit"
	"github.com/avivl/zeroth/internal/memory"
	"github.com/avivl/zeroth/internal/tracker"
	"go.uber.org/zap"
)

const commentFactPrefix = "comment."

func (s *Server) loadIssueThread(ctx context.Context, key string, iss tracker.Issue) (tracker.Issue, []tracker.Comment) {
	if iss.Key == "" && s.tracker != nil {
		s.log.Info("tracker get issue on assign", zap.String("key", key))
		got, err := s.tracker.GetIssue(ctx, key)
		if err != nil {
			s.log.Warn("tracker get issue on assign", zap.String("key", key), zap.Error(err))
		} else {
			iss = got
		}
	}
	if s.tracker == nil {
		return iss, nil
	}
	comments, err := s.tracker.ListComments(ctx, key)
	if err != nil {
		s.log.Warn("tracker list comments on assign", zap.String("key", key), zap.Error(err))
		return iss, nil
	}
	return iss, comments
}

func (s *Server) ingestOperatorComments(ctx context.Context, issueKey string, comments []tracker.Comment) {
	human := tracker.HumanComments(comments)
	if len(human) == 0 {
		return
	}
	nb := s.notebook()
	if nb == nil {
		return
	}
	key := commentFactKey(issueKey)
	body := commentFactBody(issueKey, human)
	if prev, err := nb.Get(ctx, memory.KindOperator, "", key); err == nil && !prev.Deleted && prev.Body == body {
		return
	}
	author := strings.TrimSpace(human[len(human)-1].Author)
	if author == "" {
		author = audit.ApproverOperator
	}
	source := "tracker.comment:" + issueKey
	if u := strings.TrimSpace(human[len(human)-1].URL); u != "" {
		source = u
	}
	if _, err := nb.Write(ctx, memory.Human(author), memory.KindOperator, "", key, body, source); err != nil {
		s.log.Warn("memory ingest tracker comments", zap.String("key", issueKey), zap.Error(err))
	}
}

func (s *Server) sessionFacts(ctx context.Context, sessionID, agentID string) []memory.Fact {
	nb := s.notebook()
	if nb == nil {
		return nil
	}
	all, err := nb.Slice(ctx, "", "")
	if err != nil {
		s.log.Warn("memory slice on assign", zap.Error(err))
		return nil
	}
	return memory.ForSession(all, sessionID, agentID)
}

func commentFactKey(issueKey string) string {
	return commentFactPrefix + strings.TrimSpace(issueKey)
}

func commentFactBody(issueKey string, comments []tracker.Comment) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Settled operator comments on Linear %s.\n\n", issueKey)
	for i, c := range comments {
		if i > 0 {
			b.WriteByte('\n')
		}
		who := strings.TrimSpace(c.Author)
		if who == "" {
			who = "operator"
		}
		fmt.Fprintf(&b, "[%s]\n%s\n", who, strings.TrimSpace(c.Body))
	}
	return strings.TrimRight(b.String(), "\n")
}

func issuePrompt(key string, iss tracker.Issue, comments []tracker.Comment, facts []memory.Fact) string {
	title := strings.TrimSpace(iss.Title)
	if title == "" {
		title = key
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Linear %s: %s", key, title)
	if d := strings.TrimSpace(iss.Description); d != "" {
		b.WriteString("\n\n")
		b.WriteString(d)
	}
	human := tracker.HumanComments(comments)
	if len(human) > 0 {
		b.WriteString("\n\n## Comment thread\n")
		for _, c := range human {
			b.WriteString("\n")
			who := strings.TrimSpace(c.Author)
			if who == "" {
				who = "operator"
			}
			fmt.Fprintf(&b, "**%s**", who)
			if !c.At.IsZero() {
				fmt.Fprintf(&b, " (%s)", c.At.UTC().Format("2006-01-02"))
			}
			b.WriteString(":\n")
			b.WriteString(strings.TrimSpace(c.Body))
			b.WriteString("\n")
		}
	}
	live := make([]memory.Fact, 0, len(facts))
	for _, f := range facts {
		if f.Deleted || strings.TrimSpace(f.Body) == "" {
			continue
		}
		live = append(live, f)
	}
	if len(live) > 0 {
		b.WriteString("\n## Project memory\n")
		for _, f := range live {
			fmt.Fprintf(&b, "\n### `%s`\n\n%s\n", f.Key, strings.TrimRight(f.Body, "\n"))
		}
	}
	return b.String()
}

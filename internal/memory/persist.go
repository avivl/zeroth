package memory

import (
	"github.com/avivl/zeroth/internal/store"
)

func factFromEntry(m store.MemoryEntry) Fact {
	who := Actor{Kind: ActorKind(m.AuthorKind), Name: m.Author}
	if who.Kind == "" && m.Author != "" {
		who.Kind = ActorHuman
	}
	f := Fact{
		ID:        m.ID,
		Kind:      m.Kind,
		RefID:     m.RefID,
		Key:       m.Key,
		Body:      m.Content,
		Deleted:   m.Deleted,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
		Provenance: Provenance{
			Who:    who,
			What:   m.Action,
			When:   m.UpdatedAt,
			Source: m.Source,
		},
		Versions: revisionsFromStore(m.History),
	}
	if f.Key == "" {
		f.Key = m.ID.String()
	}
	if f.UpdatedAt.IsZero() {
		f.UpdatedAt = m.CreatedAt
	}
	if f.Provenance.When.IsZero() {
		f.Provenance.When = f.UpdatedAt
	}
	if len(f.Versions) == 0 && m.Content != "" {
		f.Versions = []Revision{{
			Version: 1,
			Key:     f.Key,
			Body:    m.Content,
			Author:  who,
			Action:  firstNonEmpty(m.Action, ActionWrite),
			Source:  m.Source,
			Deleted: m.Deleted,
			At:      f.CreatedAt,
		}}
	}
	return f
}

func entryFromFact(f Fact) store.MemoryEntry {
	action := f.Provenance.What
	if action == "" && len(f.Versions) > 0 {
		action = f.Versions[len(f.Versions)-1].Action
	}
	return store.MemoryEntry{
		ID:         f.ID,
		Kind:       f.Kind,
		RefID:      f.RefID,
		Key:        f.Key,
		Content:    f.Body,
		Author:     f.Provenance.Who.Name,
		AuthorKind: string(f.Provenance.Who.Kind),
		Source:     f.Provenance.Source,
		Action:     action,
		Deleted:    f.Deleted,
		Version:    len(f.Versions),
		CreatedAt:  f.CreatedAt,
		UpdatedAt:  f.UpdatedAt,
		History:    revisionsToStore(f.Versions),
	}
}

func proposalFromStore(p store.MemoryProposal) Proposal {
	kind := ActorKind(p.AuthorKind)
	if kind == "" {
		kind = ActorAgent
	}
	out := Proposal{
		ID:         p.ID,
		Kind:       p.Kind,
		RefID:      p.RefID,
		SessionID:  p.SessionID,
		Key:        p.Key,
		Body:       p.Content,
		Agent:      Actor{Kind: kind, Name: p.Author},
		Status:     p.Status,
		MemoryID:   p.MemoryID,
		Source:     p.Source,
		CreatedAt:  p.CreatedAt,
		ReviewedAt: p.ReviewedAt,
	}
	if out.Key == "" {
		out.Key = p.ID.String()
	}
	if out.Agent.Name == "" {
		out.Agent = AgentActor("agent")
	}
	return out
}

func storeFromProposal(p Proposal) store.MemoryProposal {
	return store.MemoryProposal{
		ID:         p.ID,
		Kind:       p.Kind,
		RefID:      p.RefID,
		SessionID:  p.SessionID,
		Key:        p.Key,
		Content:    p.Body,
		Author:     p.Agent.Name,
		AuthorKind: string(p.Agent.Kind),
		Source:     p.Source,
		Status:     p.Status,
		MemoryID:   p.MemoryID,
		CreatedAt:  p.CreatedAt,
		ReviewedAt: p.ReviewedAt,
	}
}

func revisionsFromStore(in []store.MemoryRevision) []Revision {
	out := make([]Revision, 0, len(in))
	for _, r := range in {
		who := Actor{Kind: ActorKind(r.AuthorKind), Name: r.Author}
		if who.Kind == "" {
			who.Kind = ActorHuman
		}
		out = append(out, Revision{
			Version: r.Version,
			Key:     r.Key,
			Body:    r.Body,
			Author:  who,
			Action:  r.Action,
			Source:  r.Source,
			Deleted: r.Deleted,
			At:      r.At,
		})
	}
	return out
}

func revisionsToStore(in []Revision) []store.MemoryRevision {
	out := make([]store.MemoryRevision, 0, len(in))
	for _, r := range in {
		out = append(out, store.MemoryRevision{
			Version:    r.Version,
			Key:        r.Key,
			Body:       r.Body,
			Author:     r.Author.Name,
			AuthorKind: string(r.Author.Kind),
			Action:     r.Action,
			Source:     r.Source,
			Deleted:    r.Deleted,
			At:         r.At,
		})
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

package server

import (
	"context"

	"github.com/avivl/zeroth/internal/plan"
)

const passNotesModel = "pass-notes"

// PassThroughNotes is the placeholder when no independent reviewer is
// configured. A real reviewer must never return this string.
const PassThroughNotes = "No independent reviewer model is configured. Human approval is the gate."

// passNotesReviewer lets a draft reach the human inbox when no
// independent reviewer model is configured. Missing review is still a
// deny for Approve (the exam must run); this is the exam, not a skip of
// the human gate. Notes stay on the plan so the operator can see that
// no second model ran.
type passNotesReviewer struct{}

func (passNotesReviewer) Review(_ context.Context, model string, _ plan.Packet) (plan.Review, error) {
	if model == "" {
		model = passNotesModel
	}
	return plan.Review{
		Verdict: plan.VerdictPassWithNotes,
		Notes:   PassThroughNotes,
		Model:   model,
	}, nil
}

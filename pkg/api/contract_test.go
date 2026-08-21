package api_test

import (
	"os"
	"reflect"
	"strings"
	"testing"

	gen "github.com/avivl/zeroth/pkg/api/gen/go"
)

// stage1Paths is the product surface from Linear 42-17. A view that cannot
// be expressed against these paths does not ship on the web UI or the CLI.
// /health is operational liveness, not a product view, but is part of the spec.
var stage1Paths = []string{
	"/health",
	"/runs",
	"/runs/{id}",
	"/runs/{id}/events",
	"/runs/{id}/steer",
	"/runs/{id}/background",
	"/plans/{id}",
	"/plans/{id}/approve",
	"/plans/{id}/request-changes",
	"/plans/{id}/branch",
	"/agents",
	"/agents/{id}",
	"/approvals",
	"/memory",
	"/memory/proposals",
	"/memory/proposals/{id}/accept",
	"/memory/proposals/{id}/reject",
	"/audit",
	"/audit/{id}/verify",
	"/checkpoints",
	"/checkpoints/{id}/restore",
}

func TestStage1Surface(t *testing.T) {
	t.Parallel()
	spec := readSpec(t)
	for _, path := range stage1Paths {
		if !hasPath(spec, path) {
			t.Errorf("openapi.yaml missing path %s", path)
		}
	}
}

func TestStage1Operations(t *testing.T) {
	t.Parallel()
	spec := readSpec(t)
	ops := []string{
		"health",
		"listRuns",
		"createRun",
		"getRun",
		"getRunEvents",
		"steerRun",
		"backgroundRun",
		"getPlan",
		"approvePlan",
		"requestPlanChanges",
		"branchPlan",
		"listAgents",
		"getAgent",
		"patchAgent",
		"listApprovals",
		"listMemory",
		"createMemory",
		"listMemoryProposals",
		"acceptMemoryProposal",
		"rejectMemoryProposal",
		"listAudit",
		"verifyAudit",
		"listCheckpoints",
		"restoreCheckpoint",
	}
	for _, op := range ops {
		if !strings.Contains(spec, "operationId: "+op+"\n") {
			t.Errorf("openapi.yaml missing operationId %s", op)
		}
	}
}

func TestGeneratedArtifactsExist(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"gen/go/server.go", "gen/ts/client.ts"} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("generated file %s: %v (run task generate)", path, err)
			continue
		}
		if len(b) == 0 {
			t.Errorf("generated file %s is empty", path)
		}
	}
}

func TestGeneratedIDKindsAreDistinct(t *testing.T) {
	t.Parallel()
	runT := reflect.TypeOf(gen.RunID(""))
	planT := reflect.TypeOf(gen.PlanID(""))
	agentT := reflect.TypeOf(gen.AgentID(""))
	if runT == planT || runT == agentT || planT == agentT {
		t.Fatalf("generated ID kinds are interchangeable: RunID=%s PlanID=%s AgentID=%s", runT, planT, agentT)
	}
}

func readSpec(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatalf("read openapi.yaml: %v", err)
	}
	return string(b)
}

func hasPath(spec, path string) bool {
	quoted := "  \"" + path + "\":\n"
	unquoted := "  " + path + ":\n"
	return strings.Contains(spec, quoted) || strings.Contains(spec, unquoted)
}

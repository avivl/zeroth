package api_test

import (
	"os"
	"reflect"
	"strings"
	"testing"

	gen "github.com/avivl/zeroth/pkg/api/gen/go"
)

// stage1Paths is the product surface. A view that cannot be expressed
// against these paths does not ship on the web UI or the CLI.
// /health is operational liveness, not a product view, but is part of the spec.
var stage1Paths = []string{
	"/health",
	"/runs",
	"/runs/{id}",
	"/runs/{id}/events",
	"/runs/{id}/steer",
	"/runs/{id}/background",
	"/runs/{id}/foreground",
	"/runs/{id}/stop",
	"/runs/{id}/retract",
	"/runs/{id}/checkpoints",
	"/plans",
	"/plans/{id}",
	"/plans/{id}/approve",
	"/plans/{id}/request-changes",
	"/plans/{id}/branch",
	"/plans/{id}/apply",
	"/agents",
	"/agents/{id}",
	"/agents/{id}/cross-exam-stats",
	"/agents/{id}/leases",
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
		"foregroundRun",
		"stopRun",
		"retractRun",
		"createRunCheckpoint",
		"listPlans",
		"getPlan",
		"approvePlan",
		"requestPlanChanges",
		"branchPlan",
		"applyPlan",
		"listAgents",
		"getAgent",
		"patchAgent",
		"getAgentCrossExamStats",
		"listAgentLeases",
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

func TestBindAddressIsDocumented(t *testing.T) {
	t.Parallel()
	spec := readSpec(t)
	if !strings.Contains(spec, "http://127.0.0.1:8420") {
		t.Error("openapi.yaml must document default bind 127.0.0.1:8420")
	}
	if !strings.Contains(spec, "ZEROTH_ADDR") {
		t.Error("openapi.yaml must document ZEROTH_ADDR")
	}
}

func TestKnownRunEventTypesAreDocumented(t *testing.T) {
	t.Parallel()
	spec := readSpec(t)
	block := schemaBlock(spec, "RunEvent:")
	if !strings.Contains(block, "Known values:") {
		t.Fatal("RunEvent.type description must list known values")
	}
	for _, kind := range []string{
		"log",
		"tool_call",
		"tool_result",
		"plan_drafted",
		"cross_exam_verdict",
		"checkpoint_created",
		"effect_applied",
		"status_changed",
		"error",
	} {
		if !strings.Contains(block, kind) {
			t.Errorf("RunEvent.type description missing known value %s", kind)
		}
	}
}

func TestCreateRunHasNoSandboxField(t *testing.T) {
	t.Parallel()
	block := schemaBlock(readSpec(t), "CreateRunRequest:")
	if block == "" {
		t.Fatal("CreateRunRequest schema missing")
	}
	for _, banned := range []string{"sandbox", "driver", "docker"} {
		if strings.Contains(strings.ToLower(block), banned) {
			t.Errorf("CreateRunRequest must not include %q (stage 1 driver is not selectable)", banned)
		}
	}
}

func TestCrossExamVerdictsAreDocumented(t *testing.T) {
	t.Parallel()
	block := schemaBlock(readSpec(t), "CrossExam:")
	for _, v := range []string{"pass", "fail", "pass_with_notes"} {
		if !strings.Contains(block, v) {
			t.Errorf("CrossExam.verdict description missing %s", v)
		}
	}
	if schemaBlock(readSpec(t), "CrossExamStats:") == "" {
		t.Fatal("CrossExamStats schema missing")
	}
	if schemaBlock(readSpec(t), "ReviewerConfig:") == "" {
		t.Fatal("ReviewerConfig schema missing")
	}
}

func TestPlanModelFieldsAreDocumented(t *testing.T) {
	t.Parallel()
	spec := readSpec(t)
	plan := schemaBlock(spec, "Plan:")
	for _, field := range []string{"hash:", "expires_at:", "cost_ceiling:", "scope_id:", "credentials:"} {
		if !strings.Contains(plan, field) {
			t.Errorf("Plan schema missing %s", field)
		}
	}
	effect := schemaBlock(spec, "PlanEffect:")
	for _, field := range []string{"precondition_hash:", "postcondition_hash:", "idempotency_key:", "lease_id:"} {
		if !strings.Contains(effect, field) {
			t.Errorf("PlanEffect schema missing %s", field)
		}
	}
	if schemaBlock(spec, "CredentialConstraint:") == "" {
		t.Fatal("CredentialConstraint schema missing")
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
	kinds := []reflect.Type{
		reflect.TypeOf(gen.RunID("")),
		reflect.TypeOf(gen.PlanID("")),
		reflect.TypeOf(gen.AgentID("")),
		reflect.TypeOf(gen.ApprovalID("")),
		reflect.TypeOf(gen.MemoryID("")),
		reflect.TypeOf(gen.MemoryProposalID("")),
		reflect.TypeOf(gen.AuditID("")),
		reflect.TypeOf(gen.CheckpointID("")),
		reflect.TypeOf(gen.LeaseID("")),
		reflect.TypeOf(gen.GrantID("")),
		reflect.TypeOf(gen.ScopeID("")),
	}
	for i, left := range kinds {
		if left.Kind() == reflect.String && left.Name() == "string" {
			t.Fatalf("generated ID kind %d is a string alias, want a defined type", i)
		}
		for j, right := range kinds {
			if i < j && left == right {
				t.Fatalf("generated ID kinds are interchangeable: %s and %s", left, right)
			}
		}
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

func schemaBlock(spec, heading string) string {
	idx := strings.Index(spec, "    "+heading)
	if idx < 0 {
		return ""
	}
	rest := spec[idx+len("    "+heading):]
	// Next sibling schema is indented exactly four spaces. Nested fields
	// are indented further, so "\n    " alone would match them too.
	for i := 0; i+5 < len(rest); i++ {
		if rest[i] == '\n' && rest[i+1:i+5] == "    " && rest[i+5] != ' ' && rest[i+5] != '\t' {
			return spec[idx : idx+len("    "+heading)+i]
		}
	}
	return spec[idx:]
}

import { createElement } from "react";
import { renderToString } from "react-dom/server";
import { expect, test } from "vitest";
import { App, CrossExamNotes } from "./App";
import { ChangePlanCard } from "./views";
import { PlanStatus, PlanEffectType, type Plan } from "@zeroth/api";
import { contrastRatio, darkTheme, lightTheme, themePairs, AA_NORMAL } from "./contrast";
import { parseHash, hrefFor } from "./routes";

test("renders the product name and seven nav views", () => {
  const html = renderToString(createElement(App));
  expect(html).toContain("Zeroth");
  expect(html).toContain("Runs");
  expect(html).toContain("Agents");
  expect(html).toContain("Approvals");
  expect(html).toContain("Memory");
  expect(html).toContain("Audit");
});

test("renders cross-exam notes inline", () => {
  const html = renderToString(
    createElement(CrossExamNotes, {
      exam: {
        verdict: "fail",
        reviewer_model: "gpt-4o",
        reasoning: "scope violation: .ssh/authorized_keys",
        at: "2026-08-22T00:00:00Z",
      },
    }),
  );
  expect(html).toContain("Cross-exam");
  expect(html).toContain("fail");
  expect(html).toContain("gpt-4o");
  expect(html).toContain("scope violation: .ssh/authorized_keys");
  expect(html).toContain("Reviewer flagged a concern");
  expect(html).toContain("role=\"alert\"");
});

const pendingPlan: Plan = {
  id: "p_1",
  run_id: "s_1",
  status: PlanStatus.PendingApproval,
  summary: "edit README",
  hash: "abc",
  effects: [
    {
      type: PlanEffectType.Modify,
      path: "README.md",
      diff: "-old\n+new",
      lease_id: "lease-1",
    },
  ],
  cross_exam: {
    verdict: "pass",
    reviewer_model: "sonnet",
    reasoning: "in scope",
    at: "2026-08-22T00:00:00Z",
  },
  created_at: "2026-08-22T00:00:00Z",
  updated_at: "2026-08-22T00:00:00Z",
};

test("change plan card exposes approve, apply, request changes, and branch", () => {
  const html = renderToString(
    createElement(ChangePlanCard, {
      plan: pendingPlan,
      busy: null,
      onApprove: () => undefined,
      onApply: () => undefined,
      onChanges: () => undefined,
      onBranch: () => undefined,
    }),
  );
  expect(html).toContain("Approve");
  expect(html).toContain("Apply");
  expect(html).toContain("Request changes");
  expect(html).toContain("Branch");
  expect(html).toContain("README.md");
  expect(html).toContain("lease-1");
  expect(html).toContain("Cross-exam");
});

test("hash routes cover the seven views", () => {
  expect(parseHash("#/runs")).toEqual({ name: "runs" });
  expect(parseHash("#/runs/s_1")).toEqual({ name: "run", id: "s_1" });
  expect(parseHash("#/agents")).toEqual({ name: "agents" });
  expect(parseHash("#/agents/a_default")).toEqual({ name: "agent", id: "a_default" });
  expect(parseHash("#/approvals")).toEqual({ name: "approvals" });
  expect(parseHash("#/memory")).toEqual({ name: "memory" });
  expect(parseHash("#/audit")).toEqual({ name: "audit" });
  expect(hrefFor({ name: "run", id: "s/1" })).toBe("#/runs/s%2F1");
});

test("light and dark theme pairs meet WCAG AA contrast", () => {
  for (const theme of [lightTheme, darkTheme]) {
    for (const [fg, bg, min] of themePairs(theme)) {
      const ratio = contrastRatio(fg, bg);
      expect(ratio, `${fg} on ${bg}`).toBeGreaterThanOrEqual(min);
      expect(min).toBe(AA_NORMAL);
    }
  }
  expect(lightTheme.sigBg).toBe(darkTheme.sigBg);
});

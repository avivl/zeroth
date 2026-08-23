import { expect, test } from "vitest";
import { createElement } from "react";
import { renderToString } from "react-dom/server";
import { ChangePlanCard } from "./views";
import { StreamingText } from "./components/ui";
import { liveTailLabel } from "./api/runEvents";
import {
  executePlanGate,
  planGateFailed,
  planGatePath,
  planGatePending,
  planGateSucceeded,
} from "./planGate";
import { PlanStatus, PlanEffectType, type Plan } from "@zeroth/api";

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
    },
  ],
  created_at: "2026-08-22T00:00:00Z",
  updated_at: "2026-08-22T00:00:00Z",
};

function renderSurfaces(args: {
  gate: ReturnType<typeof planGateSucceeded> | null;
  tail: "open" | "reconnecting" | "closed";
  busy?: string | null;
}): string {
  return renderToString(
    createElement(
      "div",
      null,
      createElement(StreamingText, { text: "token chunk", status: args.tail }),
      createElement(ChangePlanCard, {
        plan: pendingPlan,
        busy: args.busy ?? null,
        gate: args.gate,
        onApprove: () => undefined,
        onApply: () => undefined,
        onChanges: () => undefined,
        onBranch: () => undefined,
      }),
    ),
  );
}

test("approve path names POST /plans/{id}/approve, not apply", () => {
  expect(planGatePath("approve", "p_1")).toBe("POST /plans/p_1/approve");
  expect(planGatePath("apply", "p_1")).toBe("POST /plans/p_1/apply");
});

test("executePlanGate reports pending then ok from the REST call", async () => {
  const seen: string[] = [];
  const result = await executePlanGate(
    "approve",
    "p_1",
    async () => "ok-body",
    (o) => seen.push(`${o.phase}:${o.message}`),
  );
  expect(result).toEqual({ ok: true, value: "ok-body" });
  expect(seen).toHaveLength(2);
  expect(seen[0]).toMatch(/^pending:Sending approval\. Waiting for POST \/plans\/p_1\/approve\./);
  expect(seen[1]).toMatch(/^ok:Approval sent\. POST \/plans\/p_1\/approve succeeded\./);
});

test("pending is reported before the REST promise settles", async () => {
  let resolve!: (value: string) => void;
  const pending = new Promise<string>((r) => {
    resolve = r;
  });
  const phases: string[] = [];
  const done = executePlanGate("approve", "p_1", () => pending, (o) => phases.push(o.phase));
  expect(phases).toEqual(["pending"]);
  resolve("ok-body");
  await done;
  expect(phases).toEqual(["pending", "ok"]);
});

test("executePlanGate reports pending then error from a REST failure", async () => {
  const seen: string[] = [];
  const err = Object.assign(new Error("plan is not pending approval"), { status: 409 });
  const result = await executePlanGate(
    "approve",
    "p_1",
    async () => {
      throw err;
    },
    (o) => seen.push(`${o.phase}:${o.message}`),
  );
  expect(result.ok).toBe(false);
  expect(seen[0]).toMatch(/^pending:/);
  expect(seen[1]).toBe(
    "error:Approval failed. POST /plans/p_1/approve returned 409: plan is not pending approval.",
  );
});

test("planGateFailed reads the generated client's HttpResponse error body", () => {
  const httpLike = {
    status: 409,
    error: { code: "conflict", message: "plan is not pending approval" },
  };
  expect(planGateFailed("approve", "p_1", httpLike).message).toBe(
    "Approval failed. POST /plans/p_1/approve returned 409: plan is not pending approval.",
  );
});

test("apply REST failure is named as apply, not as a socket drop", async () => {
  const seen: string[] = [];
  await executePlanGate(
    "apply",
    "p_1",
    async () => {
      throw Object.assign(new Error("secretscan findings"), { status: 409 });
    },
    (o) => seen.push(o.message),
  );
  expect(seen[1]).toBe("Apply failed. POST /plans/p_1/apply returned 409: secretscan findings.");
  expect(seen.join(" ")).not.toMatch(/EPIPE|proxy|websocket|live tail/i);
});

test("clicking Approve success is visible and distinct from a dropped live-tail", () => {
  const html = renderSurfaces({
    gate: planGateSucceeded("approve", "p_1"),
    tail: "reconnecting",
  });
  expect(html).toContain("data-plan-gate=\"approve\"");
  expect(html).toContain("data-plan-gate-phase=\"ok\"");
  expect(html).toContain("Approval sent");
  expect(html).toContain("POST /plans/p_1/approve");
  expect(html).toContain("Plan gate");
  expect(html).toContain("data-live-tail=\"reconnecting\"");
  expect(html).toContain("Live tail:");
  expect(html).toContain("output stream only");
  expect(html).not.toContain("Approval failed");
  expect(html).not.toContain("EPIPE");
  expect(html).not.toContain("ws proxy");
});

test("a dropped live-tail with no REST call is not an approve failure", () => {
  const html = renderSurfaces({ gate: null, tail: "reconnecting" });
  expect(html).toContain("data-live-tail=\"reconnecting\"");
  expect(html).toContain("Live tail:");
  expect(html).toContain("output stream only");
  expect(html).not.toContain("data-plan-gate");
  expect(html).not.toContain("Approval failed");
  expect(html).not.toContain("Approval sent");
  expect(html).not.toContain("Apply failed");
  expect(html).not.toContain("Plan gate");
});

test("approve REST failure is a plan-gate alert, not live-tail copy", () => {
  const html = renderSurfaces({
    gate: planGateFailed("approve", "p_1", Object.assign(new Error("lease expired"), { status: 403 })),
    tail: "open",
  });
  expect(html).toContain("data-plan-gate-phase=\"error\"");
  expect(html).toContain("role=\"alert\"");
  expect(html).toContain("Approval failed");
  expect(html).toContain("POST /plans/p_1/approve");
  expect(html).toContain("returned 403: lease expired");
  expect(html).toContain("data-live-tail=\"open\"");
  expect(html).toContain("output stream");
  expect(html).not.toContain("reconnecting");
  expect(html).not.toContain("EPIPE");
});

test("apply REST failure is a distinct plan-gate alert", () => {
  const html = renderSurfaces({
    gate: planGateFailed("apply", "p_1", Object.assign(new Error("precondition mismatch"), { status: 409 })),
    tail: "reconnecting",
  });
  expect(html).toContain("data-plan-gate=\"apply\"");
  expect(html).toContain("Apply failed");
  expect(html).toContain("POST /plans/p_1/apply");
  expect(html).toContain("precondition mismatch");
  expect(html).toContain("Live tail:");
  expect(html).toContain("output stream only");
});

test("pending approve is an immediate sending acknowledgment", () => {
  const html = renderSurfaces({
    gate: planGatePending("approve", "p_1"),
    tail: "reconnecting",
    busy: "approve",
  });
  expect(html).toContain("Sending approval");
  expect(html).toContain("POST /plans/p_1/approve");
  expect(html).toContain("Sending approval…");
  expect(html).toContain("data-plan-gate-phase=\"pending\"");
});

test("liveTailLabel never uses plan-gate verbs", () => {
  for (const status of ["open", "reconnecting", "closed"] as const) {
    const label = liveTailLabel(status);
    expect(label).toMatch(/output stream/);
    expect(label).not.toMatch(/approv|apply|EPIPE|proxy/i);
  }
});

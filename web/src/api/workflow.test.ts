import { expect, test } from "vitest";
import { Api, PlanStatus } from "@zeroth/api";

test("golden workflow: approve is POST /plans/{id}/approve via the generated client", async () => {
  const calls: Array<{ method: string; url: string; body: unknown }> = [];
  const plan = {
    id: "p_1",
    run_id: "s_1",
    status: PlanStatus.PendingApproval,
    summary: "edit README",
    effects: [{ type: "modify", path: "README.md", diff: "-a\n+b" }],
    created_at: "2026-08-22T00:00:00Z",
    updated_at: "2026-08-22T00:00:00Z",
  };
  const customFetch: typeof fetch = async (input, init) => {
    const url = String(input);
    const method = init?.method ?? "GET";
    const body = init?.body ? JSON.parse(String(init.body)) : undefined;
    calls.push({ method, url, body });
    if (method === "POST" && url.endsWith("/plans/p_1/approve")) {
      return json({ ...plan, status: PlanStatus.Approved, review_comment: body?.comment });
    }
    if (method === "POST" && url.endsWith("/plans/p_1/apply")) {
      return json({ plan: { ...plan, status: PlanStatus.Applied }, audit_id: "aud_1" });
    }
    if (method === "POST" && url.endsWith("/audit/aud_1/verify")) {
      return json({ id: "aud_1", valid: true });
    }
    throw new Error(`unexpected ${method} ${url}`);
  };
  const api = new Api<unknown>({ baseUrl: "http://127.0.0.1:8420", customFetch });
  const approved = await api.plans.approvePlan("p_1", { comment: "approved in UI" });
  expect(approved.data.status).toBe(PlanStatus.Approved);
  const applied = await api.plans.applyPlan("p_1");
  expect(applied.data.audit_id).toBe("aud_1");
  const verified = await api.audit.verifyAudit("aud_1");
  expect(verified.data.valid).toBe(true);
  expect(calls.map((c) => `${c.method} ${c.url}`)).toEqual([
    "POST http://127.0.0.1:8420/plans/p_1/approve",
    "POST http://127.0.0.1:8420/plans/p_1/apply",
    "POST http://127.0.0.1:8420/audit/aud_1/verify",
  ]);
  expect(calls[0]?.body).toEqual({ comment: "approved in UI" });
});

function json(data: unknown): Response {
  return new Response(JSON.stringify(data), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

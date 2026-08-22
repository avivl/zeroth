import { errorMessage } from "./api/client";

/**
 * First-class UI state for the plan-gate REST calls (approve, then apply).
 *
 * This is deliberately independent of the run-events live-tail WebSocket.
 * A dropped proxy socket, Vite HMR hiccup, or reconnecting tail must never
 * be presented as an approve/apply outcome, and a real REST failure must
 * never hide in that stream's status text.
 */
export type PlanGateKind = "approve" | "apply";

export type PlanGatePhase = "pending" | "ok" | "error";

export type PlanGateOutcome = {
  kind: PlanGateKind;
  phase: PlanGatePhase;
  planId: string;
  /** Operator-facing sentence naming the REST call and its result. */
  message: string;
};

export function planGatePath(kind: PlanGateKind, planId: string): string {
  return kind === "approve" ? `POST /plans/${planId}/approve` : `POST /plans/${planId}/apply`;
}

export function planGatePending(kind: PlanGateKind, planId: string): PlanGateOutcome {
  const path = planGatePath(kind, planId);
  return {
    kind,
    phase: "pending",
    planId,
    message:
      kind === "approve"
        ? `Sending approval. Waiting for ${path}.`
        : `Sending apply. Waiting for ${path}.`,
  };
}

export function planGateSucceeded(kind: PlanGateKind, planId: string): PlanGateOutcome {
  const path = planGatePath(kind, planId);
  return {
    kind,
    phase: "ok",
    planId,
    message:
      kind === "approve"
        ? `Approval sent. ${path} succeeded. The plan is eligible to apply; this did not mutate the world.`
        : `Apply succeeded. ${path} completed.`,
  };
}

export function planGateFailed(kind: PlanGateKind, planId: string, err: unknown): PlanGateOutcome {
  const path = planGatePath(kind, planId);
  const verb = kind === "approve" ? "Approval" : "Apply";
  return {
    kind,
    phase: "error",
    planId,
    message: `${verb} failed. ${path} ${restFailureDetail(err)}.`,
  };
}

export type PlanGateResult<T> = { ok: true; value: T } | { ok: false; error: unknown };

/**
 * Drive a plan-gate REST call into UI state. `report` is invoked
 * synchronously with pending before the request, then with ok or error
 * from that request's real outcome. Stream status is not an input.
 */
export async function executePlanGate<T>(
  kind: PlanGateKind,
  planId: string,
  request: () => Promise<T>,
  report: (outcome: PlanGateOutcome) => void,
): Promise<PlanGateResult<T>> {
  report(planGatePending(kind, planId));
  try {
    const value = await request();
    report(planGateSucceeded(kind, planId));
    return { ok: true, value };
  } catch (error) {
    report(planGateFailed(kind, planId, error));
    return { ok: false, error };
  }
}

function restFailureDetail(err: unknown): string {
  const msg = errorMessage(err);
  if (err && typeof err === "object" && "status" in err) {
    const status = (err as { status: unknown }).status;
    if (typeof status === "number" && status >= 400) {
      return `returned ${status}: ${msg}`;
    }
  }
  return msg;
}

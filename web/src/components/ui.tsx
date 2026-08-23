import type { ReactNode } from "react";
import type { CrossExam, PlanEffect, PlanEffectType, RunStatus } from "@zeroth/api";
import { PlanEffectType as Effect } from "@zeroth/api";
import { liveTailLabel, type StreamStatus } from "../api/runEvents";
import type { PlanGateOutcome } from "../planGate";

export function Badge({
  kind,
  children,
}: {
  kind: string;
  children: ReactNode;
}) {
  const cls = kind.replace(/[^a-z0-9_]+/gi, "_").toLowerCase();
  return <span className={`badge badge-${cls}`}>{children}</span>;
}

export function statusLabel(status: RunStatus | string): string {
  return String(status).replaceAll("_", " ");
}

export function SignatureChip({
  signature,
  valid,
  onVerify,
  busy,
}: {
  signature: string;
  valid?: boolean;
  onVerify: () => void;
  busy?: boolean;
}) {
  const short = signature.length > 16 ? `${signature.slice(0, 10)}…${signature.slice(-6)}` : signature;
  return (
    <span className="sig-chip">
      <span className="mono">{short}</span>
      {valid === true ? <span>valid</span> : null}
      {valid === false ? <span>invalid</span> : null}
      <button type="button" onClick={onVerify} disabled={busy} aria-label="Verify signature">
        Verify
      </button>
    </span>
  );
}

export function CrossExamNotes({ exam }: { exam?: CrossExam | null }) {
  if (!exam) {
    return null;
  }
  return (
    <section className="card" aria-label="Cross-exam">
      <h3>Cross-exam</h3>
      <p>
        <Badge kind={exam.verdict}>{exam.verdict}</Badge>
        <span className="muted"> · {exam.reviewer_model}</span>
      </p>
      {exam.reasoning ? <p>{exam.reasoning}</p> : <p className="muted">No notes</p>}
    </section>
  );
}

export function Thinking({
  events,
}: {
  events: Array<{ id: string; type: string; message?: string; created_at: string }>;
}) {
  if (events.length === 0) {
    return <p className="muted">No trace yet.</p>;
  }
  return (
    <ol className="stack" style={{ listStyle: "none", padding: 0, margin: 0 }}>
      {events.map((ev) => (
        <li key={ev.id}>
          <details className="thinking-step" open={ev.type === "log" || ev.type === "plan_drafted"}>
            <summary>
              <Badge kind={ev.type}>{ev.type.replaceAll("_", " ")}</Badge>
              {ev.type === "tool_call" ? (
                <span className="tool-chip">{ev.message ?? "tool"}</span>
              ) : null}
              <span className="muted"> {new Date(ev.created_at).toLocaleTimeString()}</span>
            </summary>
            {ev.message ? <p className="stream">{ev.message}</p> : null}
          </details>
        </li>
      ))}
    </ol>
  );
}

export function StreamingText({
  text,
  status,
}: {
  text: string;
  status?: StreamStatus;
}) {
  const label = status ? liveTailLabel(status) : undefined;
  return (
    <section className="card" aria-live="polite" aria-label="Live output">
      <div className="row">
        <h3 style={{ margin: 0 }}>Live output</h3>
        {label ? (
          <span className="live-tail-status muted" data-live-tail={status}>
            Live tail: {label}
          </span>
        ) : null}
      </div>
      <pre className="stream">{text || "Waiting for tokens…"}</pre>
    </section>
  );
}

export function PlanGateBanner({ outcome }: { outcome: PlanGateOutcome }) {
  const title = outcome.kind === "approve" ? "Approve" : "Apply";
  const role = outcome.phase === "error" ? "alert" : "status";
  return (
    <div
      className={`plan-gate plan-gate-${outcome.phase}`}
      role={role}
      aria-live={outcome.phase === "error" ? "assertive" : "polite"}
      aria-busy={outcome.phase === "pending" ? true : undefined}
      data-plan-gate={outcome.kind}
      data-plan-gate-phase={outcome.phase}
    >
      <strong className="plan-gate-label">Plan gate · {title}</strong>
      <p>{outcome.message}</p>
    </div>
  );
}

export function DiffTable({ effects }: { effects: PlanEffect[] }) {
  if (effects.length === 0) {
    return <p className="muted">No resource diffs.</p>;
  }
  return (
    <div className="stack">
      {effects.map((row, i) => (
        <EffectRow key={`${row.type}-${row.path ?? i}`} effect={row} />
      ))}
    </div>
  );
}

function EffectRow({ effect }: { effect: PlanEffect }) {
  const destructive = effect.type === Effect.Destroy;
  return (
    <details className="card">
      <summary className="row">
        <Badge kind={effect.type}>{effect.type.replaceAll("_", " ")}</Badge>
        <span className={`id ${destructive ? "error" : ""}`}>{effect.path ?? "unpathed"}</span>
        {effect.lease_id ? <span className="id muted">lease {effect.lease_id}</span> : null}
      </summary>
      {effect.diff ? (
        <pre className="diff">
          {effect.diff.split("\n").map((line, i) => (
            <span key={i} className={line.startsWith("+") ? "add" : line.startsWith("-") ? "del" : ""}>
              {line}
              {"\n"}
            </span>
          ))}
        </pre>
      ) : (
        <p className="muted">No diff payload.</p>
      )}
      {effect.lease_id ? <p className="id muted">lease {effect.lease_id}</p> : null}
    </details>
  );
}

export function effectKind(type: PlanEffectType): string {
  return type;
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="empty card">{children}</div>;
}

export function ErrorText({ children }: { children: ReactNode }) {
  return (
    <p className="error" role="alert">
      {children}
    </p>
  );
}

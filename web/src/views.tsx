import { useCallback, useEffect, useState } from "react";
import type { Client } from "./api/client";
import {
  ApprovalStatus,
  AuditResourceType,
  MemoryProposalStatus,
  PlanStatus,
  type AgentPatch,
  type Approval,
  type AuditRecord,
  type AuditVerification,
  type Checkpoint,
  type CrossExamStats,
  type MemoryEntry,
  type MemoryProposal,
  type Plan,
} from "@zeroth/api";
import { errorMessage } from "./api/client";
import { useRunEvents } from "./api/useRunEvents";
import {
  Badge,
  CrossExamNotes,
  DiffTable,
  Empty,
  ErrorText,
  PlanGateBanner,
  SignatureChip,
  StreamingText,
  Thinking,
  statusLabel,
} from "./components/ui";
import { executePlanGate, type PlanGateOutcome } from "./planGate";
import { hrefFor } from "./routes";

function useLoad<T>(loader: () => Promise<T>, deps: unknown[]): {
  data: T | null;
  error: string | null;
  loading: boolean;
  reload: () => void;
} {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [tick, setTick] = useState(0);
  const reload = useCallback(() => setTick((n) => n + 1), []);
  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    loader()
      .then((value) => {
        if (!cancelled) {
          setData(value);
          setError(null);
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setError(errorMessage(err));
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
    // loader identity is the caller's deps.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tick, ...deps]);
  return { data, error, loading, reload };
}

export function RunsView({ api }: { api: Client }) {
  const { data, error, loading } = useLoad(async () => {
    const res = await api.runs.listRuns({ limit: 50 });
    return res.data.items;
  }, [api]);
  return (
    <section>
      <div className="page-head">
        <div>
          <h2>Runs</h2>
          <p className="muted">Human-supervised sessions. Status is a replay of the event log.</p>
        </div>
      </div>
      {error ? <ErrorText>{error}</ErrorText> : null}
      {loading && !data ? <p className="muted">Loading…</p> : null}
      {data && data.length === 0 ? <Empty>No runs yet. Start one from the CLI with `zeroth run`.</Empty> : null}
      <div className="stack">
        {(data ?? []).map((run) => (
          <a key={run.id} className="card task-row" href={hrefFor({ name: "run", id: run.id })}>
            <div>
              <div className="id">{run.id}</div>
              <div className="muted">{run.prompt ?? "no prompt"}</div>
            </div>
            <Badge kind={run.status}>{statusLabel(run.status)}</Badge>
            <span className="muted">{new Date(run.created_at).toLocaleString()}</span>
          </a>
        ))}
      </div>
    </section>
  );
}

export function RunDetailView({ api, id }: { api: Client; id: string }) {
  const { data: run, error, reload } = useLoad(async () => (await api.runs.getRun(id)).data, [api, id]);
  const { data: plans, reload: reloadPlans } = useLoad(async () => {
    const res = await api.plans.listPlans({ run_id: id, limit: 20 });
    return res.data.items;
  }, [api, id]);
  const { data: checkpoints, reload: reloadCk } = useLoad(async () => {
    const res = await api.checkpoints.listCheckpoints({ run_id: id, limit: 50 });
    return res.data.items;
  }, [api, id]);
  const { data: proposals, reload: reloadMem } = useLoad(async () => {
    const res = await api.memory.listMemoryProposals({ status: MemoryProposalStatus.Pending, limit: 50 });
    return res.data.items.filter((p) => p.run_id === id);
  }, [api, id]);
  const { data: agent } = useLoad(async () => {
    if (!run) {
      return null;
    }
    const [a, stats] = await Promise.all([
      api.agents.getAgent(run.agent_id),
      api.agents.getAgentCrossExamStats(run.agent_id),
    ]);
    return { agent: a.data, stats: stats.data };
  }, [api, run?.agent_id]);
  const { events, status, text } = useRunEvents(id);
  const [busy, setBusy] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [planGate, setPlanGate] = useState<PlanGateOutcome | null>(null);
  const [auditByPlan, setAuditByPlan] = useState<AuditRecord | null>(null);
  const [auditValid, setAuditValid] = useState<boolean | undefined>(undefined);

  const plan = (plans ?? []).find((p) => p.id === run?.plan_id) ?? (plans ?? [])[0];

  function refresh() {
    reload();
    reloadPlans();
    reloadCk();
    reloadMem();
  }

  async function act(name: string, fn: () => Promise<void>) {
    setBusy(name);
    setActionError(null);
    setPlanGate(null);
    try {
      await fn();
      refresh();
    } catch (err) {
      setActionError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  }

  return (
    <section>
      <div className="page-head">
        <div>
          <h2>Run</h2>
          <p className="id muted">{id}</p>
        </div>
        {run ? <Badge kind={run.status}>{statusLabel(run.status)}</Badge> : null}
      </div>
      {error ? <ErrorText>{error}</ErrorText> : null}
      {actionError ? <ErrorText>{actionError}</ErrorText> : null}
      <div className="rail">
        <div className="stack">
          <StreamingText text={text} status={status} />
          <section className="card">
            <h3>Trace</h3>
            <Thinking events={events} />
          </section>
          {plan ? (
            <ChangePlanCard
              plan={plan}
              audit={auditByPlan}
              auditValid={auditValid}
              busy={busy}
              gate={planGate && planGate.planId === plan.id ? planGate : null}
              onApprove={() =>
                void (async () => {
                  setBusy("approve");
                  setActionError(null);
                  try {
                    const result = await executePlanGate(
                      "approve",
                      plan.id,
                      () => api.plans.approvePlan(plan.id, { comment: "approved in UI" }),
                      setPlanGate,
                    );
                    if (result.ok) {
                      refresh();
                    }
                  } catch (err) {
                    setActionError(errorMessage(err));
                  } finally {
                    setBusy(null);
                  }
                })()
              }
              onApply={() =>
                void (async () => {
                  setBusy("apply");
                  setActionError(null);
                  setAuditValid(undefined);
                  try {
                    const result = await executePlanGate(
                      "apply",
                      plan.id,
                      () => api.plans.applyPlan(plan.id),
                      setPlanGate,
                    );
                    if (result.ok) {
                      try {
                        const list = await api.audit.listAudit({ resource_id: plan.id, limit: 5 });
                        setAuditByPlan(
                          list.data.items.find((r) => r.id === result.value.data.audit_id) ??
                            list.data.items[0] ??
                            null,
                        );
                      } catch (err) {
                        setActionError(errorMessage(err));
                      }
                      refresh();
                    }
                  } catch (err) {
                    setActionError(errorMessage(err));
                  } finally {
                    setBusy(null);
                  }
                })()
              }
              onChanges={() =>
                act("changes", async () => {
                  await api.plans.requestPlanChanges(plan.id, { comment: "please revise" });
                })
              }
              onBranch={() =>
                act("branch", async () => {
                  await api.plans.branchPlan(plan.id, { note: "branch from UI" });
                })
              }
              onVerify={
                auditByPlan
                  ? () =>
                      act("verify", async () => {
                        const v = await api.audit.verifyAudit(auditByPlan.id);
                        setAuditValid(v.data.valid);
                        if (!v.data.valid) {
                          throw new Error(v.data.reason ?? "invalid signature");
                        }
                      })
                  : undefined
              }
            />
          ) : (
            <Empty>No change plan on this run yet.</Empty>
          )}
        </div>
        <aside className="stack">
          <EvalGates plan={plan} stats={agent?.stats} />
          <CheckpointRail
            items={checkpoints ?? []}
            busy={busy}
            onRestore={(ck) =>
              act("restore", async () => {
                const forked = await api.checkpoints.restoreCheckpoint(ck.id);
                window.location.hash = hrefFor({ name: "run", id: forked.data.id });
              })
            }
          />
          <MemoryQueue
            items={proposals ?? []}
            busy={busy}
            onAccept={(p) => act("accept", async () => { await api.memory.acceptMemoryProposal(p.id); })}
            onReject={(p) => act("reject", async () => { await api.memory.rejectMemoryProposal(p.id); })}
          />
        </aside>
      </div>
    </section>
  );
}

function EvalGates({ plan, stats }: { plan?: Plan; stats?: CrossExamStats }) {
  const exam = plan?.cross_exam;
  const findings = plan?.secret_scan_findings ?? [];
  return (
    <section className="card">
      <h3>Eval gates</h3>
      <p className="row">
        <span>Cross-exam</span>
        <Badge kind={exam?.verdict ?? "pending"}>{exam?.verdict ?? "pending"}</Badge>
      </p>
      <p className="row">
        <span>Secretscan</span>
        <Badge kind={findings.length ? "danger" : "ok"}>{findings.length ? `${findings.length} finding` : "clean"}</Badge>
      </p>
      {stats ? (
        <p className="muted">
          Agent pass rate {(stats.pass_rate * 100).toFixed(0)}% of {stats.examined}
        </p>
      ) : null}
    </section>
  );
}

function CheckpointRail({
  items,
  busy,
  onRestore,
}: {
  items: Checkpoint[];
  busy: string | null;
  onRestore: (ck: Checkpoint) => void;
}) {
  return (
    <section className="card">
      <h3>Checkpoints</h3>
      {items.length === 0 ? <p className="muted">None yet.</p> : null}
      <ol className="stack" style={{ listStyle: "none", padding: 0, margin: 0 }}>
        {items.map((ck) => (
          <li key={ck.id} className="row">
            <span className="id">{ck.label ?? ck.id}</span>
            <button type="button" className="btn" disabled={busy !== null} onClick={() => onRestore(ck)}>
              Restore
            </button>
          </li>
        ))}
      </ol>
    </section>
  );
}

function MemoryQueue({
  items,
  busy,
  onAccept,
  onReject,
}: {
  items: MemoryProposal[];
  busy: string | null;
  onAccept: (p: MemoryProposal) => void;
  onReject: (p: MemoryProposal) => void;
}) {
  return (
    <section className="card">
      <h3>Memory queue</h3>
      {items.length === 0 ? <p className="muted">No pending proposals.</p> : null}
      {items.map((p) => (
        <div key={p.id} className="stack">
          <p>{p.content}</p>
          <div className="row">
            <button type="button" className="btn btn-primary" disabled={busy !== null} onClick={() => onAccept(p)}>
              Accept
            </button>
            <button type="button" className="btn btn-danger" disabled={busy !== null} onClick={() => onReject(p)}>
              Reject
            </button>
          </div>
        </div>
      ))}
    </section>
  );
}

export function ChangePlanCard({
  plan,
  audit,
  auditValid,
  busy,
  gate,
  onApprove,
  onApply,
  onChanges,
  onBranch,
  onVerify,
}: {
  plan: Plan;
  audit?: AuditRecord | null;
  auditValid?: boolean;
  busy: string | null;
  gate?: PlanGateOutcome | null;
  onApprove: () => void;
  onApply: () => void;
  onChanges: () => void;
  onBranch: () => void;
  onVerify?: () => void;
}) {
  const canApprove = plan.status === PlanStatus.PendingApproval;
  const canApply = plan.status === PlanStatus.Approved;
  return (
    <section className="card" aria-label="Change plan">
      <div className="row">
        <h3 style={{ margin: 0 }}>Change plan</h3>
        <Badge kind={plan.status}>{statusLabel(plan.status)}</Badge>
      </div>
      <p>{plan.summary}</p>
      {plan.hash ? <p className="id muted">hash {plan.hash}</p> : null}
      {plan.expires_at ? <p className="muted">expires {new Date(plan.expires_at).toLocaleString()}</p> : null}
      <DiffTable effects={plan.effects} />
      <CrossExamNotes exam={plan.cross_exam} />
      <div className="row" style={{ marginTop: "0.75rem" }}>
        <button type="button" className="btn btn-primary" disabled={!canApprove || busy !== null} onClick={onApprove}>
          {busy === "approve" ? "Sending approval…" : "Approve"}
        </button>
        <button type="button" className="btn" disabled={!canApply || busy !== null} onClick={onApply}>
          {busy === "apply" ? "Sending apply…" : "Apply"}
        </button>
        <button type="button" className="btn" disabled={busy !== null} onClick={onChanges}>
          Request changes
        </button>
        <button type="button" className="btn" disabled={busy !== null} onClick={onBranch}>
          Branch
        </button>
      </div>
      {gate ? <PlanGateBanner outcome={gate} /> : null}
      {audit ? (
        <p className="row" style={{ marginTop: "0.75rem" }}>
          <span className="muted">Signed apply</span>
          <SignatureChip signature={audit.signature} valid={auditValid} onVerify={() => onVerify?.()} />
        </p>
      ) : null}
    </section>
  );
}

export function AgentsView({ api }: { api: Client }) {
  const { data, error } = useLoad(async () => {
    const list = await api.agents.listAgents({ limit: 50 });
    const rows = await Promise.all(
      list.data.items.map(async (agent) => {
        const [stats, runs] = await Promise.all([
          api.agents.getAgentCrossExamStats(agent.id),
          api.runs.listRuns({ agent_id: agent.id, limit: 50 }),
        ]);
        return { agent, stats: stats.data, runs: runs.data.items.length };
      }),
    );
    return rows;
  }, [api]);
  return (
    <section>
      <div className="page-head">
        <div>
          <h2>Agents</h2>
          <p className="muted">Local records. Tiers are earned; they do not skip the kernel.</p>
        </div>
      </div>
      {error ? <ErrorText>{error}</ErrorText> : null}
      {data && data.length === 0 ? <Empty>No agents.</Empty> : null}
      <div className="stack">
        {(data ?? []).map(({ agent, stats, runs }) => (
          <a key={agent.id} className="card task-row" href={hrefFor({ name: "agent", id: agent.id })}>
            <div>
              <strong>{agent.name}</strong>
              <div className="id muted">{agent.id}</div>
              <div className="meter" aria-label="eval pass meter">
                <span style={{ width: `${Math.round(stats.pass_rate * 100)}%` }} />
              </div>
            </div>
            <Badge kind={agent.autonomy_tier ?? "t1"}>tier {agent.autonomy_tier ?? "1"}</Badge>
            <span className="muted">{runs} runs</span>
          </a>
        ))}
      </div>
    </section>
  );
}

const TIER_UNLOCK =
  "Tiers 3 and 4 stay locked until eval gates pass (cross-exam pass rate and secretscan clean applies) and the operator explicitly promotes. Stage 1 never auto-promotes.";

export function AgentConfigView({ api, id }: { api: Client; id: string }) {
  const { data, error, reload } = useLoad(async () => {
    const [agent, leases, audit] = await Promise.all([
      api.agents.getAgent(id),
      api.agents.listAgentLeases(id),
      api.audit.listAudit({ resource_type: AuditResourceType.Agent, resource_id: id, limit: 20 }),
    ]);
    const pubkey = audit.data.items.find((r) => r.agent_pubkey)?.agent_pubkey;
    return { agent: agent.data, leases: leases.data.items, pubkey };
  }, [api, id]);
  const [draft, setDraft] = useState<AgentPatch>({});
  const [busy, setBusy] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const agent = data?.agent;
  const name = draft.name ?? agent?.name ?? "";
  const model = draft.model ?? agent?.model ?? "";
  const tier = draft.autonomy_tier ?? agent?.autonomy_tier ?? "t1";
  const tools = draft.tools ?? agent?.tools ?? [];

  async function save() {
    setBusy(true);
    setSaveError(null);
    try {
      await api.agents.patchAgent(id, {
        name,
        model: model || undefined,
        autonomy_tier: tier,
        tools,
        reviewer: draft.reviewer ?? agent?.reviewer,
      });
      setDraft({});
      reload();
    } catch (err) {
      setSaveError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section>
      <div className="page-head">
        <div>
          <h2>Agent configuration</h2>
          <p className="muted">A config change is itself a signed audited action.</p>
        </div>
        {agent ? <Badge kind={agent.status}>{agent.status}</Badge> : null}
      </div>
      {error ? <ErrorText>{error}</ErrorText> : null}
      {saveError ? <ErrorText>{saveError}</ErrorText> : null}
      {agent ? (
        <div className="stack">
          <section className="card stack">
            <h3>Identity</h3>
            <label>
              Name
              <input value={name} onChange={(e) => setDraft((d) => ({ ...d, name: e.target.value }))} />
            </label>
            <p className="id muted">id {agent.id}</p>
            <p className="id muted">signing key {data?.pubkey ?? "not yet on the audit chain"}</p>
            <p>Harness {agent.harness}</p>
            <label>
              Model
              <input value={model} onChange={(e) => setDraft((d) => ({ ...d, model: e.target.value }))} />
            </label>
          </section>
          <section className="card stack">
            <h3>Autonomy tiers</h3>
            {["t1", "t2", "t3", "t4"].map((t) => {
              const locked = t === "t3" || t === "t4";
              return (
                <label key={t} className={locked ? "locked" : ""}>
                  <input
                    type="radio"
                    name="tier"
                    checked={tier === t}
                    disabled={locked}
                    onChange={() => setDraft((d) => ({ ...d, autonomy_tier: t }))}
                  />{" "}
                  {t}
                  {locked ? <span className="muted"> locked</span> : null}
                </label>
              );
            })}
            <p className="muted">{TIER_UNLOCK}</p>
          </section>
          <section className="card stack">
            <h3>Capabilities and leases</h3>
            {(agent.tools ?? []).length === 0 ? <p className="muted">No capability toggles on this agent.</p> : null}
            {(agent.tools ?? []).map((tool) => (
              <label key={tool}>
                <input
                  type="checkbox"
                  checked={tools.includes(tool)}
                  onChange={(e) => {
                    const next = e.target.checked ? [...tools, tool] : tools.filter((x) => x !== tool);
                    setDraft((d) => ({ ...d, tools: next }));
                  }}
                />{" "}
                {tool}
              </label>
            ))}
            {(data?.leases ?? []).map((lease) => (
              <p key={lease.id} className="id muted">
                lease {lease.id} expires {new Date(lease.expires_at).toLocaleString()}
              </p>
            ))}
            {(data?.leases ?? []).length === 0 ? <p className="muted">No live leases. Apply mints them.</p> : null}
          </section>
          <section className="card stack">
            <h3>Guardrails</h3>
            <label>
              Reviewer model
              <input
                value={draft.reviewer?.model ?? agent.reviewer?.model ?? ""}
                onChange={(e) =>
                  setDraft((d) => ({
                    ...d,
                    reviewer: { ...(d.reviewer ?? agent.reviewer), model: e.target.value },
                  }))
                }
              />
            </label>
            <label>
              <input
                type="checkbox"
                checked={draft.reviewer?.block_on_fail ?? agent.reviewer?.block_on_fail ?? false}
                onChange={(e) =>
                  setDraft((d) => ({
                    ...d,
                    reviewer: { ...(d.reviewer ?? agent.reviewer), block_on_fail: e.target.checked },
                  }))
                }
              />{" "}
              Block on fail (return notes to the agent instead of escalating)
            </label>
          </section>
          <div className="sticky-save">
            <span className="muted">Saving patches the agent and appends a signed audit record. The client does not send a signature.</span>
            <button type="button" className="btn btn-primary" disabled={busy} onClick={() => void save()}>
              Save
            </button>
          </div>
        </div>
      ) : null}
    </section>
  );
}

export function ApprovalsView({ api }: { api: Client }) {
  const { data, error } = useLoad(async () => {
    const res = await api.approvals.listApprovals({ status: ApprovalStatus.Pending, limit: 50 });
    return res.data.items;
  }, [api]);
  return (
    <section>
      <div className="page-head">
        <div>
          <h2>Approvals</h2>
          <p className="muted">Decide on the subject resource. This inbox does not approve by itself.</p>
        </div>
      </div>
      {error ? <ErrorText>{error}</ErrorText> : null}
      {data && data.length === 0 ? <Empty>Inbox is clear.</Empty> : null}
      <div className="stack">
        {(data ?? []).map((item) => (
          <ApprovalRow key={item.id} item={item} />
        ))}
      </div>
    </section>
  );
}

function ApprovalRow({ item }: { item: Approval }) {
  const href = item.plan_id
    ? hrefFor({ name: "run", id: item.run_id ?? "" })
    : item.run_id
      ? hrefFor({ name: "run", id: item.run_id })
      : "#/approvals";
  return (
    <a className="card task-row" href={item.run_id ? href : "#/approvals"}>
      <div>
        <div>{item.summary ?? item.kind}</div>
        <div className="id muted">{item.id}</div>
      </div>
      <Badge kind={item.kind}>{item.kind}</Badge>
      <Badge kind={item.status}>{item.status.replaceAll("_", " ")}</Badge>
    </a>
  );
}

export function MemoryView({ api }: { api: Client }) {
  const { data, error, reload } = useLoad(async () => {
    const [pending, accepted] = await Promise.all([
      api.memory.listMemoryProposals({ status: MemoryProposalStatus.Pending, limit: 50 }),
      api.memory.listMemory({ limit: 50 }),
    ]);
    return { pending: pending.data.items, accepted: accepted.data.items };
  }, [api]);
  const [busy, setBusy] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);
  async function review(id: string, accept: boolean) {
    setBusy(id);
    setErr(null);
    try {
      if (accept) {
        await api.memory.acceptMemoryProposal(id);
      } else {
        await api.memory.rejectMemoryProposal(id);
      }
      reload();
    } catch (e) {
      setErr(errorMessage(e));
    } finally {
      setBusy(null);
    }
  }
  return (
    <section>
      <div className="page-head">
        <div>
          <h2>Project memory</h2>
          <p className="muted">Pending proposals plus accepted entries. Agent-proposed memory is never auto-written.</p>
        </div>
      </div>
      {error || err ? <ErrorText>{error ?? err}</ErrorText> : null}
      <h3>Pending</h3>
      {(data?.pending ?? []).length === 0 ? <Empty>No pending proposals.</Empty> : null}
      <div className="stack">
        {(data?.pending ?? []).map((p) => (
          <article key={p.id} className="card stack">
            <Badge kind="memory">{p.kind}</Badge>
            <p>{p.content}</p>
            <div className="row">
              <button type="button" className="btn btn-primary" disabled={busy !== null} onClick={() => void review(p.id, true)}>
                Accept
              </button>
              <button type="button" className="btn btn-danger" disabled={busy !== null} onClick={() => void review(p.id, false)}>
                Reject
              </button>
            </div>
          </article>
        ))}
      </div>
      <h3>Accepted</h3>
      {(data?.accepted ?? []).length === 0 ? <Empty>No accepted entries.</Empty> : null}
      <div className="stack">
        {(data?.accepted ?? []).map((m: MemoryEntry) => (
          <article key={m.id} className="card">
            <Badge kind="memory">{m.kind}</Badge>
            <p>{m.content}</p>
            <p className="id muted">{m.id}</p>
          </article>
        ))}
      </div>
    </section>
  );
}

export function AuditView({ api }: { api: Client }) {
  const { data, error } = useLoad(async () => (await api.audit.listAudit({ limit: 50 })).data.items, [api]);
  const [results, setResults] = useState<Record<string, AuditVerification>>({});
  const [busy, setBusy] = useState<string | null>(null);
  async function verify(id: string) {
    setBusy(id);
    try {
      const res = await api.audit.verifyAudit(id);
      setResults((prev) => ({ ...prev, [id]: res.data }));
    } finally {
      setBusy(null);
    }
  }
  return (
    <section>
      <div className="page-head">
        <div>
          <h2>Audit log</h2>
          <p className="muted">Append-only Schnorr signatures. Verify re-checks one row; `zeroth verify` walks the chain offline.</p>
        </div>
      </div>
      {error ? <ErrorText>{error}</ErrorText> : null}
      {data && data.length === 0 ? <Empty>No audit records.</Empty> : null}
      <div className="stack">
        {(data ?? []).map((rec) => (
          <article key={rec.id} className="card row" style={{ justifyContent: "space-between" }}>
            <div>
              <strong>{rec.action}</strong>
              <div className="id muted">
                {rec.resource_type} {rec.resource_id}
              </div>
            </div>
            <SignatureChip
              signature={rec.signature}
              valid={results[rec.id]?.valid}
              busy={busy === rec.id}
              onVerify={() => void verify(rec.id)}
            />
          </article>
        ))}
      </div>
    </section>
  );
}


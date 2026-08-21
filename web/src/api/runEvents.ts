import type { RunEvent } from "@zeroth/api";

/**
 * Thin WebSocket client for GET /runs/{id}/events.
 *
 * OpenAPI codegen emits an HTTP GET helper for this path, not a socket
 * client. This wrapper is the web UI's caller. The contract is still
 * pkg/api/openapi.yaml (RunEvent JSON frames after a replay of `last` N).
 */
export type RunEventsHandlers = {
  onEvent: (event: RunEvent) => void;
  onError?: (error: unknown) => void;
  last?: number;
};

export type RunEventSubscription = {
  close: () => void;
};

export function runEventsUrl(httpOrigin: string, runId: string, last?: number): string {
  const trimmed = httpOrigin.replace(/\/+$/, "");
  const wsOrigin = trimmed.replace(/^http/i, "ws");
  const url = new URL(`${wsOrigin}/runs/${encodeURIComponent(runId)}/events`);
  if (last !== undefined) {
    url.searchParams.set("last", String(last));
  }
  return url.toString();
}

export function parseRunEvent(data: string): RunEvent {
  let value: unknown;
  try {
    value = JSON.parse(data);
  } catch (err) {
    throw new Error("run event: invalid JSON", { cause: err });
  }
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error("run event: payload is not an object");
  }
  const rec = value as Record<string, unknown>;
  if (
    typeof rec.id !== "string" ||
    typeof rec.run_id !== "string" ||
    typeof rec.type !== "string" ||
    typeof rec.created_at !== "string"
  ) {
    throw new Error("run event: missing required fields");
  }
  return value as RunEvent;
}

export function subscribeRunEvents(
  httpOrigin: string,
  runId: string,
  handlers: RunEventsHandlers,
): RunEventSubscription {
  const ws = new WebSocket(runEventsUrl(httpOrigin, runId, handlers.last));
  ws.addEventListener("message", (ev: MessageEvent<unknown>) => {
    if (typeof ev.data !== "string") {
      handlers.onError?.(new Error("run event: non-text frame"));
      return;
    }
    try {
      handlers.onEvent(parseRunEvent(ev.data));
    } catch (err) {
      handlers.onError?.(err);
    }
  });
  ws.addEventListener("error", (ev) => {
    handlers.onError?.(ev);
  });
  return {
    close: () => {
      ws.close();
    },
  };
}

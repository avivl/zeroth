import type { RunEvent } from "@zeroth/api";

/**
 * Thin WebSocket client for GET /runs/{id}/events.
 *
 * OpenAPI codegen emits an HTTP GET helper for this path, not a socket
 * client. This wrapper is the web UI's caller. The contract is still
 * pkg/api/openapi.yaml (RunEvent JSON frames after a replay of `last` N).
 *
 * After a drop the client reconnects and replays `last` events. Callers
 * must tolerate duplicate ids from the replay window.
 */
export type StreamStatus = "open" | "reconnecting" | "closed";

export type RunEventsHandlers = {
  onEvent: (event: RunEvent) => void;
  onError?: (error: unknown) => void;
  onStatus?: (status: StreamStatus) => void;
  last?: number;
  reconnect?: boolean;
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

const DEFAULT_LAST = 50;

export function subscribeRunEvents(
  httpOrigin: string,
  runId: string,
  handlers: RunEventsHandlers,
): RunEventSubscription {
  const reconnect = handlers.reconnect !== false;
  const last = handlers.last ?? DEFAULT_LAST;
  let closed = false;
  let attempt = 0;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let ws: WebSocket | null = null;
  const seen = new Set<string>();

  const connect = () => {
    if (closed) {
      return;
    }
    if (attempt > 0) {
      handlers.onStatus?.("reconnecting");
    }
    const socket = new WebSocket(runEventsUrl(httpOrigin, runId, last));
    ws = socket;
    socket.addEventListener("open", () => {
      attempt = 0;
      handlers.onStatus?.("open");
    });
    socket.addEventListener("message", (ev: MessageEvent<unknown>) => {
      if (typeof ev.data !== "string") {
        handlers.onError?.(new Error("run event: non-text frame"));
        return;
      }
      try {
        const event = parseRunEvent(ev.data);
        if (seen.has(event.id)) {
          return;
        }
        seen.add(event.id);
        handlers.onEvent(event);
      } catch (err) {
        handlers.onError?.(err);
      }
    });
    socket.addEventListener("error", (ev) => {
      handlers.onError?.(ev);
    });
    socket.addEventListener("close", () => {
      if (closed || !reconnect) {
        if (closed) {
          handlers.onStatus?.("closed");
        }
        return;
      }
      handlers.onStatus?.("reconnecting");
      const delay = Math.min(8000, 250 * 2 ** attempt);
      attempt += 1;
      timer = setTimeout(connect, delay);
    });
  };

  connect();
  return {
    close: () => {
      closed = true;
      if (timer !== undefined) {
        clearTimeout(timer);
      }
      ws?.close();
      handlers.onStatus?.("closed");
    },
  };
}

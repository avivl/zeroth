import { afterEach, expect, test, vi } from "vitest";
import { liveTailLabel, parseRunEvent, runEventsUrl, subscribeRunEvents } from "./runEvents";

afterEach(() => {
  vi.unstubAllGlobals();
});

test("liveTailLabel marks reconnecting as an output-stream drop", () => {
  expect(liveTailLabel("reconnecting")).toBe("reconnecting after drop (output stream only)");
  expect(liveTailLabel("open")).toBe("connected (output stream)");
  expect(liveTailLabel("closed")).toBe("disconnected (output stream)");
});

test("runEventsUrl upgrades http origin and encodes the run id", () => {
  expect(runEventsUrl("http://127.0.0.1:8420", "run/1", 50)).toBe(
    "ws://127.0.0.1:8420/runs/run%2F1/events?last=50",
  );
});

test("runEventsUrl upgrades https to wss and strips a trailing slash", () => {
  expect(runEventsUrl("https://127.0.0.1:8420/", "abc")).toBe(
    "wss://127.0.0.1:8420/runs/abc/events",
  );
});

test("parseRunEvent accepts a generated-schema-shaped frame", () => {
  const event = parseRunEvent(
    JSON.stringify({
      id: "evt-1",
      run_id: "run-1",
      type: "plan_drafted",
      created_at: "2026-08-21T00:00:00Z",
      message: "draft ready",
    }),
  );
  expect(event.type).toBe("plan_drafted");
  expect(event.run_id).toBe("run-1");
});

test("parseRunEvent rejects a non-object payload", () => {
  expect(() => parseRunEvent("[]")).toThrow(/not an object/);
});

test("subscribeRunEvents delivers parsed frames and closes the socket", () => {
  const sockets: FakeWebSocket[] = [];
  class FakeWebSocket {
    readonly url: string;
    readonly listeners = new Map<string, Array<(ev: unknown) => void>>();
    closed = false;
    constructor(url: string) {
      this.url = url;
      sockets.push(this);
    }
    addEventListener(type: string, fn: (ev: unknown) => void) {
      const list = this.listeners.get(type) ?? [];
      list.push(fn);
      this.listeners.set(type, list);
    }
    close() {
      this.closed = true;
      this.emit("close", {});
    }
    emit(type: string, ev: unknown) {
      for (const fn of this.listeners.get(type) ?? []) {
        fn(ev);
      }
    }
  }
  vi.stubGlobal("WebSocket", FakeWebSocket);

  const onEvent = vi.fn();
  const sub = subscribeRunEvents("http://127.0.0.1:8420", "run-9", {
    onEvent,
    last: 20,
  });
  expect(sockets).toHaveLength(1);
  expect(sockets[0]?.url).toBe("ws://127.0.0.1:8420/runs/run-9/events?last=20");

  sockets[0]?.emit("message", {
    data: JSON.stringify({
      id: "evt-2",
      run_id: "run-9",
      type: "log",
      created_at: "2026-08-21T00:00:01Z",
    }),
  });
  expect(onEvent).toHaveBeenCalledTimes(1);
  expect(onEvent.mock.calls[0]?.[0]).toMatchObject({ type: "log", run_id: "run-9" });

  sub.close();
  expect(sockets[0]?.closed).toBe(true);
});

test("subscribeRunEvents reconnects after a drop and dedupes replayed ids", async () => {
  vi.useFakeTimers();
  const sockets: FakeWebSocket[] = [];
  class FakeWebSocket {
    readonly url: string;
    readonly listeners = new Map<string, Array<(ev: unknown) => void>>();
    closed = false;
    constructor(url: string) {
      this.url = url;
      sockets.push(this);
    }
    addEventListener(type: string, fn: (ev: unknown) => void) {
      const list = this.listeners.get(type) ?? [];
      list.push(fn);
      this.listeners.set(type, list);
    }
    close() {
      this.closed = true;
      this.emit("close", {});
    }
    emit(type: string, ev: unknown) {
      for (const fn of this.listeners.get(type) ?? []) {
        fn(ev);
      }
    }
  }
  vi.stubGlobal("WebSocket", FakeWebSocket);

  const onEvent = vi.fn();
  const onStatus = vi.fn();
  const sub = subscribeRunEvents("http://127.0.0.1:8420", "run-9", {
    onEvent,
    onStatus,
    last: 20,
  });
  sockets[0]?.emit("open", {});
  sockets[0]?.emit("message", {
    data: JSON.stringify({
      id: "evt-2",
      run_id: "run-9",
      type: "log",
      created_at: "2026-08-21T00:00:01Z",
    }),
  });
  sockets[0]?.emit("close", {});
  expect(onStatus).toHaveBeenCalledWith("reconnecting");

  await vi.advanceTimersByTimeAsync(500);
  expect(sockets.length).toBeGreaterThan(1);
  sockets[1]?.emit("open", {});
  sockets[1]?.emit("message", {
    data: JSON.stringify({
      id: "evt-2",
      run_id: "run-9",
      type: "log",
      created_at: "2026-08-21T00:00:01Z",
    }),
  });
  sockets[1]?.emit("message", {
    data: JSON.stringify({
      id: "evt-3",
      run_id: "run-9",
      type: "log",
      created_at: "2026-08-21T00:00:02Z",
    }),
  });
  expect(onEvent).toHaveBeenCalledTimes(2);
  sub.close();
  vi.useRealTimers();
});


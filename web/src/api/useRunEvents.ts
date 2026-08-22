import { useEffect, useRef, useState } from "react";
import type { RunEvent } from "@zeroth/api";
import { httpOrigin } from "./client";
import { subscribeRunEvents, type StreamStatus } from "./runEvents";

export function useRunEvents(runId: string | undefined): {
  events: RunEvent[];
  status: StreamStatus;
  text: string;
} {
  const [events, setEvents] = useState<RunEvent[]>([]);
  const [status, setStatus] = useState<StreamStatus>("closed");
  const eventsRef = useRef<RunEvent[]>([]);

  useEffect(() => {
    if (!runId) {
      return;
    }
    eventsRef.current = [];
    setEvents([]);
    const sub = subscribeRunEvents(httpOrigin(), runId, {
      onEvent: (event) => {
        eventsRef.current = [...eventsRef.current, event];
        setEvents(eventsRef.current);
      },
      onStatus: setStatus,
    });
    return () => sub.close();
  }, [runId]);

  const text = events
    .filter((e) => e.type === "log" && e.message)
    .map((e) => e.message)
    .join("");

  return { events, status, text };
}

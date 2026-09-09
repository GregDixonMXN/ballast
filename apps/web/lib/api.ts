export const BALLAST_SERVER =
  process.env.NEXT_PUBLIC_BALLAST_SERVER ?? "http://localhost:8080";
export const token = () =>
  typeof window === "undefined" ? "" : localStorage.getItem("ballast_token") ?? "";

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BALLAST_SERVER}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${token()}`,
      ...(init?.headers ?? {}),
    },
  });
  if (!res.ok) throw new Error(`${res.status} ${await res.text()}`);
  return res.json() as Promise<T>;
}

// Live events over SSE. Reconnects with backoff; history via REST.
import { useEffect } from "react";
export function useEvents(project: string, onEvent: (e: unknown) => void) {
  useEffect(() => {
    let es: EventSource | null = null;
    let alive = true;
    const connect = () => {
      es = new EventSource(
        `${BALLAST_SERVER}/events?project=${encodeURIComponent(project)}`
      );
      es.onmessage = (m) => {
        try {
          onEvent(JSON.parse(m.data));
        } catch {
          /* keep stream alive on bad frames */
        }
      };
      es.onerror = () => {
        es?.close();
        if (alive) setTimeout(connect, 2000);
      };
    };
    connect();
    return () => {
      alive = false;
      es?.close();
    };
  }, [project]);
}

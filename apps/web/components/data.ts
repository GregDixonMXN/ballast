"use client";
import { useCallback, useEffect, useState } from "react";
import { api } from "../lib/api";
import { useSession } from "./session";

export function useData<T>(paths: string[], initial: T) {
  const { token } = useSession(); const key = JSON.stringify(paths);
  const [data, setData] = useState<T>(initial); const [error, setError] = useState("");
  const [loading, setLoading] = useState(true); const [updated, setUpdated] = useState<Date | null>(null); const [revision, setRevision] = useState(0);
  const refresh = useCallback(() => setRevision(v => v + 1), []);
  useEffect(() => {
    if (!token) return; let alive = true; let running = false;
    const controller = new AbortController();
    const load = async () => {
      if (running || document.visibilityState === "hidden") return; running = true;
      try {
        const values = await Promise.all((JSON.parse(key) as string[]).map(path => api<unknown>(token, path, { signal: controller.signal })));
        if (alive) { setData(values as T); setError(""); setUpdated(new Date()); }
      } catch (e) { if (alive && !(e instanceof DOMException && e.name === "AbortError")) setError(e instanceof Error ? e.message : "Refresh failed."); }
      finally { running = false; if (alive) setLoading(false); }
    };
    void load(); const timer = setInterval(load, 5000); document.addEventListener("visibilitychange", load);
    return () => { alive = false; controller.abort(); clearInterval(timer); document.removeEventListener("visibilitychange", load); };
  }, [token, key, revision]);
  return { data, error, loading, updated, refresh };
}

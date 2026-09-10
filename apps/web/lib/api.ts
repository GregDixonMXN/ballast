export class APIError extends Error {
  constructor(message: string, readonly status: number) { super(message); }
}
export async function api<T>(token: string, path: string, init: RequestInit = {}): Promise<T> {
  let response: Response;
  try {
    response = await fetch(`/api${path}`, {
      ...init, cache: "no-store", credentials: "omit", signal: init.signal ?? AbortSignal.timeout(125000),
      headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}`, ...init.headers },
    });
  } catch (error) {
    if (error instanceof DOMException && error.name === "AbortError") throw error;
    throw new APIError("Connection interrupted. Check the local server and try again.", 0);
  }
  const payload = await response.json().catch(() => null);
  if (!response.ok) throw new APIError(payload?.error ?? `Request failed (${response.status}).`, response.status);
  return payload as T;
}
export type Project = { id: string; name: string; repo_path: string; branch: string; canonical_sha: string; created_at: string };
export type Task = { id: string; project_id: string; title: string; description: string; status: string; scopes?: string[]; created_at: string; updated_at: string };
export type Workspace = { id: string; task_id: string; status: string; base_commit: string; path: string; runner_id?: string; agent_id?: string; created_at: string; updated_at: string };
export type Changeset = { id: string; project_id: string; task_id: string; status: string; files?: string[]; diff: string; base_commit: string; test_ref?: string; created_at: string };
export type Runner = { id: string; hostname: string; online: boolean; last_seen: string; os: string; arch: string; capabilities?: string[]; adapters?: string[] };
export const short = (value?: string) => value ? value.slice(0, 8) : "—";
export function when(value: string) { const d = new Date(value); return Number.isNaN(d.getTime()) ? "Unknown time" : d.toLocaleString([], { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }); }

export type ActivityEvent = { id: string; type: string; entity_id?: string; actor_id?: string; at: string; metadata?: Record<string, unknown> };

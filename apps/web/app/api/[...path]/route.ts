import { NextRequest } from "next/server";

export const dynamic = "force-dynamic";
const loopback = new Set(["localhost", "127.0.0.1", "[::1]"]);
const routes: Record<string, RegExp[]> = {
  GET: [/^projects$/, /^runners$/, /^activity$/, /^projects\/[^/]+$/, /^projects\/[^/]+\/(tasks|workspaces|changesets)$/, /^workspaces\/[^/]+\/(files|diff)$/, /^changesets\/[^/]+$/],
  POST: [/^projects$/, /^projects\/[^/]+\/(tasks|workspaces)$/, /^tasks\/[^/]+\/(assign|transition)$/, /^workspaces\/[^/]+\/changesets$/, /^changesets\/[^/]+\/(decision|integrate)$/],
};
const fail = (error: string, status: number) => Response.json({ error }, { status, headers: { "Cache-Control": "no-store" } });

async function proxy(request: NextRequest, context: { params: Promise<{ path: string[] }> }) {
  let authority: URL;
  try { authority = new URL(`http://${request.headers.get("host")}`); } catch { return fail("Invalid authority.", 400); }
  if (!loopback.has(authority.hostname)) return fail("The dashboard is available on loopback only.", 403);
  const origin = request.headers.get("origin");
  if (origin && origin !== `${request.nextUrl.protocol}//${authority.host}`) return fail("Cross-origin requests are not allowed.", 403);
  if (["cross-site", "same-site"].includes(request.headers.get("sec-fetch-site") ?? "")) return fail("Cross-origin requests are not allowed.", 403);
  const { path } = await context.params;
  if (!path.every(segment => /^[a-zA-Z0-9_-]+$/.test(segment))) return fail("Invalid API path.", 400);
  const resource = path.join("/");
  if (!routes[request.method]?.some(pattern => pattern.test(resource))) return fail("API route not found.", 404);
  const authorization = request.headers.get("authorization") ?? "";
  if (!/^Bearer [a-zA-Z0-9_-]{16,512}$/.test(authorization)) return fail("Enter a valid operator token to connect.", 401);
  let upstream: URL;
  try { upstream = new URL(process.env.BALLAST_API_URL ?? "http://127.0.0.1:8080"); } catch { return fail("The local API address is misconfigured.", 503); }
  if (!loopback.has(upstream.hostname) || !["http:", "https:"].includes(upstream.protocol) || upstream.username || upstream.password || upstream.pathname !== "/" || upstream.search || upstream.hash) return fail("BALLAST_API_URL must be a loopback HTTP origin without credentials or a path.", 503);
  let body: string | undefined;
  if (request.method === "POST") {
    if (!(request.headers.get("content-type") ?? "").startsWith("application/json")) return fail("JSON is required.", 415);
    const reader = request.body?.getReader();
    const chunks: Uint8Array[] = []; let size = 0;
    if (reader) {
      while (true) {
        const { done, value } = await reader.read(); if (done) break;
        size += value.length;
        if (size > 4 * 1024 * 1024) { await reader.cancel(); return fail("Request is too large.", 413); }
        chunks.push(value);
      }
    }
    body = Buffer.concat(chunks).toString("utf8");
  }
  try {
    const response = await fetch(new URL(`/${resource}${resource === "activity" && request.nextUrl.searchParams.has("project") ? "?project=" + encodeURIComponent(request.nextUrl.searchParams.get("project")!) : ""}`, upstream), {
      method: request.method, headers: { Authorization: authorization, "Content-Type": "application/json", Accept: "application/json" },
      body, cache: "no-store", redirect: "error", signal: AbortSignal.timeout(resource.endsWith("/integrate") ? 120000 : 30000),
    });
    return new Response(response.body, { status: response.status, headers: { "Content-Type": "application/json; charset=utf-8", "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff" } });
  } catch { return fail("Cannot reach the local Ballast API. Check that the server is running, then refresh.", 502); }
}
export const GET = proxy;
export const POST = proxy;

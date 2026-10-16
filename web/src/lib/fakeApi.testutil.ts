import { vi } from "vitest";

/**
 * A stand-in for the Go server at the HTTP boundary: it answers Connect
 * protocol JSON requests that the real transport sends through
 * globalThis.fetch. Only tests import it.
 */
export type FakeCall = { procedure: string; authorization: string | null; credentials: RequestCredentials | undefined };

export type FakeReply = { status?: number; body: unknown };

export type FakeHandler = (call: FakeCall) => FakeReply | Promise<FakeReply>;

export function connectError(code: string, message = code): FakeReply {
  const status = { unauthenticated: 401, permission_denied: 403, not_found: 404, unavailable: 503 }[code] ?? 400;
  return { status, body: { code, message } };
}

export function sessionReply(opts: { token: string; username?: string; role?: string; expiresInSec?: number }): FakeReply {
  const expires = new Date(Date.now() + (opts.expiresInSec ?? 600) * 1000).toISOString();
  return {
    body: {
      accessToken: opts.token,
      accessTokenExpiresAt: expires,
      user: {
        userId: "0b9e0a52-5d0f-4a8e-9a43-1f0f7f3c2a10",
        username: opts.username ?? "teller",
        role: opts.role ?? "ROLE_USER",
        emailVerified: true,
      },
    },
  };
}

export function installFakeApi(routes: Record<string, FakeHandler>) {
  const calls: FakeCall[] = [];
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.href : input.url;
    const prefix = "/api/perspectiv.v1.";
    const at = url.indexOf(prefix);
    if (at < 0) throw new Error(`unexpected fetch ${url}`);
    const procedure = url.slice(at + prefix.length);
    const headers = new Headers(init?.headers);
    const call = { procedure, authorization: headers.get("Authorization"), credentials: init?.credentials };
    calls.push(call);
    const handler = routes[procedure];
    const reply = handler ? await handler(call) : connectError("unimplemented");
    return new Response(JSON.stringify(reply.body), {
      status: reply.status ?? 200,
      headers: { "Content-Type": "application/json" },
    });
  });
  vi.stubGlobal("fetch", fetchMock);
  return { calls, fetchMock };
}

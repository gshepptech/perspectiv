import { Code, ConnectError } from "@connectrpc/connect";
import { useSyncExternalStore } from "react";
import { redirect } from "react-router";

import type { LoginResponse, RefreshResponse, SessionUser, SignUpResponse } from "@/gen/perspectiv/v1/auth_pb";
import type { Role } from "@/gen/perspectiv/v1/enums_pb";
import { clients } from "@/lib/connect";

/** The signed-in caller as the web app sees it. */
export type Session = {
  userId: string;
  username: string;
  role: Role;
  emailVerified: boolean;
};

// The access token lives only in this module's memory. It is never written to
// Web Storage, a URL or a log; a reload loses it and the HttpOnly refresh cookie
// buys a new one through AuthService/Refresh.
let accessToken: string | null = null;
let expiresAtMs = 0;
let current: Session | null = null;
let loaded = false;
let inflight: Promise<Session | null> | null = null;
const listeners = new Set<() => void>();

// Refresh this long before the access token's stated expiry so a request does
// not race the server's clock.
const EXPIRY_SKEW_MS = 30_000;

function publish(next: Session | null): void {
  current = next;
  loaded = true;
  for (const listener of listeners) listener();
}

function toSession(user: SessionUser | undefined): Session | null {
  if (!user || !user.userId) return null;
  return { userId: user.userId, username: user.username, role: user.role, emailVerified: user.emailVerified };
}

/** The current access token, or null when signed out. */
export function getAccessToken(): string | null {
  return accessToken;
}

/** The session held in memory right now, without asking the server. */
export function currentSession(): Session | null {
  return current;
}

/**
 * Takes the tokens from a Login, Refresh or SignUp response. A response
 * without an access token or a user is treated as signed out rather than
 * half signed in.
 */
export function signIn(res: LoginResponse | RefreshResponse | SignUpResponse): Session | null {
  const session = toSession(res.user);
  if (!res.accessToken || !session) {
    clearSession();
    return null;
  }
  accessToken = res.accessToken;
  expiresAtMs = res.accessTokenExpiresAt ? Number(res.accessTokenExpiresAt.seconds) * 1000 : 0;
  publish(session);
  return session;
}

/** Forgets the in-memory token and session. The server-side Logout is the caller's to make. */
export function clearSession(): void {
  accessToken = null;
  expiresAtMs = 0;
  publish(null);
}

/** Updates fields of the current session after the server confirmed a change (say, a new username). */
export function updateSession(patch: Partial<Session>): void {
  if (current) publish({ ...current, ...patch });
}

/**
 * Asks the server for a fresh access token with the refresh cookie. Concurrent
 * callers share one request: refresh tokens rotate on every use, so two
 * parallel refreshes would present the same token twice and the server would
 * treat the second as reuse.
 *
 * An Unauthenticated, PermissionDenied or NotFound answer means "no valid
 * cookie" and resolves to null (signed out). Any other failure (the server is
 * down, a network error) rejects, so the caller can show an error instead of
 * silently presenting a signed-in reader as signed out.
 */
export function refreshSession(): Promise<Session | null> {
  if (inflight) return inflight;
  inflight = (async () => {
    try {
      return signIn(await clients.auth.refresh({}));
    } catch (err) {
      const code = ConnectError.from(err).code;
      if (code === Code.Unauthenticated || code === Code.PermissionDenied || code === Code.NotFound) {
        clearSession();
        return null;
      }
      throw err;
    } finally {
      inflight = null;
    }
  })();
  return inflight;
}

/** True when the held access token is missing or about to expire. */
export function tokenIsStale(now = Date.now()): boolean {
  if (!accessToken) return true;
  return expiresAtMs > 0 && now >= expiresAtMs - EXPIRY_SKEW_MS;
}

/**
 * The session for a loader: the held one while its token is fresh, otherwise
 * one refresh (shared with any other caller in flight).
 */
export async function loadSession(): Promise<Session | null> {
  if (inflight) return inflight;
  if (loaded && !tokenIsStale()) return current;
  if (loaded && !current) return null;
  return refreshSession();
}

/**
 * The signed-in session, or a thrown redirect to /login carrying the current
 * path and query in `next`. It reads the session held in memory, so a loader
 * calls it after `await loadSession()`; the router does that for every route
 * that needs a signed-in user before the route's own loader runs.
 */
export function requireUser(request: Request): Session {
  if (current) return current;
  const url = new URL(request.url);
  throw redirect(`/login?next=${encodeURIComponent(url.pathname + url.search)}`);
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** The current session, re-rendering on sign-in, sign-out and refresh. */
export function useSession(): Session | null {
  return useSyncExternalStore(subscribe, currentSession, currentSession);
}

/** Test seam: forgets everything, including whether a session was ever loaded. */
export function resetSessionForTests(): void {
  accessToken = null;
  expiresAtMs = 0;
  current = null;
  loaded = false;
  inflight = null;
  for (const listener of listeners) listener();
}

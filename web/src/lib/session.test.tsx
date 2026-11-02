import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { Role } from "@/gen/perspectiv/v1/enums_pb";
import { connectError, installFakeApi, sessionReply } from "@/lib/fakeApi.testutil";
import {
  clearSession,
  currentSession,
  getAccessToken,
  loadSession,
  refreshSession,
  requireUser,
  resetSessionForTests,
  useSession,
} from "@/lib/session";

beforeEach(() => {
  resetSessionForTests();
  localStorage.clear();
  sessionStorage.clear();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("refreshSession", () => {
  it("shares one Refresh request among concurrent callers", async () => {
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    const api = installFakeApi({
      "AuthService/Refresh": async () => {
        await gate;
        return sessionReply({ token: "tok-1", username: "ada", role: "ROLE_ADMIN" });
      },
    });
    const all = Promise.all([loadSession(), refreshSession(), loadSession()]);
    release();
    const [a, b, c] = await all;
    expect(api.calls.filter((c) => c.procedure === "AuthService/Refresh")).toHaveLength(1);
    expect(a).toEqual({ userId: expect.any(String), username: "ada", role: Role.ADMIN, emailVerified: true, twoFactorEnabled: false });
    expect(b).toBe(a);
    expect(c).toBe(a);
    expect(getAccessToken()).toBe("tok-1");
  });

  it("sends the refresh request with same-origin credentials so the HttpOnly cookie rides along", async () => {
    const api = installFakeApi({ "AuthService/Refresh": () => sessionReply({ token: "tok" }) });
    await refreshSession();
    expect(api.calls[0]?.credentials).toBe("same-origin");
  });

  it("treats an Unauthenticated answer as signed out and does not ask again on the next load", async () => {
    const api = installFakeApi({ "AuthService/Refresh": () => connectError("unauthenticated") });
    expect(await loadSession()).toBeNull();
    expect(getAccessToken()).toBeNull();
    expect(await loadSession()).toBeNull();
    expect(api.calls).toHaveLength(1);
  });

  it("rejects on an unreachable server and retries on the next load", async () => {
    let down = true;
    const api = installFakeApi({
      "AuthService/Refresh": () => (down ? connectError("unavailable") : sessionReply({ token: "tok-2" })),
    });
    await expect(loadSession()).rejects.toThrow();
    expect(currentSession()).toBeNull();
    down = false;
    expect(await loadSession()).toMatchObject({ username: "teller" });
    expect(api.calls).toHaveLength(2);
  });

  it("reuses a fresh token without a request and refreshes one about to expire", async () => {
    const api = installFakeApi({ "AuthService/Refresh": () => sessionReply({ token: "short", expiresInSec: 10 }) });
    await loadSession();
    expect(api.calls).toHaveLength(1);
    await loadSession();
    expect(api.calls).toHaveLength(2);
  });

  it("refuses a reply without a user rather than half signing in", async () => {
    installFakeApi({ "AuthService/Refresh": () => ({ body: { accessToken: "orphan" } }) });
    expect(await refreshSession()).toBeNull();
    expect(getAccessToken()).toBeNull();
  });

  it("never writes the token to Web Storage", async () => {
    installFakeApi({ "AuthService/Refresh": () => sessionReply({ token: "secret-token" }) });
    await refreshSession();
    expect(localStorage.length).toBe(0);
    expect(sessionStorage.length).toBe(0);
    expect(document.cookie).not.toContain("secret-token");
  });
});

describe("requireUser", () => {
  it("throws a redirect to /login carrying the path and query in next when signed out", () => {
    let thrown: unknown;
    try {
      requireUser(new Request("http://localhost/compose?placeId=p1&date=2026-10-01"));
    } catch (err) {
      thrown = err;
    }
    expect(thrown).toBeInstanceOf(Response);
    const res = thrown as Response;
    expect(res.status).toBe(302);
    expect(res.headers.get("Location")).toBe("/login?next=%2Fcompose%3FplaceId%3Dp1%26date%3D2026-10-01");
  });

  it("returns the session when signed in", async () => {
    installFakeApi({ "AuthService/Refresh": () => sessionReply({ token: "tok", username: "bo" }) });
    await loadSession();
    expect(requireUser(new Request("http://localhost/inbox")).username).toBe("bo");
  });
});

function Who() {
  const session = useSession();
  return <p>{session ? `in:${session.username}` : "out"}</p>;
}

describe("useSession", () => {
  it("re-renders on sign-in and sign-out", async () => {
    installFakeApi({ "AuthService/Refresh": () => sessionReply({ token: "tok", username: "cy" }) });
    render(<Who />);
    expect(screen.getByText("out")).toBeInTheDocument();
    await act(async () => {
      await refreshSession();
    });
    expect(screen.getByText("in:cy")).toBeInTheDocument();
    act(() => clearSession());
    expect(screen.getByText("out")).toBeInTheDocument();
  });
});

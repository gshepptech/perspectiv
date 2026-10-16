import { Code, ConnectError } from "@connectrpc/connect";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { clients } from "@/lib/connect";
import { connectError, installFakeApi, sessionReply } from "@/lib/fakeApi.testutil";
import { currentSession, getAccessToken, refreshSession, resetSessionForTests } from "@/lib/session";

beforeEach(() => resetSessionForTests());
afterEach(() => vi.unstubAllGlobals());

describe("transport", () => {
  it("posts to the /api mount with no Authorization header when signed out", async () => {
    const api = installFakeApi({ "PlaceService/SearchPlaces": () => ({ body: { places: [] } }) });
    await clients.place.searchPlaces({ query: "Austin" });
    expect(api.calls).toEqual([{ procedure: "PlaceService/SearchPlaces", authorization: null, credentials: "same-origin" }]);
  });

  it("sends the in-memory access token as a bearer header when signed in", async () => {
    const api = installFakeApi({
      "AuthService/Refresh": () => sessionReply({ token: "tok-a" }),
      "PlaceService/SearchPlaces": () => ({ body: { places: [] } }),
    });
    await refreshSession();
    await clients.place.searchPlaces({ query: "Austin" });
    expect(api.calls.at(-1)?.authorization).toBe("Bearer tok-a");
  });

  it("refreshes once and retries once when a signed-in call is answered Unauthenticated", async () => {
    let token = "old";
    const api = installFakeApi({
      "AuthService/Refresh": () => sessionReply({ token }),
      "NotifyService/ListInbox": (call) =>
        call.authorization === "Bearer new" ? { body: { notifications: [] } } : connectError("unauthenticated"),
    });
    await refreshSession();
    token = "new";
    await clients.notify.listInbox({});
    expect(api.calls.map((c) => [c.procedure, c.authorization])).toEqual([
      ["AuthService/Refresh", null],
      ["NotifyService/ListInbox", "Bearer old"],
      ["AuthService/Refresh", "Bearer old"],
      ["NotifyService/ListInbox", "Bearer new"],
    ]);
    expect(getAccessToken()).toBe("new");
  });

  it("surfaces the original error and signs out when the retry's refresh is refused", async () => {
    let refreshOK = true;
    installFakeApi({
      "AuthService/Refresh": () => (refreshOK ? sessionReply({ token: "t" }) : connectError("unauthenticated")),
      "NotifyService/ListInbox": () => connectError("unauthenticated", "session revoked"),
    });
    await refreshSession();
    refreshOK = false;
    const err = await clients.notify.listInbox({}).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ConnectError);
    expect((err as ConnectError).code).toBe(Code.Unauthenticated);
    expect(currentSession()).toBeNull();
    expect(getAccessToken()).toBeNull();
  });

  it("refreshes a token about to expire before the call instead of sending it", async () => {
    let n = 0;
    const api = installFakeApi({
      "AuthService/Refresh": () => sessionReply({ token: `t${++n}`, expiresInSec: n === 1 ? 5 : 600 }),
      "PlaceService/SearchPlaces": () => ({ body: { places: [] } }),
    });
    await refreshSession();
    await clients.place.searchPlaces({ query: "x" });
    expect(api.calls.map((c) => c.procedure)).toEqual([
      "AuthService/Refresh",
      "AuthService/Refresh",
      "PlaceService/SearchPlaces",
    ]);
    expect(api.calls.at(-1)?.authorization).toBe("Bearer t2");
  });

  it("does not retry a refused call that was sent without a token", async () => {
    const api = installFakeApi({ "VoteService/Vote": () => connectError("unauthenticated") });
    await expect(clients.vote.vote({})).rejects.toMatchObject({ code: Code.Unauthenticated });
    expect(api.calls).toHaveLength(1);
  });

  it("exposes a client for every service", () => {
    expect(Object.keys(clients).sort()).toEqual(
      ["arc", "auth", "lore", "moderation", "notify", "perspective", "place", "react", "user", "vote"].sort(),
    );
  });
});

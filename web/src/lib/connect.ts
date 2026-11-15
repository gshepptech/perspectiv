import { Code, ConnectError, createClient, type Interceptor } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";

import { ArcService } from "@/gen/perspectiv/v1/arc_pb";
import { AuthService } from "@/gen/perspectiv/v1/auth_pb";
import { LoreService } from "@/gen/perspectiv/v1/lore_pb";
import { ModerationService } from "@/gen/perspectiv/v1/moderation_pb";
import { NotifyService } from "@/gen/perspectiv/v1/notify_pb";
import { PerspectiveService } from "@/gen/perspectiv/v1/perspective_pb";
import { PlaceService } from "@/gen/perspectiv/v1/place_pb";
import { ReactService } from "@/gen/perspectiv/v1/react_pb";
import { UserService } from "@/gen/perspectiv/v1/user_pb";
import { VoteService } from "@/gen/perspectiv/v1/vote_pb";
import { getAccessToken, refreshSession, tokenIsStale } from "@/lib/session";

const authServiceName = AuthService.typeName;

/**
 * Sends the in-memory access token as a bearer header. Before a call it
 * refreshes a token that is about to expire; when a call is still answered
 * Unauthenticated while a token was sent, it refreshes once and retries once.
 * AuthService calls are passed through untouched so a failing refresh can
 * never recurse into another refresh.
 */
const authInterceptor: Interceptor = (next) => async (req) => {
  if (req.service.typeName === authServiceName) {
    const token = getAccessToken();
    if (token) req.header.set("Authorization", `Bearer ${token}`);
    return next(req);
  }
  if (getAccessToken() && tokenIsStale()) await refreshSession();
  const sent = getAccessToken();
  if (sent) req.header.set("Authorization", `Bearer ${sent}`);
  try {
    return await next(req);
  } catch (err) {
    if (!sent || req.stream || ConnectError.from(err).code !== Code.Unauthenticated) throw err;
    const session = await refreshSession();
    const token = getAccessToken();
    if (!session || !token) throw err;
    req.header.set("Authorization", `Bearer ${token}`);
    return next(req);
  }
};

/** The one transport: Connect protocol to the server's /api mount, same-origin cookies. */
export const transport = createConnectTransport({
  baseUrl: "/api",
  interceptors: [authInterceptor],
  fetch: (input, init) => globalThis.fetch(input, { ...init, credentials: "same-origin" }),
});

export const clients = {
  auth: createClient(AuthService, transport),
  user: createClient(UserService, transport),
  place: createClient(PlaceService, transport),
  arc: createClient(ArcService, transport),
  perspective: createClient(PerspectiveService, transport),
  lore: createClient(LoreService, transport),
  vote: createClient(VoteService, transport),
  react: createClient(ReactService, transport),
  notify: createClient(NotifyService, transport),
  moderation: createClient(ModerationService, transport),
};

import { QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { vi } from "vitest";

import { createQueryClient } from "@/app/queryClient";
import { buildRoutes } from "@/app/router";
import { preloadedPageLoader, preloadPages } from "@/app/routes.testutil";
import { connectError, installFakeApi, type FakeReply } from "@/lib/fakeApi.testutil";
import { resetSessionForTests } from "@/lib/session";

// Every test file that renders routes through this harness loads the pages
// first, so no test races a cold module transform.
preloadPages();

/** One request the app sent: the procedure ("PlaceService/GetEpic") and its Connect JSON body. */
export type SentRequest = { procedure: string; body: Record<string, unknown> };

/** Answers a procedure from the request body the app sent. */
export type BodyHandler = (body: Record<string, unknown>) => FakeReply | Promise<FakeReply>;

function decode(body: BodyInit | null | undefined): Record<string, unknown> {
  if (body == null) return {};
  const text = typeof body === "string" ? body : new TextDecoder().decode(body as Uint8Array);
  return text ? (JSON.parse(text) as Record<string, unknown>) : {};
}

/**
 * The real app (route table, shell, lazy route modules, Connect transport) in
 * a memory router, with the Go server replaced at the HTTP boundary by
 * handlers that see each request's body. Signed out unless `refresh` says
 * otherwise.
 */
export function renderApp(path: string, handlers: Record<string, BodyHandler>, opts: { refresh?: () => FakeReply } = {}) {
  resetSessionForTests();
  const sent: SentRequest[] = [];
  let body: Record<string, unknown> = {};
  const routes = Object.fromEntries(Object.entries(handlers).map(([procedure, handle]) => [procedure, () => handle(body)]));
  installFakeApi({ "AuthService/Refresh": opts.refresh ?? (() => connectError("unauthenticated")), ...routes });
  const fake = globalThis.fetch;
  vi.stubGlobal("fetch", (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.href : input.url;
    body = decode(init?.body);
    sent.push({ procedure: url.slice(url.indexOf("perspectiv.v1.") + "perspectiv.v1.".length), body });
    return fake(input, init);
  });
  const router = createMemoryRouter(buildRoutes(preloadedPageLoader), { initialEntries: [path] });
  // The app's own client, so caching matches production; retries are off so
  // an error reply shows at once.
  const queryClient = createQueryClient();
  queryClient.setDefaultOptions({ ...queryClient.getDefaultOptions(), queries: { ...queryClient.getDefaultOptions().queries, retry: false } });
  const view = render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  const calls = (procedure: string) => sent.filter((r) => r.procedure === procedure).map((r) => r.body);
  return { router, sent, calls, ...view };
}

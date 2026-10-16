import { QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import type { ComponentType } from "react";
import { createMemoryRouter, RouterProvider, useLocation } from "react-router";

import { createQueryClient } from "@/app/queryClient";
import { installFakeApi, sessionReply, connectError, type FakeHandler } from "@/lib/fakeApi.testutil";
import { loadSession } from "@/lib/session";

/** The user id sessionReply signs in as. */
export const ME = "0b9e0a52-5d0f-4a8e-9a43-1f0f7f3c2a10";

export type Route = { path: string; Component: ComponentType };

function LoginStub() {
  const location = useLocation();
  return (
    <p data-testid="login">
      login at {location.pathname}
      {location.search}
    </p>
  );
}

function ReaderStub() {
  const location = useLocation();
  return <p data-testid="reader">reader at {location.pathname}</p>;
}

/**
 * Installs the fake API (with a request-body reader the stateful handlers use),
 * optionally signs in, and renders `route` at `path` inside a memory router and
 * a fresh query client.
 */
export async function renderApp(
  route: Route,
  path: string,
  opts: { signedIn?: boolean; routes?: Record<string, FakeHandler> } = {},
) {
  const api = installFakeApi({
    "AuthService/Refresh": opts.signedIn ? () => sessionReply({ token: "tok", username: "wren" }) : () => connectError("unauthenticated"),
    ...opts.routes,
  });
  await loadSession();
  const router = createMemoryRouter(
    [
      route,
      { path: "/login", Component: LoginStub },
      { path: "/perspectives/:perspectiveId", Component: ReaderStub },
    ],
    { initialEntries: [path] },
  );
  const client = createQueryClient();
  client.setDefaultOptions({ queries: { retry: false, staleTime: 30_000 }, mutations: { retry: false } });
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return { api, router, client };
}

function decode(raw: unknown): Record<string, unknown> {
  // The transport may hand fetch a typed array from another realm (jsdom's),
  // so instanceof is not reliable; ArrayBuffer.isView is.
  const text =
    typeof raw === "string" ? raw : ArrayBuffer.isView(raw) ? new TextDecoder().decode(raw as Uint8Array) : "{}";
  return JSON.parse(text) as Record<string, unknown>;
}

type FetchMock = { mock: { calls: unknown[][] } };

/**
 * The JSON body of the request being answered. The fake API invokes a handler
 * synchronously right after recording the fetch, so the last recorded call is
 * the one in hand.
 */
export function currentBody(): Record<string, unknown> {
  const init = (globalThis.fetch as unknown as FetchMock).mock.calls.at(-1)?.[1] as RequestInit | undefined;
  return decode(init?.body);
}

/** The JSON bodies of every request sent to `procedure`, in order. */
export function bodiesFor(api: { calls: { procedure: string }[]; fetchMock: FetchMock }, procedure: string) {
  const out: Record<string, unknown>[] = [];
  api.calls.forEach((call, i) => {
    if (call.procedure !== procedure) return;
    out.push(decode((api.fetchMock.mock.calls[i]?.[1] as RequestInit | undefined)?.body));
  });
  return out;
}

/** Radix Select measures and captures pointers; jsdom has neither. */
export function polyfillRadix(): void {
  Element.prototype.scrollIntoView ??= function scrollIntoView() {};
  Element.prototype.hasPointerCapture ??= () => false;
  Element.prototype.releasePointerCapture ??= () => {};
}

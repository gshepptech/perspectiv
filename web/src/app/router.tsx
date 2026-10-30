import type { ComponentType } from "react";
import { createBrowserRouter, type LoaderFunction, type LoaderFunctionArgs, type RouteObject } from "react-router";

import { loadSession, requireUser, type Session } from "@/lib/session";

import { NotFound, RouteError } from "./RouteError";
import { RootLayout, ShellFallback } from "./RootLayout";

/** What a route module under src/routes exports. */
export type RouteModule = { Component: ComponentType; loader?: LoaderFunction };

/** Loads a route module by its path under src/routes, e.g. "landing/LandingPage". */
export type PageLoader = (module: string) => Promise<RouteModule>;

export type RootLoaderData = { session: Session | null };

type Page = {
  path: string;
  module: string;
  /** Signed-out visitors are redirected to /login?next=<this path and query>. */
  signedIn?: boolean;
};

/**
 * The route table. Reading (landing, place, arc, a perspective) is public;
 * every writing or account surface needs a signed-in user. The server enforces
 * the same split on every RPC: this guard is usability, not authorization.
 */
export const pages: readonly Page[] = [
  { path: "/", module: "landing/LandingPage" },
  { path: "/places/:placeId", module: "place/PlacePage" },
  { path: "/perspectives/:perspectiveId", module: "read/ReadPage" },
  { path: "/arcs/:arcId", module: "arc/ArcPage" },
  { path: "/lores/:loreId", module: "lore/LorePage" },
  { path: "/users/:username", module: "profile/ProfilePage" },
  { path: "/i/:code", module: "invite/InvitePage" },
  { path: "/compose", module: "compose/ComposePage", signedIn: true },
  { path: "/claims", module: "claims/ClaimsPage", signedIn: true },
  { path: "/admin/reports", module: "admin/ReportsPage", signedIn: true },
  { path: "/admin/access", module: "admin/AccessPage", signedIn: true },
  { path: "/login", module: "auth/LoginPage" },
  { path: "/signup", module: "auth/SignUpPage" },
  { path: "/verify-email", module: "auth/VerifyEmailPage" },
  { path: "/forgot-password", module: "auth/ForgotPasswordPage" },
  { path: "/reset-password", module: "auth/ResetPasswordPage" },
  { path: "/confirm-email", module: "auth/ConfirmEmailPage" },
  { path: "/settings", module: "settings/SettingsPage", signedIn: true },
  { path: "/terms", module: "legal/TermsPage" },
  { path: "/privacy", module: "legal/PrivacyPage" },
  { path: "/guidelines", module: "legal/GuidelinesPage" },
  { path: "/inbox", module: "inbox/InboxPage", signedIn: true },
];

// Route modules are found with import.meta.glob rather than one literal
// import() per route. Each module is still its own lazily loaded chunk, but a
// route whose module has not been written yet fails when it is visited (the
// route's error element names the missing module) instead of failing the
// whole build: the route pages are built by separate work streams.
const routeModules = import.meta.glob<RouteModule>("../routes/*/*Page.tsx");

export const globPageLoader: PageLoader = async (module) => {
  const load = routeModules[`../routes/${module}.tsx`];
  if (!load) throw new Error(`route module @/routes/${module} does not exist`);
  const mod = await load();
  if (typeof mod.Component !== "function") throw new Error(`route module @/routes/${module} exports no Component`);
  return mod;
};

/**
 * The root loader resolves the session from memory or with one cookie refresh.
 * A refresh that fails for any reason other than "no valid cookie" (the
 * server is unreachable, say) leaves the reader signed out instead of failing
 * every page: public reading keeps working, and signed-in surfaces still ask
 * again through their own guard.
 */
export async function rootLoader(): Promise<RootLoaderData> {
  try {
    return { session: await loadSession() };
  } catch {
    return { session: null };
  }
}

function Unrendered() {
  return null;
}

function pageRoute(page: Page, load: PageLoader): RouteObject {
  return {
    path: page.path === "/" ? undefined : page.path.slice(1),
    index: page.path === "/" ? true : undefined,
    errorElement: <RouteError />,
    lazy: async () => {
      let mod: RouteModule;
      try {
        mod = await load(page.module);
      } catch (err) {
        // A route whose lazy() rejects is left unresolved and renders nothing;
        // resolving to a loader that rethrows shows the route's error element.
        return {
          Component: Unrendered,
          loader: () => {
            throw err;
          },
        };
      }
      if (!page.signedIn && !mod.loader) return { Component: mod.Component };
      const moduleLoader = mod.loader;
      const loader = async (args: LoaderFunctionArgs) => {
        // Child loaders run beside the root loader, so each waits for the same
        // in-flight session before it reads it.
        await loadSession();
        if (page.signedIn) requireUser(args.request);
        return moduleLoader ? moduleLoader(args) : null;
      };
      return { Component: mod.Component, loader };
    },
  };
}

/** The route tree; tests pass their own PageLoader, the app uses the glob. */
export function buildRoutes(load: PageLoader = globPageLoader): RouteObject[] {
  return [
    {
      id: "root",
      path: "/",
      Component: RootLayout,
      HydrateFallback: ShellFallback,
      errorElement: <RouteError />,
      loader: rootLoader,
      children: [...pages.map((page) => pageRoute(page, load)), { path: "*", Component: NotFound }],
    },
  ];
}

export function createAppRouter() {
  return createBrowserRouter(buildRoutes());
}

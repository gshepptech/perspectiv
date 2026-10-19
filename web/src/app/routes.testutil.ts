import { beforeAll } from "vitest";

import { globPageLoader, pages, type PageLoader, type RouteModule } from "./router";

// The first import of a page is a cold Vite transform of the page and
// everything it imports, which on a loaded host takes longer than a findBy
// query's 1000 ms or a test's 5000 ms. Done inside a test, that cost races the
// test's first assertion. preloadPages does it once per test file in a
// beforeAll with its own timeout, so every test renders a page whose module is
// already in memory.
const PRELOAD_TIMEOUT_MS = 120_000;
const loaded = new Map<string, RouteModule>();

/** Loads the matching route pages before the calling test file's first test. */
export function preloadPages(match: (module: string) => boolean = () => true) {
  beforeAll(async () => {
    const modules = pages.map((p) => p.module).filter(match);
    await Promise.all(modules.map(async (m) => loaded.set(m, await globPageLoader(m))));
  }, PRELOAD_TIMEOUT_MS);
}

/** The app's page loader, answering from the preloaded modules when it can. */
export const preloadedPageLoader: PageLoader = (module) => {
  const mod = loaded.get(module);
  return mod ? Promise.resolve(mod) : globPageLoader(module);
};

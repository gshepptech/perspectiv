# 0001. Pin the frontend forward to Vite 8, TypeScript 7, vitest 5, react-router 8 and lucide-react 1

- Status: accepted
- Date: 2026-10-16
- Deciders: project owner

## Context and Problem Statement

The usual React stack templates pin Vite ^7.3, TypeScript ~5.9, vitest ^4,
react-router ^7.14 Data Mode and lucide-react ^0.577. A registry check on
2026-10-13 found a newer major of each on npm `latest`: Vite 8.3, TypeScript 7.0,
vitest 5.0, react-router 8.4 and lucide-react 1.x. Which majors does the web
app start on?

## Decision Drivers

- A greenfield tree has no configuration or call sites to migrate, so a major
  taken now costs nothing; the same major taken later costs a migration.
- react-router 8 is the release the router's own docs point new apps at;
  starting on 7 means planning that migration straight away.
- Tooling compatibility: TypeScript 7's language service is newer than 5.9's.

## Considered Options

1. Pin forward: Vite 8, TypeScript 7, vitest 5, react-router 8 Data Mode, lucide-react 1.x.
2. Stay on the snapshot: Vite 7, TypeScript 5.9, vitest 4, react-router 7, lucide-react 0.577.
3. Mixed: Vite 8 and vitest 5 with TypeScript 5.9 and react-router 7 plus its v8 future flags.

## Decision Outcome

Chosen option: 1, pin forward. The majors are:

| Package | Major | Verified latest (2026-10-16) |
|---|---|---|
| vite | 8 | 8.3.4 |
| typescript | 7 | 7.0.2 |
| vitest | 5 | 5.0.5 |
| react-router | 8 (Data Mode, `createBrowserRouter`) | 8.4.0 |
| lucide-react | 1 | 1.58.0 |

The rest of the frontend stack stays on its current majors: React 19, Tailwind 4,
TanStack Query 5, Connect-Web 2, `@bufbuild/protobuf` 2, Zod 4, react-hook-form 7,
`@hookform/resolvers` 5, `@playwright/test` 1.

### Consequences

- Good: no migration is owed on any of the five packages.
- Good: react-router 8 removes `react-router-dom`; every import comes from `react-router`.
- Bad: TypeScript 7 drops `baseUrl` and `node10` resolution; `tsconfig.json` uses
  `moduleResolution: "bundler"` and relative `paths` for the `@` alias.
- Bad: vitest 5 enforces top-level `vi.mock` hoisting and clears mocks between tests by default.
- Bad: Vite 8 replaces esbuild and Rollup options with `oxc` and `rolldownOptions`;
  plugins written for Vite 7 may need changes.
- Bad: route match objects expose `loaderData`, not `data`.

### Support Obligation

The web-shell owner keeps `web/package.json` on these majors and re-verifies them
each quarter. A move back to an older major needs a new ADR
that supersedes this one.

## Pros and Cons of the Options

### 1. Pin forward

- Good: current majors from the first commit; no deferred migration.
- Bad: smaller pool of community answers for TypeScript 7 and vitest 5 today.

### 2. Stay on the snapshot

- Good: matches most templates and examples written in 2026.
- Bad: five majors of migration owed on a codebase that did not exist when they shipped.

### 3. Mixed

- Good: defers the TypeScript 7 editor-parity gap.
- Bad: still owes two migrations, and react-router 7 future flags are a moving target.

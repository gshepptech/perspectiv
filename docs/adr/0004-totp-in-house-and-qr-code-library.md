# 0004. Implement TOTP in-house and draw its QR code with qrcode.react

- Status: accepted
- Date: 2026-11-02
- Deciders: project owner (the two-factor sign-in request)

## Context and Problem Statement

Members can turn on two-factor sign-in with an authenticator app. The server
must make RFC 6238 time-based codes (HMAC-SHA-1, six digits, 30-second
steps, which every common app assumes) and check them with one step of
clock drift and no replay; the web app must show the secret as a QR code
drawn in the browser, with no network call. The Go side has no TOTP code
today and the web side has no QR code library. Both choices add or avoid a
dependency, so they are recorded here.

## Decision Drivers

- Correctness against RFC 6238's own test vectors, and control over the
  replay rule (a code is accepted only for a step after the last one used)
  and the drift window, which sit next to Postgres state the library would
  not see.
- The supply chain: every dependency is pinned, scanned and reviewed on bump.
- No runtime call to a third party except the email provider (CLAUDE.md), so
  the QR code is drawn in the page, never fetched from a QR service.
- The design system's tokens: the QR code must draw in `ink` on `paper`.

## Considered Options

Server:

1. Implement RFC 4226 and RFC 6238 in `internal/auth/totp.go` on the standard
   library (`crypto/hmac`, `crypto/sha1`, `encoding/base32`).
2. `github.com/pquerna/otp`, the most used Go TOTP library.

Web:

3. `qrcode.react` 4.2.0, a React component with no runtime dependencies,
   built on Project Nayuki's QR Code generator, drawing SVG.
4. `qrcode` 1.5.4 (node-qrcode), drawing to a canvas or data URL.
5. `qrcode-generator` 2.0.4, a framework-free generator.

## Decision Outcome

Server: option 1. HOTP and TOTP are about thirty lines over `crypto/hmac`;
the replay and drift rules, the base32 secret and the `otpauth://` URI are
ours either way. `TestTOTPMatchesRFC6238Vectors` pins the generator to every
SHA-1 vector in RFC 6238 Appendix B. No Go module is added.

Web: option 3, `qrcode.react` pinned exactly at 4.2.0 in `web/package.json`,
checked against the npm registry on 2026-11-02 (latest 4.2.0, ISC licence,
no dependencies, peer React `^16.8 || ^17 || ^18 || ^19`, lockfile integrity
`sha512-QpgqWi8r…GjA==` matching the registry, about 13.8 million downloads a
week). `QRCodeSVG` draws inline SVG with `fgColor="currentColor"`, so the
code takes the `ink` token; its square sets `color-scheme: light` so it
stays dark on light in the dark theme, as scanners expect.

### Consequences

- Good: no new Go dependency; the TOTP code is small, table-tested against
  the RFC and reviewed with the auth package.
- Good: the QR code is SVG in the page: no request, no canvas, themable by
  token, and named for assistive technology.
- Bad: we own the TOTP code. A mistake there is ours to find; the RFC vector
  test and the integration tests (drift, replay, lockout) are the guard.
- Bad: `qrcode.react` has had no release since December 2024. It is small
  and dependency-free, and the QR format does not change, so a quiet
  release history is acceptable; a security advisory against it means
  replacing it with option 5.
- Neutral: secrets are sealed through `pkg/crypto` with the
  `ENCRYPTION_KEYS` age keys. Rows sealed under an old key stay readable
  while that key is listed; re-sealing them is not yet a background job,
  so an old key can be retired only once no row names it
  (`deploy/k3s/README.md` says how to check).
- Neutral: recovery codes are hashed, not encrypted: SHA-256 over the
  account's id and the normalised code (hash version 2, migration 41), so
  the same code hashes differently for every account. An HMAC keyed from
  `ENCRYPTION_KEYS` was considered and refused: those keys rotate, and a
  hash keyed by a retired key would stop every code stored under it
  working. At 80 random bits a code needs no secret key to resist search;
  the per-account salt is what stops one search covering every account.
  Codes stored unsalted before migration 41 (hash version 1) are checked
  both ways and keep working until used or replaced, rather than being
  deleted in a hard cutover that would lock out a member who already lost
  their phone. Version 1 can be dropped once
  `SELECT count(*) FROM recovery_codes WHERE hash_version = 1 AND used_at IS NULL`
  is zero.

### Support Obligation

The auth package owner keeps `internal/auth/totp.go` and its RFC vector
test, and changes the algorithm, digits or period only with a new ADR, since
every enrolled app depends on them. The web owner tracks `qrcode.react`
through `make updates`; a bump is re-checked against the registry, pinned
exactly, and confirmed by the Settings two-factor tests and the
`two-factor.spec.ts` journey, which turns two-factor sign-in on in a real
browser and signs in with codes computed from the key the page shows.

## Pros and Cons of the Options

### 1. In-house RFC 6238

- Good: standard library only; replay and drift rules live with the state.
- Bad: our code to maintain.

### 2. pquerna/otp

- Good: widely used.
- Bad: a dependency for thirty lines; its validate helpers have no notion of
  a last-used step, so replay would be ours anyway; it pulls in a barcode
  package we would not use server-side.

### 3. qrcode.react

- Good: SVG, React-native, no dependencies, token colours.
- Bad: quiet release history.

### 4. qrcode (node-qrcode)

- Good: the most downloaded QR library.
- Bad: three runtime dependencies (`pngjs`, `yargs`, `dijkstrajs`) for a
  browser that needs none of PNG or a CLI; canvas or data URL output.

### 5. qrcode-generator

- Good: no dependencies.
- Bad: no React component; it returns markup strings we would have to inject.

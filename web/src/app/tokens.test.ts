import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

// Reads the shipped stylesheet, not a copy of the palette, so a token change
// that breaks WCAG 2.1 AA fails here.
const css = readFileSync(resolve(import.meta.dirname, "styles.css"), "utf8");

function block(source: string, start: number): string {
  const open = source.indexOf("{", start);
  let depth = 0;
  for (let i = open; i < source.length; i++) {
    if (source[i] === "{") depth++;
    if (source[i] === "}" && --depth === 0) return source.slice(open + 1, i);
  }
  throw new Error("unbalanced block");
}

// Each token is light-dark(<Paper>, <Lamplight>); side 0 is Paper, 1 Lamplight.
function tokens(body: string, side: 0 | 1): Record<string, string> {
  const out: Record<string, string> = {};
  for (const m of body.matchAll(/--([a-z-]+):\s*light-dark\(\s*(#[0-9a-f]{6})\s*,\s*(#[0-9a-f]{6})\s*\)\s*;/gi)) {
    out[m[1]!] = m[side + 2]!;
  }
  return out;
}

function luminance(hex: string): number {
  const [r, g, b] = [1, 3, 5].map((i) => {
    const c = parseInt(hex.slice(i, i + 2), 16) / 255;
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r! + 0.7152 * g! + 0.0722 * b!;
}

function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi! + 0.05) / (lo! + 0.05);
}

const root = block(css, css.indexOf(":root"));
const light = tokens(root, 0);
const dark = tokens(root, 1);

const textOnBackgrounds = ["ink", "ink-muted", "accent", "success", "warning", "error", "info"];

describe("Folio palette", () => {
  for (const [theme, palette] of [
    ["Paper", light],
    ["Lamplight", dark],
  ] as const) {
    describe(theme, () => {
      it("defines every token", () => {
        for (const name of ["paper", "surface", "rule", "control", "accent-ink", ...textOnBackgrounds]) {
          expect(palette[name], name).toMatch(/^#[0-9a-f]{6}$/i);
        }
      });

      for (const fg of textOnBackgrounds) {
        for (const bg of ["paper", "surface"]) {
          it(`${fg} on ${bg} meets 4.5:1`, () => {
            expect(contrast(palette[fg]!, palette[bg]!)).toBeGreaterThanOrEqual(4.5);
          });
        }
      }

      it("accent-ink on accent meets 4.5:1", () => {
        expect(contrast(palette["accent-ink"]!, palette.accent!)).toBeGreaterThanOrEqual(4.5);
      });

      for (const bg of ["paper", "surface"]) {
        it(`control borders on ${bg} meet the 3:1 component contrast`, () => {
          expect(contrast(palette.control!, palette[bg]!)).toBeGreaterThanOrEqual(3);
        });
      }
    });
  }

  it("lets the device pick the theme unless the reader forces one", () => {
    expect(root).toMatch(/color-scheme:\s*light dark;/);
    expect(block(css, css.indexOf(':root[data-theme="light"]'))).toMatch(/color-scheme:\s*light;/);
    expect(block(css, css.indexOf(':root[data-theme="dark"]'))).toMatch(/color-scheme:\s*dark;/);
  });

  it("cuts all motion to zero under prefers-reduced-motion", () => {
    const at = css.indexOf("@media (prefers-reduced-motion: reduce)");
    expect(at).toBeGreaterThan(-1);
    const body = block(css, at);
    expect(body).toMatch(/transition-duration:\s*0s !important/);
    expect(body).toMatch(/animation-duration:\s*0s !important/);
  });
});

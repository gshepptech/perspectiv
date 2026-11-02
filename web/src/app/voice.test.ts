import { readdirSync, readFileSync } from "node:fs";
import { join, relative, resolve } from "node:path";

import { describe, expect, it } from "vitest";

// docs/design-system.md, Voice: a reader or writer sees "story", never
// "perspective". Code identifiers, URLs and query keys keep `perspective`, so
// this looks for the word used as prose: after a word and a space ("a
// perspective", "otter's perspective"), or opening a string or a line of JSX
// text that goes on in words ("Perspective not found"), or as a whole quoted
// word that does not open a query key ("Perspective", `n === 1 ? "perspective"`).
// It reads lines, not a syntax tree, so a line it misjudges goes in ALLOWED
// with the reason.

const SRC = resolve(import.meta.dirname, "..");
const ROOTS = ["routes", "components"];

/** Lines that may say "perspective" to a reader, keyed by path under src/. */
const ALLOWED: readonly { file: string; line: string; reason: string }[] = [];

// Words that put a code identifier, not prose, after them.
const KEYWORDS = new Set(["const", "let", "var", "return", "await", "typeof", "keyof", "type", "of", "in", "as", "is", "function", "case", "readonly", "extends", "satisfies"]);

const WORD = /(?<![\w./-])[Pp]erspectives?(?![\w(])/g;

/** The prose uses of "perspective" on one line of source, if any. */
function proseHits(line: string): string[] {
  const code = line.replace(/\/\*.*?\*\//g, "").replace(/(^|\s)\/\/.*$/, "");
  if (/^\s*(\*|\/\*|\/\/)/.test(code)) return [];
  const hits: string[] = [];
  for (const m of code.matchAll(WORD)) {
    const before = code.slice(0, m.index);
    const after = code.slice(m.index + m[0].length);
    const word = /([A-Za-z]+)['’]?s?\s$/.exec(before);
    const afterWord = word !== null && !KEYWORDS.has(word[1]!);
    const opensProse = /(^\s*|["`>]\s*)$/.test(before) && /^\s+[a-z]/.test(after);
    const quotedLabel = /["`]$/.test(before) && /^["`]/.test(after) && !/\[\s*["`]$/.test(before);
    if (afterWord || opensProse || quotedLabel) hits.push(m[0]);
  }
  return hits;
}

function sources(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const path = join(dir, e.name);
    if (e.isDirectory()) return sources(path);
    return /\.tsx?$/.test(e.name) && !/\.(test|testutil)\.tsx?$/.test(e.name) ? [path] : [];
  });
}

describe("proseHits", () => {
  it.each([
    ['<h1 className="text-2xl">Write a perspective</h1>'],
    ["        Your perspectives"],
    ["targetName={`${authorName}'s perspective`}"],
    ['{"\'s perspective"}'],
    ['return <Missing title="Perspective not found" />;'],
    ['      return "Perspective";'],
    ["Accepting one links both perspectives into the same lore."],
    ['{ kind: NotificationKind.NEW_PERSPECTIVE_IN_ARC, label: "A new perspective on your day" },'],
    ['{n} {n === 1 ? "perspective" : "perspectives"}'],
  ])("flags %s", (line) => {
    expect(proseHits(line)).not.toEqual([]);
  });

  it.each([
    ['queryKey: ["perspective", "get", perspectiveId],'],
    ["href: `/perspectives/${encodeURIComponent(id)}`,"],
    ["const perspective = useQuery({"],
    ["  perspective?: Perspective;"],
    ["function SavedRow({ p }: { p: Perspective }) {"],
    [".filter((p): p is Perspective => !!p && !p.deleted)"],
    ["const p: Perspective | undefined = perspective.data?.perspective;"],
    ["export function Composer({ place, today, initial, perspective, notice }: ComposerProps) {"],
    [" * the writer goes once the new perspective is published"],
    ["const name = 'Perspectiv'; // the product name"],
    ['import { type Perspective, PerspectiveSummarySchema } from "@/gen/perspectiv/v1/perspective_pb";'],
    ['await queryClient.invalidateQueries({ queryKey: ["perspective"] });'],
  ])("leaves %s alone", (line) => {
    expect(proseHits(line)).toEqual([]);
  });
});

describe("reader-facing copy says story (Voice)", () => {
  it("has no prose 'perspective' in web/src/routes or web/src/components", () => {
    const found: string[] = [];
    for (const root of ROOTS) {
      for (const path of sources(join(SRC, root))) {
        const file = relative(SRC, path);
        readFileSync(path, "utf8")
          .split("\n")
          .forEach((line, i) => {
            if (proseHits(line).length === 0) return;
            if (ALLOWED.some((a) => a.file === file && line.includes(a.line))) return;
            found.push(`${file}:${i + 1}: ${line.trim()}`);
          });
      }
    }
    expect(found).toEqual([]);
  });
});

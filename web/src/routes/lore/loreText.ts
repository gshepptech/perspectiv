import type { Perspective } from "@/gen/perspectiv/v1/perspective_pb";

/** What a lore its members have not named is called wherever a name is required. */
export const UNNAMED = "Unnamed lore";

/**
 * The lore's member stories a reader may read, in the lore's member order:
 * a deleted or hidden story, or one that could not be read, is left out.
 */
export function readableStories(stories: readonly { data?: Perspective | null }[]): Perspective[] {
  return stories.map((s) => s.data).filter((p): p is Perspective => !!p && !p.deleted && !p.hidden);
}

/**
 * The lore's title for the browser tab: its name, or "Unnamed lore in <day
 * title>", never the day's own title, which is the day page's title.
 */
export function loreTabTitle(name: string, dayTitle: string): string {
  return name || (dayTitle ? `${UNNAMED} in ${dayTitle}` : UNNAMED);
}

/** One line of "Who was who": `who` turned out to be `label` in `author`'s story. */
export type CastLine = { id: string; who: string; whoId: string; label: string; author: string };

/**
 * Who was who across a lore's stories: every character whose accepted
 * claimant is one of the lore's members, in story order and then the
 * characters' order within each story.
 */
export function castOf(stories: readonly Perspective[], memberIds: ReadonlySet<string>): CastLine[] {
  return stories.flatMap((p) =>
    p.characters
      .toSorted((a, b) => a.position - b.position)
      .filter((c) => c.claimedBy && memberIds.has(c.claimedBy.userId))
      .map((c) => ({
        id: c.id,
        who: c.claimedBy?.username ?? "",
        whoId: c.claimedBy?.userId ?? "",
        label: c.label,
        author: p.author?.username ?? "",
      })),
  );
}

/** How long a chapter title taken from a story's first words may run, in characters. */
const FIRST_WORDS_CHARS = 40;

/**
 * A story's opening as a title: its first words up to FIRST_WORDS_CHARS
 * characters, cut after the last whole word that fits, with "…" when the
 * story goes on. A first word longer than that is cut inside the word.
 */
export function openingWords(p: Perspective): string {
  const first = p.parts.toSorted((a, b) => a.position - b.position).find((part) => part.body.trim() !== "");
  const words = first?.body.trim().split(/\s+/) ?? [];
  let shown = "";
  for (const w of words) {
    const next = shown ? `${shown} ${w}` : w;
    if ([...next].length > FIRST_WORDS_CHARS) break;
    shown = next;
  }
  if (shown === "" && words.length > 0) shown = [...(words[0] ?? "")].slice(0, FIRST_WORDS_CHARS).join("");
  return shown === words.join(" ") ? shown : `${shown}…`;
}

/** The first letter upper-cased, as a title starts. */
function capitalised(s: string): string {
  const [head = "", ...rest] = [...s];
  return head.toLocaleUpperCase() + rest.join("");
}

/**
 * Each chapter's title (docs/design/reading.md, Lore as a book): its writer's
 * role in the lore, the first character they are in "Who was who" across
 * the chapters, else its story's opening words. The writer's name is never
 * the title, only a fallback for a story with no words at all.
 */
export function chapterTitles(chapters: readonly Perspective[], memberIds: ReadonlySet<string>): string[] {
  const roles = new Map<string, string>();
  for (const line of castOf(chapters, memberIds)) {
    if (!roles.has(line.whoId)) roles.set(line.whoId, line.label);
  }
  return chapters.map((p) => {
    const role = roles.get(p.author?.userId ?? "");
    if (role) return capitalised(role);
    return openingWords(p) || p.author?.username || "A story";
  });
}

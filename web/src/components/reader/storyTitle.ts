import { PerspectiveStatus } from "@/gen/perspectiv/v1/enums_pb";
import type { Perspective } from "@/gen/perspectiv/v1/perspective_pb";

/** How many of a draft's first words title it when no day title does. */
const TITLE_WORDS = 6;

/** The draft's first words, at most TITLE_WORDS, with "…" when the text goes on. */
function firstWords(p: Perspective): string {
  const first = [...p.parts].sort((a, b) => a.position - b.position).find((part) => part.body.trim() !== "");
  const words = first?.body.trim().split(/\s+/) ?? [];
  if (words.length === 0) return "";
  const shown = words.slice(0, TITLE_WORDS).join(" ");
  return words.length > TITLE_WORDS ? `${shown}…` : shown;
}

/**
 * What a list names one of the writer's stories by. A published story, and a
 * draft that joins a day that already exists, take the day's title (the
 * server fills arcTitle for such a draft); a draft that would start its day
 * takes the title its writer gave it; a draft with neither takes its first
 * words, and an empty draft is "Untitled draft". "Untitled" alone never
 * names anything.
 */
export function storyTitle(p: Perspective): string {
  if (p.arcTitle) return p.arcTitle;
  // A published story always has a day; only one filed at a hidden place
  // comes without its title.
  if (p.status !== PerspectiveStatus.DRAFT) return "A story";
  return p.titleIfNewArc || firstWords(p) || "Untitled draft";
}

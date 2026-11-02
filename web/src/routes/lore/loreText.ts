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

import type { Profile } from "@/gen/perspectiv/v1/user_pb";

/**
 * One count of the track record: the words before the number, the number,
 * and the words after it ("12 stories", "Linked with 5 writers").
 */
type Stat = { key: string; before: string; n: number; after: string };

/** The words after a count, one or many: "1 story", "12 stories". */
function counted(key: string, n: number, one: string, many: string, before = ""): Stat {
  return { key, before, n, after: n === 1 ? one : many };
}

/**
 * The writer's track record: four counts of what a reader can open, each a
 * list item a screen reader reads as one phrase ("12 stories", "Linked with
 * 5 writers"). The server counts only readable stories; see ProfileStats.
 */
export function TrackRecord({ profile }: { profile: Profile }) {
  const stats = [
    counted("stories", profile.storyCount, "story", "stories"),
    counted("upvotes", profile.upvotesReceived, "upvote", "upvotes"),
    counted("linked", profile.peopleFound, "writer", "writers", "Linked with"),
    counted("places", profile.placesWritten, "place", "places"),
  ];
  return (
    <ul aria-label="Track record" className="mt-6 flex flex-wrap gap-x-6 gap-y-2 text-sm">
      {stats.map((s) => (
        <li key={s.key} className="min-w-0 text-ink-muted">
          {s.before && `${s.before} `}
          <span className="font-bold text-ink">{s.n.toLocaleString("en-US")}</span> {s.after}
        </li>
      ))}
    </ul>
  );
}

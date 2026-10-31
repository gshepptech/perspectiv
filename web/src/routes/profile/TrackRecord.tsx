import type { Profile } from "@/gen/perspectiv/v1/user_pb";

/** "1 story", "12 stories": a count and its word, as one phrase. */
function stat(n: number, one: string, many: string) {
  return { n, word: n === 1 ? one : many };
}

/**
 * The writer's track record: four counts of what a reader can open, each a
 * list item a screen reader reads as one phrase ("12 stories"). The server
 * counts only readable stories; see ProfileStats.
 */
export function TrackRecord({ profile }: { profile: Profile }) {
  const stats = [
    stat(profile.storyCount, "story", "stories"),
    stat(profile.upvotesReceived, "upvote", "upvotes"),
    stat(profile.peopleFound, "person found", "people found"),
    stat(profile.placesWritten, "place", "places"),
  ];
  return (
    <ul aria-label="Track record" className="mt-6 flex flex-wrap gap-x-6 gap-y-2 text-sm">
      {stats.map((s) => (
        <li key={s.word} className="min-w-0 text-ink-muted">
          <span className="font-bold text-ink">{s.n.toLocaleString("en-US")}</span> {s.word}
        </li>
      ))}
    </ul>
  );
}

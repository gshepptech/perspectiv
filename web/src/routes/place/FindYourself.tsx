import { useQuery } from "@tanstack/react-query";
import { Search } from "lucide-react";
import { useId, useState } from "react";
import { Link } from "react-router";

import { formatDay } from "@/components/reader/format";
import { Input } from "@/components/ui/Input";
import { clients } from "@/lib/connect";
import { useDebounced } from "@/routes/compose/search";

const MIN_QUERY = 2;

/**
 * "Were you in someone's story?": search the descriptions nobody has claimed
 * in stories at this place and below, on any day, and jump straight to
 * claiming one. The server matches anywhere in a description, ignoring case.
 */
export function FindYourself({ placeId, placeName }: { placeId: string; placeName: string }) {
  const fieldId = useId();
  const [text, setText] = useState("");
  const query = useDebounced(text.trim().replace(/\s+/g, " "), 250);
  const ready = [...query].length >= MIN_QUERY;
  const found = useQuery({
    queryKey: ["place", "searchCharacters", { placeId, query }],
    queryFn: ({ signal }) => clients.place.searchCharacters({ placeId, query }, { signal }),
    enabled: ready,
  });
  const chars = found.data?.characters ?? [];
  return (
    <section aria-labelledby={`${fieldId}-title`} className="min-w-0 rounded-md border border-rule bg-paper p-4 text-sm">
      <h2 id={`${fieldId}-title`} className="font-sans text-base font-medium">
        Were you in someone's story?
      </h2>
      <p className="mt-1 text-ink-muted">Search how people at {placeName} described the strangers they saw.</p>
      <label htmlFor={fieldId} className="sr-only">
        Search descriptions
      </label>
      <div className="relative mt-3">
        <Search aria-hidden size={16} className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-ink-muted" />
        <Input
          id={fieldId}
          type="search"
          autoComplete="off"
          maxLength={60}
          placeholder="gold paint, red umbrella…"
          value={text}
          onChange={(e) => setText(e.target.value)}
          className="pl-9"
        />
      </div>
      {ready && found.isError && (
        <p role="alert" className="mt-3 text-error">
          The search failed. Try again in a moment.
        </p>
      )}
      {/* Always mounted, so a screen reader hears each search's result. */}
      <p role="status" className={chars.length > 0 ? "sr-only" : "mt-3 text-ink-muted empty:hidden"}>
        {ready && found.isSuccess
          ? chars.length === 0
            ? `Nobody unclaimed matches “${query}” here yet.`
            : `${chars.length} ${chars.length === 1 ? "description matches" : "descriptions match"} “${query}”.`
          : ""}
      </p>
      {chars.length > 0 && (
        <ul aria-label="Matching descriptions" className="mt-3 grid gap-3">
          {chars.map((c) => (
            <li key={c.characterId} className="min-w-0">
              <p className="font-serif text-base wrap-anywhere text-ink">“{c.label}”</p>
              <p className="text-ink-muted wrap-anywhere">
                {c.author?.username ?? "Someone"} at {c.place?.name}, {formatDay(c.localDate)}
              </p>
              <Link
                to={`/perspectives/${encodeURIComponent(c.perspectiveId)}?claim=1`}
                aria-label={`That was me: ${c.label}`}
                className="inline-flex min-h-9 items-center font-medium text-accent underline underline-offset-4"
              >
                That was me
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

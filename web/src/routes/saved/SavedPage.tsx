import { useInfiniteQuery } from "@tanstack/react-query";
import { useState } from "react";

import { savedListKey } from "@/components/actions/queries";
import { SaveButton } from "@/components/actions/SaveButton";
import { StoryLinkRow } from "@/components/reader/StoryLinkRow";
import { Button } from "@/components/ui/Button";
import type { Perspective } from "@/gen/perspectiv/v1/perspective_pb";
import { clients } from "@/lib/connect";
import { useShowMore } from "@/lib/showMore";
import { useDocumentTitle } from "@/lib/title";

const PAGE_SIZE = 20;

/**
 * One saved story with its Save toggle. Unsaving keeps the row, saying
 * "Removed." with the toggle turned to Undo, until the reader leaves the
 * page; the toggle is one button throughout, so focus stays on it.
 */
function SavedRow({ p }: { p: Perspective }) {
  const [saved, setSaved] = useState(true);
  return (
    <StoryLinkRow p={p} withAuthor>
      {/* While saved the toggle starts the row, so it sits flush with the text. */}
      <div className="mt-1 flex flex-wrap items-center gap-x-2">
        {!saved && (
          <span aria-hidden className="text-sm text-ink-muted">
            Removed.
          </span>
        )}
        <SaveButton
          perspectiveId={p.id}
          saved={saved}
          targetName={`${p.author?.username ?? "someone"}'s perspective`}
          onSaved={setSaved}
          undo
        />
        {/* Always rendered, so the word is announced when it appears. */}
        <span role="status" className="sr-only">
          {saved ? "" : "Removed."}
        </span>
      </div>
    </StoryLinkRow>
  );
}

/**
 * The stories the caller saved to read later, most recently saved first, a
 * page at a time. The server leaves out a saved story that is no longer
 * readable, and brings it back if it is restored. A story unsaved here
 * stays listed until the page is left; the list is read afresh next time.
 */
export function Component() {
  useDocumentTitle("Saved stories");
  const list = useInfiniteQuery({
    queryKey: savedListKey,
    queryFn: ({ pageParam, signal }) => clients.perspective.listSaved({ pageToken: pageParam, pageSize: PAGE_SIZE }, { signal }),
    initialPageParam: "",
    getNextPageParam: (last) => last.nextPageToken || undefined,
  });
  const stories = list.data?.pages.flatMap((page) => page.perspectives) ?? [];
  const more = useShowMore(stories.length, list.fetchNextPage);
  return (
    <section aria-labelledby="saved-heading" className="mx-auto min-w-0 max-w-measure">
      <h1 id="saved-heading" className="text-2xl sm:text-3xl">
        Saved stories
      </h1>
      <p className="mt-2 text-ink-muted">Stories you saved to read later, most recently saved first.</p>
      {list.isPending && (
        <p role="status" className="mt-6 text-ink-muted">
          Loading…
        </p>
      )}
      {list.isError && (
        <p role="alert" className="mt-6 text-sm text-error">
          Your saved stories could not be loaded. Try again in a moment.
        </p>
      )}
      {list.isSuccess && stories.length === 0 && (
        <p className="mt-6 text-ink-muted">You have not saved any stories yet. Press Save under a story to keep it here.</p>
      )}
      {stories.length > 0 && (
        <ul ref={more.list} aria-label="Saved stories" className="mt-6 divide-y divide-rule border-y border-rule">
          {stories.map((p) => (
            <SavedRow key={p.id} p={p} />
          ))}
        </ul>
      )}
      {list.hasNextPage && (
        <Button variant="secondary" className="mt-3" pending={list.isFetchingNextPage} onClick={more.showMore}>
          Show more
        </Button>
      )}
    </section>
  );
}

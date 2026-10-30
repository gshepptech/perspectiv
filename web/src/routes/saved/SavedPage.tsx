import { useInfiniteQuery } from "@tanstack/react-query";

import { savedListKey } from "@/components/actions/queries";
import { StoryLinkRow } from "@/components/reader/StoryLinkRow";
import { Button } from "@/components/ui/Button";
import { clients } from "@/lib/connect";
import { useShowMore } from "@/lib/showMore";
import { useDocumentTitle } from "@/lib/title";

const PAGE_SIZE = 20;

/**
 * The stories the caller saved to read later, most recently saved first, a
 * page at a time. The server leaves out a saved story that is no longer
 * readable, and brings it back if it is restored.
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
            <StoryLinkRow key={p.id} p={p} withAuthor />
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

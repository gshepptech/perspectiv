import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router";

import { categorySlug, formatDayBeside, placeCategories, placeCategoryLabel } from "@/components/reader/format";
import { storyTitle } from "@/components/reader/storyTitle";
import { PLACE_SEARCH_DEBOUNCE_MS, PlaceSearch, useDebounced } from "@/components/search/PlaceSearch";
import { Button } from "@/components/ui/Button";
import { PerspectiveStatus } from "@/gen/perspectiv/v1/enums_pb";
import type { Perspective } from "@/gen/perspectiv/v1/perspective_pb";
import { clients } from "@/lib/connect";
import { useShowMore } from "@/lib/showMore";
import { useSession } from "@/lib/session";

import { ExampleArc, HowItWorks } from "./ExampleArc";
import { Following } from "./Following";
import { Storied } from "./Storied";
import { WelcomeBack } from "./WelcomeBack";

/**
 * Links set as a run-in list, like a book's index: words in a wrapping line
 * separated by middots, each link 44px tall (Folio, Landing).
 */
function RunIn({ links }: { links: { key: string; to: string; label: string }[] }) {
  return (
    <ul className="mt-3 flex flex-wrap items-center">
      {links.map((l, i) => (
        // The middot follows its word, so a wrapped line never starts with one.
        <li key={l.key} className="inline-flex items-center">
          <Link to={l.to} className="inline-flex min-h-11 items-center text-accent underline-offset-4 hover:underline">
            {l.label}
          </Link>
          {i < links.length - 1 && (
            <span aria-hidden className="px-2 text-ink-muted">
              ·
            </span>
          )}
        </li>
      ))}
    </ul>
  );
}

/**
 * The kinds of place with at least one day, each linking to the best days at
 * every place of that kind. Left out while loading, on error and when none
 * has a day: the front door does not show a broken row.
 */
function BrowseKinds() {
  const kinds = useQuery({
    queryKey: ["place", "listStoriedCategories", {}],
    queryFn: ({ signal }) => clients.place.listStoriedCategories({}, { signal }),
  });
  const storied = new Set(kinds.data?.categories ?? []);
  const shown = placeCategories.filter((c) => storied.has(c));
  if (shown.length === 0) return null;
  return (
    <nav aria-labelledby="browse-kinds" className="mt-16">
      <h2 id="browse-kinds" className="text-xl">
        Browse by kind of place
      </h2>
      <RunIn links={shown.map((c) => ({ key: String(c), to: `/best/${categorySlug(c)}`, label: placeCategoryLabel(c) }))} />
    </nav>
  );
}

function BrowseStates() {
  const states = useQuery({
    queryKey: ["place", "listChildren", { parentId: "" }],
    queryFn: ({ signal }) => clients.place.listChildren({ parentId: "", pageSize: 100 }, { signal }),
  });
  if (!states.data || states.data.places.length === 0) return null;
  return (
    <nav aria-label="Browse by state" className="mt-16">
      <h2 className="text-xl">Browse by state</h2>
      <RunIn links={states.data.places.map((state) => ({ key: state.id, to: `/places/${state.id}`, label: state.name }))} />
    </nav>
  );
}

const MINE_PAGE_SIZE = 20;

/**
 * Deletes a draft the writer's own published story has made impossible to
 * publish. One press, nothing automatic; the list is read again from the
 * server afterwards.
 */
function DeleteDraft({ p, title, onDeleted }: { p: Perspective; title: string; onDeleted: () => void }) {
  const queryClient = useQueryClient();
  const remove = useMutation({
    mutationFn: () => clients.perspective.delete({ perspectiveId: p.id }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["perspective", "listMine"] });
      onDeleted();
    },
  });
  return (
    <span className="flex flex-col items-end">
      <Button
        variant="ghost"
        size="sm"
        className="min-h-11 text-accent underline underline-offset-4"
        aria-label={`Delete draft ${title}`}
        pending={remove.isPending}
        onClick={() => remove.mutate()}
      >
        Delete
      </Button>
      {remove.isError && (
        <span role="alert" className="text-sm text-error">
          Not deleted. Try again in a moment.
        </span>
      )}
    </span>
  );
}

function MineRow({ p, onDeleted }: { p: Perspective; onDeleted: () => void }) {
  const isDraft = p.status === PerspectiveStatus.DRAFT;
  const title = storyTitle(p);
  // The writer's own published story holds this draft's day or hour, so it
  // can never be published there.
  const superseded = isDraft && p.supersededById !== "";
  const edit = `/compose?perspectiveId=${encodeURIComponent(p.id)}`;
  const where = [p.place?.name, formatDayBeside(p.arcTitle || p.titleIfNewArc, p.localDate, p.hour)].filter(Boolean).join(" · ");
  return (
    <li className="flex min-w-0 flex-wrap items-center justify-between gap-x-4 py-2">
      <Link to={isDraft ? edit : `/perspectives/${encodeURIComponent(p.id)}`} className="flex min-h-11 min-w-0 flex-col justify-center">
        <span className="font-serif text-lg wrap-anywhere text-accent underline-offset-4 hover:underline">{title}</span>
        {superseded ? (
          <span className="text-sm wrap-anywhere text-warning">
            Draft · You already published for this {p.hour === undefined ? "day" : "hour"}
          </span>
        ) : (
          <span className="text-sm wrap-anywhere text-ink-muted">
            {isDraft ? "Draft · " : "Published · "}
            {where}
          </span>
        )}
      </Link>
      {!isDraft && (
        <Link to={edit} aria-label={`Edit ${title}`} className="inline-flex min-h-11 items-center text-sm text-accent underline underline-offset-4">
          Edit
        </Link>
      )}
      {isDraft && !superseded && (
        <Link to={edit} aria-label={`Continue ${title}`} className="inline-flex min-h-11 items-center text-sm text-accent underline underline-offset-4">
          Continue
        </Link>
      )}
      {superseded && <DeleteDraft p={p} title={title} onDeleted={onDeleted} />}
    </li>
  );
}

/**
 * The signed-in author's own drafts and published perspectives, newest edit
 * first. It is their own writing, not a feed: signed-out readers see nothing
 * here and nothing of anyone else's is listed.
 */
function YourPerspectives() {
  const heading = useRef<HTMLHeadingElement>(null);
  const mine = useInfiniteQuery({
    queryKey: ["perspective", "listMine", {}],
    queryFn: ({ pageParam, signal }) => clients.perspective.listMine({ pageToken: pageParam, pageSize: MINE_PAGE_SIZE }, { signal }),
    initialPageParam: "",
    getNextPageParam: (last) => last.nextPageToken || undefined,
  });
  const items = mine.data?.pages.flatMap((page) => page.perspectives) ?? [];
  const more = useShowMore(items.length, mine.fetchNextPage);
  if (mine.isPending) return null;
  if (mine.isError) {
    return (
      <p role="alert" className="mt-12 text-sm text-error">
        Your stories could not be loaded. Try again in a moment.
      </p>
    );
  }
  if (items.length === 0) return null;
  return (
    <section aria-labelledby="landing-mine" className="mt-12">
      <h2 id="landing-mine" ref={heading} tabIndex={-1} className="text-xl focus:outline-none">
        Your stories
      </h2>
      <ul ref={more.list} className="mt-3 divide-y divide-rule border-y border-rule">
        {items.map((p) => (
          // A deleted row leaves the list, so focus goes to the heading.
          <MineRow key={p.id} p={p} onDeleted={() => heading.current?.focus()} />
        ))}
      </ul>
      {mine.hasNextPage && (
        <Button variant="secondary" className="mt-3" pending={mine.isFetchingNextPage} onClick={more.showMore}>
          Show more
        </Button>
      )}
    </section>
  );
}

/**
 * The landing page is a place search over the hierarchy and no feed; a
 * signed-in author also finds their own drafts and perspectives here. Results are links to place pages. The query lives in the address
 * (replacing the entry as the reader types) so the back button from a place
 * returns to the same results.
 */
export function Component() {
  const [params, setParams] = useSearchParams();
  const [text, setText] = useState(() => params.get("q") ?? "");
  const query = useDebounced(text.trim(), PLACE_SEARCH_DEBOUNCE_MS);
  const session = useSession();

  useEffect(() => {
    if ((params.get("q") ?? "") === query) return;
    setParams(query ? { q: query } : {}, { replace: true });
  }, [query, params, setParams]);

  const search = (
    <>
      <PlaceSearch text={text} onTextChange={setText} query={query} large className={session ? "mt-8" : "mx-auto mt-10 max-w-xl"} />
      {!query && !session && <p className="mt-3 text-center text-sm text-ink-muted">Anyone can read. Sign up to write your side.</p>}
    </>
  );

  // A returning member already knows what Perspectiv is: what is waiting on
  // them and their own writing come first, then the search.
  if (session) {
    return (
      <section className="mx-auto max-w-3xl min-w-0 pt-6 pb-16 sm:pt-10" aria-labelledby="landing-title">
        <WelcomeBack username={session.username} />
        <YourPerspectives />
        <div className="max-w-xl min-w-0">{search}</div>
        <Following />
        <Storied />
        <BrowseKinds />
        <BrowseStates />
      </section>
    );
  }

  // The front door is a book's title page (Folio, Landing): the promise, an
  // ornament rule, what Perspectiv is, the search, then the example as two
  // facing pages and how it works.
  return (
    <section className="mx-auto max-w-3xl min-w-0 pb-16" aria-labelledby="landing-title">
      <header className="pt-10 text-center sm:pt-20">
        <h1 id="landing-title" className="mx-auto max-w-measure text-4xl leading-tight text-balance sm:text-5xl">
          Same place. Same day. Every side of the story.
        </h1>
        <hr aria-hidden className="mx-auto mt-8 w-16 border-rule" />
        <p className="mx-auto mt-8 max-w-measure font-serif text-prose text-ink-muted">
          Perspectiv is where people who were in the same place at the same time each write what they saw, and find
          the strangers who were in their story.
        </p>
      </header>
      <div className="min-w-0">{search}</div>
      <div className="mt-16 min-w-0">
        <ExampleArc />
      </div>
      <div className="mt-12 min-w-0">
        <HowItWorks />
      </div>
      <Storied />
      <BrowseKinds />
      <BrowseStates />
    </section>
  );
}

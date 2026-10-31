import { useInfiniteQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Link, useParams, useSearchParams } from "react-router";

import { ArcItem } from "@/components/reader/ArcItem";
import { categoryFromSlug, placeCategoryPlural } from "@/components/reader/format";
import { Button } from "@/components/ui/Button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/Tabs";
import { EpicTab, type PlaceCategory } from "@/gen/perspectiv/v1/enums_pb";
import { clients } from "@/lib/connect";
import { useShowMore } from "@/lib/showMore";
import { useDocumentTitle } from "@/lib/title";

// The tab lives in the address (?tab=newest) so a listing is linkable;
// Ranked is the bare address.
const tabs = [
  { slug: "ranked", label: "Ranked", tab: EpicTab.RANKED, said: "Ranked by canon." },
  { slug: "newest", label: "Newest", tab: EpicTab.NEWEST, said: "Newest first." },
] as const;

const linkClass = "inline-flex min-h-11 items-center text-accent underline underline-offset-4";
type TabSlug = (typeof tabs)[number]["slug"];

function parseTab(value: string | null): TabSlug {
  return tabs.find((t) => t.slug === value)?.slug ?? "ranked";
}

/** The days at every visible place of one category, in the server's order. */
function BestList({ category, tab, plural }: { category: PlaceCategory; tab: EpicTab; plural: string }) {
  const days = useInfiniteQuery({
    queryKey: ["place", "getEpic", { placeId: "", tab, category }],
    queryFn: ({ pageParam, signal }) => clients.place.getEpic({ tab, category, pageToken: pageParam }, { signal }),
    initialPageParam: "",
    getNextPageParam: (last) => last.nextPageToken || undefined,
  });
  const arcs = days.data?.pages.flatMap((p) => p.arcs) ?? [];
  const more = useShowMore(arcs.length, days.fetchNextPage);
  if (days.isPending) {
    return (
      <p role="status" className="text-sm text-ink-muted">
        Loading…
      </p>
    );
  }
  if (days.isError) {
    return (
      <p role="alert" className="text-sm text-error">
        This list could not be loaded. Try again in a moment.
      </p>
    );
  }
  if (arcs.length === 0) {
    return (
      <p className="text-ink-muted">
        No one has written about a day at {plural} yet.{" "}
        <Link to="/" className={linkClass}>
          Search for a place
        </Link>
      </p>
    );
  }
  return (
    <>
      <ul ref={more.list} aria-label="Days" className="divide-y divide-rule">
        {arcs.map((arc) => (
          <ArcItem key={arc.arcId} arc={arc} withCity />
        ))}
      </ul>
      {days.hasNextPage && (
        <Button variant="secondary" className="mt-4" pending={days.isFetchingNextPage} onClick={more.showMore}>
          {days.isFetchingNextPage ? "Loading…" : "Show more"}
        </Button>
      )}
    </>
  );
}

function UnknownKind() {
  return (
    <section className="mx-auto max-w-measure py-12" aria-labelledby="best-missing">
      <h1 id="best-missing" className="text-2xl sm:text-3xl">
        Kind of place not found
      </h1>
      <p className="mt-3 text-ink-muted">This address does not name a kind of place.</p>
      <p className="mt-6">
        <Link to="/" className={linkClass}>
          Search for a place
        </Link>
      </p>
    </section>
  );
}

/**
 * The best days at one kind of place (docs/design-system.md, Best days at a
 * kind of place): the place page's epic across every place of a category,
 * Ranked by canon or Newest.
 */
export function Component() {
  const { category: slug = "" } = useParams();
  const [params, setParams] = useSearchParams();
  // Empty until the reader picks an order, so arriving announces nothing.
  const [orderSaid, setOrderSaid] = useState("");
  const category = categoryFromSlug(slug);
  const plural = category === undefined ? "" : placeCategoryPlural(category);
  const title = category === undefined ? "Kind of place not found" : `The best days at ${plural}`;
  useDocumentTitle(title);
  if (category === undefined) return <UnknownKind />;

  const tab = parseTab(params.get("tab"));
  const epicTab = tabs.find((t) => t.slug === tab)!.tab;
  function pick(value: string) {
    setOrderSaid(tabs.find((t) => t.slug === value)?.said ?? "");
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (value === "ranked") next.delete("tab");
        else next.set("tab", value);
        return next;
      },
      { replace: true },
    );
  }

  return (
    <section className="min-w-0" aria-labelledby="best-title">
      <header className="min-w-0">
        <h1 id="best-title" className="text-2xl wrap-anywhere sm:text-3xl">
          {title}
        </h1>
        <p className="mt-2 text-ink-muted">Days ranked by their canon across every place of this kind.</p>
      </header>
      <div className="mt-8 min-w-0 max-w-3xl">
        <p role="status" className="sr-only">
          {orderSaid}
        </p>
        <Tabs value={tab} onValueChange={pick}>
          <TabsList aria-label="Order">
            {tabs.map((t) => (
              <TabsTrigger key={t.slug} value={t.slug}>
                {t.label}
              </TabsTrigger>
            ))}
          </TabsList>
          {tabs.map((t) => (
            <TabsContent key={t.slug} value={t.slug}>
              {t.slug === tab && <BestList category={category} tab={epicTab} plural={plural} />}
            </TabsContent>
          ))}
        </Tabs>
      </div>
    </section>
  );
}

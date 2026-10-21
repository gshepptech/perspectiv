import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Code, ConnectError } from "@connectrpc/connect";
import { ArrowLeft, ArrowRight, Clock, Crown, PenLine } from "lucide-react";
import { Fragment, useState, type ReactNode } from "react";
import { Link, useLocation, useParams } from "react-router";

import type { ViewerRights } from "@/components/actions/PerspectiveActions";
import { ReportDialog } from "@/components/actions/ReportDialog";
import { Button } from "@/components/ui/Button";
import type { Arc, ArcLore, GetArcResponse, HourArc, ListArcSectionResponse } from "@/gen/perspectiv/v1/arc_pb";
import { ArcSection, ReportTargetKind } from "@/gen/perspectiv/v1/enums_pb";
import type { PerspectiveSummary } from "@/gen/perspectiv/v1/perspective_pb";
import { clients } from "@/lib/connect";
import { useSession, type Session } from "@/lib/session";

import { AuthorLink } from "@/components/reader/AuthorLink";

import { dateLabel, hourLabel } from "./labels";
import { PerspectiveCard } from "./PerspectiveCard";

export const arcKey = (arcId: string) => ["arc", "getArc", { arcId }] as const;

/** How many entries each further page of a list holds. */
const PAGE_SIZE = 20;

/**
 * The viewer's rights in this arc, from where the server says they stand: a
 * participant is anyone with a published perspective here, in any standing
 * (a REMOVED perspective is still published, and the server counts it for
 * votes and claims). A remove vote needs the viewer to be a member of the
 * target's own lore, and the lore 4 members or more. The lists are paged, so
 * the viewer's own perspective may not be on screen.
 */
function rightsIn(res: GetArcResponse, session: Session | null) {
  const mineId = session ? res.viewer?.perspectiveId || "" : "";
  const participant = mineId !== "";
  const forLore = (lore: ArcLore | undefined): ViewerRights => ({
    participant,
    loreVoter: !!session && !!lore?.lore && lore.lore.memberCount >= 4 && lore.lore.id === res.viewer?.loreId,
  });
  return { mineId, participant, forLore, outsideLore: { participant, loreVoter: false } satisfies ViewerRights };
}

/**
 * The pages of one list after the first, which GetArc returned. Nothing is
 * fetched until the reader asks for more.
 */
function useMore<T>(arcId: string, section: ArcSection, firstToken: string, pick: (page: ListArcSectionResponse) => T[]) {
  const [asked, setAsked] = useState(false);
  const more = useInfiniteQuery({
    queryKey: ["arc", "listArcSection", { arcId, section, firstToken }],
    queryFn: ({ pageParam, signal }) =>
      clients.arc.listArcSection({ arcId, section, pageToken: pageParam, pageSize: PAGE_SIZE }, { signal }),
    initialPageParam: firstToken,
    getNextPageParam: (last) => last.nextPageToken || undefined,
    enabled: asked && firstToken !== "",
  });
  return {
    items: more.data?.pages.flatMap(pick) ?? [],
    hasMore: asked ? more.hasNextPage : firstToken !== "",
    loading: more.isFetching,
    failed: more.isError,
    showMore: () => (asked ? void more.fetchNextPage() : setAsked(true)),
  };
}

function MoreButton({ label, more }: { label: string; more: ReturnType<typeof useMore> }) {
  return (
    <>
      {more.failed && (
        <p role="alert" className="mt-3 text-sm text-error">
          More could not be loaded. Try again in a moment.
        </p>
      )}
      {more.hasMore && (
        <Button variant="secondary" className="mt-4" disabled={more.loading} onClick={more.showMore}>
          {label}
        </Button>
      )}
    </>
  );
}

function composeHref(arc: Arc): string {
  const q = new URLSearchParams({ placeId: arc.place?.id ?? "", date: arc.localDate });
  if (arc.hour !== undefined) q.set("hour", String(arc.hour));
  return `/compose?${q.toString()}`;
}

function Section({ id, title, children }: { id: string; title: string; children: ReactNode }) {
  return (
    <section aria-labelledby={id} className="mt-10 min-w-0">
      <h2 id={id} className="border-b border-rule pb-2 text-xl">
        {title}
      </h2>
      {children}
    </section>
  );
}

function CardList({ items, rights, level = 3 }: { items: PerspectiveSummary[]; rights?: ViewerRights; level?: 3 | 4 }) {
  return (
    <ul className="mt-4 grid min-w-0 gap-4">
      {items.map((p) => (
        <li key={p.id} className="min-w-0">
          <PerspectiveCard summary={p} rights={rights} level={level} />
        </li>
      ))}
    </ul>
  );
}

function loreLine(l: ArcLore): string {
  const lore = l.lore;
  if (!lore) return "";
  return `${lore.memberCount} ${lore.memberCount === 1 ? "member" : "members"} · ${lore.upvotes} ${lore.upvotes === 1 ? "upvote" : "upvotes"}`;
}

/**
 * A day arc's hours in ascending hour order, each with its canon's perspectives
 * stitched in read-only: voting and claiming in an hour depend on who wrote in
 * that hour's arc, so those actions live on the hour arc's own page.
 */
function HourTimeline({ arcId, first, firstToken }: { arcId: string; first: HourArc[]; firstToken: string }) {
  const more = useMore(arcId, ArcSection.HOURS, firstToken, (p) => p.hours);
  const sorted = [...first, ...more.items].sort((a, b) => (a.arc?.hour ?? 0) - (b.arc?.hour ?? 0));
  return (
    <Section id="arc-hours" title="Hours">
      <ol className="mt-4 grid min-w-0 gap-6 border-l-2 border-rule pl-4">
        {sorted.map((h) => {
          const a = h.arc;
          if (!a) return null;
          const at = hourLabel(a.hour ?? 0);
          return (
            <li key={a.id} className="min-w-0">
              <h3 className="flex flex-wrap items-baseline gap-x-2 text-xl">
                <span className="font-sans text-base font-bold text-ink-muted">{at}</span>{" "}
                <span className="min-w-0 break-words">{a.title}</span>
              </h3>
              <p className="mt-1 flex flex-wrap gap-x-3 text-sm text-ink-muted">
                <span>
                  {h.perspectiveCount} {h.perspectiveCount === 1 ? "perspective" : "perspectives"}
                </span>
                <Link to={`/arcs/${a.id}`} className="font-medium text-accent underline underline-offset-4">
                  Open the {at} arc
                </Link>
              </p>
              {h.canon && h.canon.perspectives.length > 0 ? (
                <CardList items={h.canon.perspectives} level={4} />
              ) : (
                <p className="mt-2 text-sm text-ink-muted">No canon yet</p>
              )}
            </li>
          );
        })}
      </ol>
      <MoreButton label="Show more hours" more={more} />
    </Section>
  );
}

function ArcView({ res }: { res: GetArcResponse }) {
  const session = useSession();
  const arc = res.arc;
  if (!arc) return <Missing />;
  const rights = rightsIn(res, session);
  const isHour = arc.hour !== undefined;
  // Arriving from an invite: the reader has just written their side here.
  const invited = (useLocation().state as { invited?: boolean } | null)?.invited === true;
  return (
    <article aria-labelledby="arc-title" className="min-w-0">
      {invited && (
        <p role="status" className="mb-6 rounded-xl border border-success p-4 text-ink">
          Your side is published. If you are someone in another story here, open it and use{" "}
          <strong className="font-medium">Claim a character</strong> so your stories link.
        </p>
      )}
      <header className="min-w-0">
        <nav aria-label="Place" className="text-sm text-ink-muted">
          <ol className="flex flex-wrap items-center gap-x-1">
            {arc.placeChain.map((p, i) => (
              <Fragment key={p.id}>
                {i > 0 && <li aria-hidden>›</li>}
                <li className="min-w-0 break-words">
                  <Link to={`/places/${p.id}`} className="underline-offset-4 hover:text-ink hover:underline">
                    {p.name}
                  </Link>
                </li>
              </Fragment>
            ))}
          </ol>
        </nav>
        <h1 id="arc-title" className="mt-2 text-2xl break-words sm:text-3xl">
          {arc.title}
        </h1>
        <p className="mt-1 flex flex-wrap items-center gap-x-3 text-ink-muted">
          <span>{dateLabel(arc.localDate)}</span>
          {isHour && (
            <span className="inline-flex items-center gap-1">
              <Clock aria-hidden size={16} />
              {hourLabel(arc.hour ?? 0)}
            </span>
          )}
          {arc.createdBy?.username && <span className="text-sm">Started by {arc.createdBy.username}</span>}
        </p>
        {isHour && arc.parentArcId && (
          <p className="mt-2">
            <Link
              to={`/arcs/${arc.parentArcId}`}
              className="inline-flex min-h-9 items-center gap-1 text-sm font-medium text-accent underline underline-offset-4"
            >
              <ArrowLeft aria-hidden size={16} />
              The whole day
            </Link>
          </p>
        )}
        <div className="mt-4 flex flex-wrap items-center gap-2">
          {rights.mineId ? (
            <Button asChild variant="secondary" size="sm">
              <Link to={`/perspectives/${rights.mineId}`}>Read yours</Link>
            </Button>
          ) : (
            <Button asChild size="sm">
              <Link to={composeHref(arc)}>
                <PenLine aria-hidden size={16} />
                Write your perspective
              </Link>
            </Button>
          )}
          {arc.place && (
            <ReportDialog
              targetKind={ReportTargetKind.PLACE}
              targetId={arc.place.id}
              targetLabel={arc.place.name}
              triggerLabel="Report place"
            />
          )}
        </div>
        {session && !rights.participant && (
          <p className="mt-2 text-sm text-ink-muted">
            Only people with a perspective in this arc can upvote or claim characters. Anyone signed in can react.
          </p>
        )}
      </header>

      <Section id="arc-canon" title="Canon">
        {res.canon && res.canon.perspectives.length > 0 ? (
          <>
            <p className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-ink-muted">
              <span className="inline-flex items-center gap-1 font-medium text-accent">
                <Crown aria-hidden size={16} />
                The day as it is remembered
              </span>
              <span>{loreLine(res.canon)}</span>
              {res.canon.lore && (
                <Link
                  to={`/lores/${res.canon.lore.id}`}
                  className="inline-flex min-h-11 items-center gap-1 font-medium text-accent underline underline-offset-4"
                >
                  Open the canon, cast and thread
                  <ArrowRight aria-hidden size={16} />
                </Link>
              )}
            </p>
            <CardList items={res.canon.perspectives} rights={rights.forLore(res.canon)} />
          </>
        ) : (
          <NoCanonYet leader={res.sublores[0]} />
        )}
      </Section>

      {!isHour && res.hours.length > 0 && <HourTimeline arcId={arc.id} first={res.hours} firstToken={res.hoursNextPageToken} />}

      <Sublores arcId={arc.id} res={res} />

      <PerspectiveList
        arcId={arc.id}
        section={ArcSection.TALES}
        id="arc-tales"
        title="Tales"
        intro="Perspectives that stand alone, outside any lore."
        moreLabel="Show more tales"
        first={res.tales}
        firstToken={res.talesNextPageToken}
        rights={rights.outsideLore}
      />

      <PerspectiveList
        arcId={arc.id}
        section={ArcSection.UNLINKED}
        id="arc-unlinked"
        title="Not yet linked"
        intro="Published within the last week and waiting for a claim to link them into a lore."
        moreLabel="Show more perspectives"
        first={res.unlinked}
        firstToken={res.unlinkedNextPageToken}
        rights={rights.outsideLore}
      />
    </article>
  );
}

/** Members a lore needs before it can be canon (CANON_MIN_MEMBERS). */
const CANON_MIN_MEMBERS = 4;

function NoCanonYet({ leader }: { leader: ArcLore | undefined }) {
  const members = leader?.lore?.memberCount ?? 0;
  return (
    <p className="mt-2 text-ink-muted">
      No canon yet. When people find each other in their stories, the linked stories form a lore, and a lore of{" "}
      {CANON_MIN_MEMBERS} or more that readers favour becomes the canon of the day.
      {leader &&
        ` The leading contender has ${members} of ${CANON_MIN_MEMBERS} members${members >= CANON_MIN_MEMBERS ? " and is waiting for the next ranking" : ""}.`}
    </p>
  );
}

/**
 * A contender: a lore that is not the canon, as a card that says who is in
 * it, how close it is to being eligible for canon, and opens the lore.
 */
function ContenderCard({ l, rank }: { l: ArcLore; rank: number }) {
  const lore = l.lore;
  if (!lore) return null;
  const pct = Math.min(100, Math.round((lore.memberCount / CANON_MIN_MEMBERS) * 100));
  return (
    <article aria-labelledby={`contender-${lore.id}`} className="min-w-0 rounded-xl border border-rule bg-paper p-4 shadow-sm">
      <header className="flex flex-wrap items-baseline justify-between gap-2">
        <h3 id={`contender-${lore.id}`} className="font-sans text-base font-medium">
          Contender {rank}
        </h3>
        <p className="text-sm text-ink-muted">{loreLine(l)}</p>
      </header>
      <p className="mt-2 flex min-w-0 flex-wrap gap-x-2 text-sm text-ink">
        {lore.members.map((m, i) => (
          <span key={m.perspectiveId} className="min-w-0">
            {m.user ? <AuthorLink username={m.user.username} /> : "Someone"}
            {i < lore.members.length - 1 ? "," : ""}
          </span>
        ))}
      </p>
      <div className="mt-3" aria-label={`${lore.memberCount} of ${CANON_MIN_MEMBERS} members needed for canon`}>
        <div className="h-1.5 w-full overflow-hidden rounded-full bg-surface">
          <div className="h-full rounded-full bg-accent" style={{ width: `${pct}%` }} />
        </div>
        <p className="mt-1 text-xs text-ink-muted">
          {lore.memberCount >= CANON_MIN_MEMBERS
            ? "Eligible for canon; the ranking decides"
            : `${lore.memberCount} of ${CANON_MIN_MEMBERS} members to be eligible for canon`}
        </p>
      </div>
      <Link
        to={`/lores/${lore.id}`}
        className="mt-3 inline-flex min-h-11 items-center gap-1 text-sm font-medium text-accent underline underline-offset-4"
      >
        Read this lore
        <ArrowRight aria-hidden size={16} />
      </Link>
    </article>
  );
}

/** Every lore but the canon that has a member, highest rank first, a page at a time. */
function Sublores({ arcId, res }: { arcId: string; res: GetArcResponse }) {
  const more = useMore(arcId, ArcSection.SUBLORES, res.subloresNextPageToken, (p) => p.sublores);
  const sublores = [...res.sublores, ...more.items];
  if (sublores.length === 0) return null;
  return (
    <Section id="arc-sublores" title="Contenders">
      <p className="mt-2 text-sm text-ink-muted">Other lores from this day. One takes the canon when readers favour it more.</p>
      <div className="mt-4 grid min-w-0 gap-4 sm:grid-cols-2">
        {sublores.map((l, i) => (
          <ContenderCard key={l.lore?.id ?? i} l={l} rank={i + 1} />
        ))}
      </div>
      <MoreButton label="Show more contenders" more={more} />
    </Section>
  );
}

/** Tales or perspectives not yet linked, oldest first, a page at a time. */
function PerspectiveList(props: {
  arcId: string;
  section: ArcSection;
  id: string;
  title: string;
  intro: string;
  moreLabel: string;
  first: PerspectiveSummary[];
  firstToken: string;
  rights: ViewerRights;
}) {
  const more = useMore(props.arcId, props.section, props.firstToken, (p) => p.perspectives);
  const items = [...props.first, ...more.items];
  if (items.length === 0) return null;
  return (
    <Section id={props.id} title={props.title}>
      <p className="mt-2 text-sm text-ink-muted">{props.intro}</p>
      <CardList items={items} rights={props.rights} />
      <MoreButton label={props.moreLabel} more={more} />
    </Section>
  );
}

function Missing() {
  return (
    <section aria-labelledby="arc-missing" className="mx-auto max-w-measure py-12">
      <h1 id="arc-missing" className="text-2xl sm:text-3xl">
        Arc not found
      </h1>
      <p className="mt-3 text-ink-muted">It may have been removed, or the link is wrong.</p>
      <p className="mt-6">
        <Link to="/" className="text-accent underline underline-offset-4">
          Find a place
        </Link>
      </p>
    </section>
  );
}

/**
 * The arc page. Reading order is fixed: canon first, then (on a day arc) the
 * hour timeline with each hour's canon, then every sublore highest rank first,
 * then tales, then perspectives not yet linked. A demoted lore is just another
 * sublore here, so nothing is hidden by losing canon.
 */
export function Component() {
  const { arcId = "" } = useParams();
  const query = useQuery({
    queryKey: arcKey(arcId),
    queryFn: () => clients.arc.getArc({ arcId }),
  });
  if (query.isPending) {
    return (
      <p role="status" className="text-ink-muted">
        Opening the arc…
      </p>
    );
  }
  if (query.isError) {
    if (ConnectError.from(query.error).code === Code.NotFound) return <Missing />;
    return (
      <section role="alert" className="mx-auto max-w-measure py-12">
        <h1 className="text-2xl sm:text-3xl">This arc could not be loaded</h1>
        <p className="mt-3 text-ink-muted">Check your connection and try again.</p>
        <Button className="mt-6" variant="secondary" onClick={() => void query.refetch()}>
          Try again
        </Button>
      </section>
    );
  }
  return <ArcView res={query.data} />;
}

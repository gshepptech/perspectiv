import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Code, ConnectError } from "@connectrpc/connect";
import { ArrowLeft, ArrowRight, Clock, Crown, Pin, PenLine } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { Link, useLocation, useParams } from "react-router";

import { UpvoteRule } from "@/components/actions/JoinFirst";
import type { ViewerRights } from "@/components/actions/PerspectiveActions";
import { arcQuery } from "@/components/actions/queries";
import { ReportDialog } from "@/components/actions/ReportDialog";
import { arcSpan, titleEndsWithHour, type ArcSpan } from "@/components/reader/format";
import { Button } from "@/components/ui/Button";
import type { Arc, ArcLore, GetArcResponse, HourArc, ListArcSectionResponse } from "@/gen/perspectiv/v1/arc_pb";
import type { Lore } from "@/gen/perspectiv/v1/lore_pb";
import { ArcSection, ReportTargetKind } from "@/gen/perspectiv/v1/enums_pb";
import type { PerspectiveSummary } from "@/gen/perspectiv/v1/perspective_pb";
import { clients } from "@/lib/connect";
import { cn } from "@/lib/cn";
import { useSession, type Session } from "@/lib/session";
import { useDocumentTitle } from "@/lib/title";
import { composeHref } from "@/routes/compose/links";

import { dateLabel, hourLabel } from "./labels";
import { OpenCharacters } from "./OpenCharacters";
import { StoryRow } from "./StoryRow";

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
        <Button variant="secondary" className="mt-4" pending={more.loading} onClick={more.showMore}>
          {label}
        </Button>
      )}
    </>
  );
}

function Section({ id, title, intro, children }: { id: string; title: string; intro?: string; children: ReactNode }) {
  return (
    <section aria-labelledby={id} className="min-w-0">
      <h2 id={id} className="text-xl">
        {title}
      </h2>
      {intro && <p className="mt-1 text-sm text-ink-muted">{intro}</p>}
      {children}
    </section>
  );
}

function Rows({
  items,
  rights,
  level = 3,
  showStanding = true,
}: {
  items: PerspectiveSummary[];
  rights?: ViewerRights;
  level?: 3 | 4;
  showStanding?: boolean;
}) {
  return (
    <ul className="mt-2 min-w-0 divide-y divide-rule">
      {items.map((p) => (
        <li key={p.id} className="min-w-0">
          <StoryRow summary={p} rights={rights} level={level} showStanding={showStanding} />
        </li>
      ))}
    </ul>
  );
}

function plural(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}

/** "knightsfan_04, paintedknight and 3 others": a lore is named by who is in it. */
function loreTitle(lore: Lore, stories: PerspectiveSummary[]): string {
  const names = (lore.members.length > 0 ? lore.members.map((m) => m.user?.username) : stories.map((p) => p.author?.username)).filter(
    (n): n is string => !!n,
  );
  if (names.length === 0) return "A lore";
  if (names.length <= 2) return names.join(" and ");
  const rest = names.length - 2;
  return `${names[0]}, ${names[1]} and ${plural(rest, "other", "others")}`;
}

/** Members a lore needs before it can be canon (CANON_MIN_MEMBERS). */
const CANON_MIN_MEMBERS = 4;

/**
 * One lore as a block: its flair, who is in it, its counts, its stories as
 * rows and the way into its own page. The canon's block is pinned first.
 */
function LoreBlock({ l, canon, rank, rights, span }: { l: ArcLore; canon: boolean; rank: number; rights: ViewerRights; span: ArcSpan }) {
  const lore = l.lore;
  if (!lore) return null;
  const titleId = `lore-${lore.id}`;
  const short = CANON_MIN_MEMBERS - lore.memberCount;
  return (
    <section
      aria-labelledby={titleId}
      className={cn("min-w-0 rounded-md border bg-paper px-3 pt-3 pb-2 sm:px-4", canon ? "border-accent" : "border-rule")}
    >
      <p className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
        {canon ? (
          <>
            <span className="inline-flex items-center gap-1 font-medium text-accent">
              <Crown aria-hidden size={16} />
              {span === "hour" ? "Canon of this hour" : "Canon of the day"}
            </span>
            <span className="inline-flex items-center gap-1 text-ink-muted">
              <Pin aria-hidden size={14} />
              Pinned
            </span>
          </>
        ) : (
          <>
            <span className="font-medium text-ink">Contender {rank}</span>
            <span className="text-ink-muted">
              {short > 0 ? `${plural(short, "more person", "more people")} to be eligible for canon` : "Eligible for canon; the ranking decides"}
            </span>
          </>
        )}
      </p>
      <h3 id={titleId} className="mt-1 text-xl break-words">
        {lore.name || loreTitle(lore, l.perspectives)}
      </h3>
      <p className="text-sm text-ink-muted">
        {lore.name && <>{loreTitle(lore, l.perspectives)} · </>}
        {plural(lore.memberCount, "story", "stories")} · {plural(lore.upvotes, "upvote", "upvotes")}
      </p>
      <Rows items={l.perspectives} rights={rights} level={4} showStanding={false} />
      <p className="border-t border-rule pt-1">
        <Link
          to={`/lores/${lore.id}`}
          className="inline-flex min-h-11 items-center gap-1 text-sm font-medium text-accent underline underline-offset-4"
        >
          {canon ? "Open the canon: who was who, full stories and thread" : "Open this lore: who was who, full stories and thread"}
          <ArrowRight aria-hidden size={16} />
        </Link>
      </p>
    </section>
  );
}

function NoCanonYet({ leader, span }: { leader: ArcLore | undefined; span: ArcSpan }) {
  const members = leader?.lore?.memberCount ?? 0;
  return (
    <section aria-labelledby="arc-canon" className="rounded-md border border-dashed border-rule px-4 py-3">
      <h2 id="arc-canon" className="inline-flex items-center gap-1 font-sans text-sm font-medium text-ink-muted">
        <Crown aria-hidden size={16} />
        Canon
      </h2>
      <p className="mt-1 text-ink-muted">
        No canon yet. When people find each other in their stories, the linked stories form a lore, and the lore of{" "}
        {CANON_MIN_MEMBERS} or more that the people who wrote about this {span} favour most becomes the canon of this {span}.
        {leader &&
          ` The leading contender has ${members} of ${CANON_MIN_MEMBERS} members${members >= CANON_MIN_MEMBERS ? " and is waiting for the next ranking" : ""}.`}
      </p>
    </section>
  );
}

/**
 * A day arc's hours in ascending hour order, each with its canon's stories
 * as read-only rows: voting and claiming in an hour depend on who wrote in
 * that hour's arc, so those actions live on the hour arc's own page.
 */
function HourTimeline({
  arcId,
  first,
  firstToken,
  hoursOnly,
}: {
  arcId: string;
  first: HourArc[];
  firstToken: string;
  /** The day has no stories of its own, only its hours'. */
  hoursOnly: boolean;
}) {
  const more = useMore(arcId, ArcSection.HOURS, firstToken, (p) => p.hours);
  const sorted = [...first, ...more.items].sort((a, b) => (a.arc?.hour ?? 0) - (b.arc?.hour ?? 0));
  return (
    <Section
      id="arc-hours"
      title="Hours"
      intro={
        hoursOnly
          ? "Everything written about this day happened within these hours. Each hour has its own stories and its own canon."
          : "This day, hour by hour. Each hour has its own stories and its own canon."
      }
    >
      <ol className="mt-3 grid min-w-0 gap-4 border-l-2 border-rule pl-4">
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
                <span>{plural(h.perspectiveCount, "story", "stories")}</span>
                <Link to={`/arcs/${a.id}`} className="font-medium text-accent underline underline-offset-4">
                  Open the {at} hour
                </Link>
              </p>
              {h.canon && h.canon.perspectives.length > 0 ? (
                <Rows items={h.canon.perspectives} level={4} showStanding={false} />
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

/** Every lore but the canon that has a member, highest rank first, a page at a time. */
function Contenders({ arcId, res, rights, span }: { arcId: string; res: GetArcResponse; rights: ReturnType<typeof rightsIn>; span: ArcSpan }) {
  const more = useMore(arcId, ArcSection.SUBLORES, res.subloresNextPageToken, (p) => p.sublores);
  const sublores = [...res.sublores, ...more.items];
  if (sublores.length === 0) return null;
  return (
    <Section
      id="arc-sublores"
      title="Contenders"
      intro={`Other groups of people who found each other in their stories. One takes the canon when the people who wrote about this ${span} favour it more.`}
    >
      <div className="mt-3 grid min-w-0 gap-4">
        {sublores.map((l, i) => (
          <LoreBlock key={l.lore?.id ?? i} l={l} canon={false} rank={i + 1} rights={rights.forLore(l)} span={span} />
        ))}
      </div>
      <MoreButton label="Show more contenders" more={more} />
    </Section>
  );
}

/** Stories in no lore: those still waiting for a link, then tales. */
function StandingAlone({ arcId, res, rights }: { arcId: string; res: GetArcResponse; rights: ViewerRights }) {
  const unlinked = useMore(arcId, ArcSection.UNLINKED, res.unlinkedNextPageToken, (p) => p.perspectives);
  const tales = useMore(arcId, ArcSection.TALES, res.talesNextPageToken, (p) => p.perspectives);
  const waiting = [...res.unlinked, ...unlinked.items];
  const alone = [...res.tales, ...tales.items];
  if (waiting.length === 0 && alone.length === 0) return null;
  return (
    <Section
      id="arc-alone"
      title="Standing alone"
      intro="Stories nobody has linked to. Waiting ones can still join a lore for a week after they are published; after that they stand alone as tales."
    >
      <Rows items={[...waiting, ...alone]} rights={rights} />
      <MoreButton label="Show more waiting stories" more={unlinked} />
      <MoreButton label="Show more tales" more={tales} />
    </Section>
  );
}

/** How many stories and lores the page has loaded, with "+" when a list has more. */
function counts(res: GetArcResponse): string {
  const stories =
    (res.canon?.perspectives.length ?? 0) +
    res.sublores.reduce((n, l) => n + l.perspectives.length, 0) +
    res.unlinked.length +
    res.tales.length;
  const more = res.subloresNextPageToken || res.unlinkedNextPageToken || res.talesNextPageToken ? "+" : "";
  const lores = (res.canon ? 1 : 0) + res.sublores.length;
  return `${stories}${more} ${stories === 1 && !more ? "story" : "stories"} · ${lores}${res.subloresNextPageToken ? "+" : ""} ${lores === 1 ? "lore" : "lores"}`;
}

/** Brings "How this day works" into view and moves focus to its heading. */
function showHowItWorks() {
  document.getElementById("arc-how")?.focus();
}

function HowItWorks({ arc, res, rights }: { arc: Arc; res: GetArcResponse; rights: ReturnType<typeof rightsIn> }) {
  const session = useSession();
  const span = arcSpan(arc.hour);
  return (
    <aside aria-labelledby="arc-how" className="min-w-0 rounded-md border border-rule bg-surface p-4 text-sm">
      <h2 id="arc-how" tabIndex={-1} className="text-lg">
        How this {span} works
      </h2>
      <ol className="mt-2 grid list-decimal gap-1.5 pl-5 text-ink">
        <li>Everyone who was here writes their own side.</li>
        <li>If someone else's story describes you, open it and claim that character.</li>
        <li>Stories linked by claims become a lore, with its own thread.</li>
        <li>
          The lore the people who wrote here favour most, with {CANON_MIN_MEMBERS} or more people, becomes the canon of this {span}.
          The others stay as contenders.
        </li>
      </ol>
      <p className="mt-3 text-ink-muted">{counts(res)}</p>
      {session && !rights.participant && (
        <p className="mt-2 text-ink-muted">
          Only people who wrote about this {span} can upvote, claim characters or post in a lore&apos;s thread. Anyone signed in can react.
        </p>
      )}
      {!session && (
        <p className="mt-2 text-ink-muted">
          Anyone can read. Only people who wrote about this {span} can upvote, claim characters or post in a lore&apos;s thread.
        </p>
      )}
      {arc.place && (
        <div className="mt-3">
          <ReportDialog targetKind={ReportTargetKind.PLACE} targetId={arc.place.id} targetLabel={arc.place.name} triggerLabel="Report place" />
        </div>
      )}
    </aside>
  );
}

/**
 * For a reader who may not vote here, said once above the stories instead
 * of on every row: who can, and the way in for someone who was there.
 */
function WhoCanUpvote({ arc }: { arc: Arc }) {
  return (
    <div role="note" className="min-w-0 rounded-md border border-rule bg-surface p-4 text-sm text-ink">
      <UpvoteRule where={{ placeId: arc.place?.id ?? "", localDate: arc.localDate, hour: arc.hour }} back={`/arcs/${arc.id}`} />
    </div>
  );
}

function ArcView({ res }: { res: GetArcResponse }) {
  const session = useSession();
  const arc = res.arc;
  if (!arc) return <Missing />;
  const rights = rightsIn(res, session);
  const isHour = arc.hour !== undefined;
  const span = arcSpan(arc.hour);
  // A day whose stories were all written into its hours: the hours are the
  // page, not an empty canon.
  const hoursOnly =
    !isHour && res.hours.length > 0 && !res.canon && res.sublores.length === 0 && res.tales.length === 0 && res.unlinked.length === 0;
  // Arriving from an invite: the reader has just written their side here.
  const location = useLocation();
  const invited = (location.state as { invited?: boolean } | null)?.invited === true;
  // A story's "How this day works" link lands on the explainer.
  useEffect(() => {
    if (location.hash === "#arc-how") showHowItWorks();
  }, [location.hash]);
  return (
    <article aria-labelledby="arc-title" className="min-w-0">
      {invited && (
        <p role="status" className="mb-6 rounded-xl border border-success p-4 text-ink">
          Your side is published. If you are someone in another story here, open it and use{" "}
          <strong className="font-medium">Claim a character</strong> so your stories link.
        </p>
      )}
      <header className="min-w-0">
        {/* Set as the place page's breadcrumb (Folio, Arc page). */}
        <nav aria-label="Place">
          <ol className="flex flex-wrap items-center gap-x-2 text-sm text-ink-muted">
            {arc.placeChain.map((p, i) => (
              <li key={p.id} className="inline-flex min-w-0 items-center gap-2">
                {i > 0 && <span aria-hidden>›</span>}
                <Link to={`/places/${p.id}`} className="inline-flex min-h-11 min-w-0 items-center wrap-anywhere text-accent underline underline-offset-4">
                  {p.name}
                </Link>
              </li>
            ))}
          </ol>
        </nav>
        <h1 id="arc-title" className="text-2xl break-words sm:text-3xl">
          {arc.title}
        </h1>
        <p className="mt-1 flex flex-wrap items-center gap-x-3 text-ink-muted">
          <span>{dateLabel(arc.localDate)}</span>
          {isHour && !titleEndsWithHour(arc.title, arc.hour) && (
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
              className="inline-flex min-h-11 items-center gap-1 text-sm font-medium text-accent underline underline-offset-4"
            >
              <ArrowLeft aria-hidden size={16} />
              The whole day
            </Link>
          </p>
        )}
        <div className="mt-4 flex flex-wrap items-center gap-2">
          {rights.mineId ? (
            <Button asChild variant="secondary" size="sm" className="min-h-11">
              <Link to={`/perspectives/${rights.mineId}`}>Read yours</Link>
            </Button>
          ) : (
            <Button asChild size="sm" className="min-h-11">
              <Link to={composeHref({ placeId: arc.place?.id ?? "", localDate: arc.localDate, hour: arc.hour })}>
                <PenLine aria-hidden size={16} />
                Write your story
              </Link>
            </Button>
          )}
        </div>
      </header>

      {/* Phones: the sidebar falls below every story, so the list and the gist of how the day works start here. */}
      <p className="mt-4 text-sm text-ink-muted lg:hidden">
        The canon is the lore of {CANON_MIN_MEMBERS} or more linked stories that the people who were here favour most.{" "}
        <a
          href="#arc-how"
          onClick={(e) => {
            e.preventDefault();
            showHowItWorks();
          }}
          className="inline-flex min-h-11 items-center font-medium text-accent underline underline-offset-4"
        >
          How this {span} works
        </a>
      </p>
      <div className="mt-4 lg:hidden">
        <OpenCharacters arcId={arc.id} compact />
      </div>
      <div className="mt-6 grid min-w-0 gap-8 lg:grid-cols-[minmax(0,1fr)_18rem]">
        <div className="grid min-w-0 content-start gap-8">
          {!rights.participant && !hoursOnly && <WhoCanUpvote arc={arc} />}
          {res.canon && res.canon.lore && res.canon.perspectives.length > 0 ? (
            <section aria-labelledby="arc-canon" className="min-w-0">
              <h2 id="arc-canon" className="sr-only">
                Canon
              </h2>
              <LoreBlock l={res.canon} canon rank={0} rights={rights.forLore(res.canon)} span={span} />
            </section>
          ) : (
            !hoursOnly && <NoCanonYet leader={res.sublores[0]} span={span} />
          )}
          {!isHour && res.hours.length > 0 && <HourTimeline arcId={arc.id} first={res.hours} firstToken={res.hoursNextPageToken} hoursOnly={hoursOnly} />}
          <Contenders arcId={arc.id} res={res} rights={rights} span={span} />
          <StandingAlone arcId={arc.id} res={res} rights={rights.outsideLore} />
        </div>
        <div className="grid min-w-0 content-start gap-4">
          <div className="hidden lg:block">
            <OpenCharacters arcId={arc.id} />
          </div>
          <HowItWorks arc={arc} res={res} rights={rights} />
        </div>
      </div>
    </article>
  );
}

function Missing() {
  return (
    <section aria-labelledby="arc-missing" className="mx-auto max-w-measure py-12">
      <h1 id="arc-missing" className="text-2xl sm:text-3xl">
        Day not found
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
 * The arc page, an index of the day like a subreddit's front page. Reading
 * order is fixed: the canon pinned first, then (on a day arc) the hour
 * timeline with each hour's canon, then every contender highest rank first,
 * then the stories in no lore. A demoted lore is just another contender here,
 * so nothing is hidden by losing canon. Stories are rows; reading, claiming
 * and reacting happen on a story's own page.
 */
export function Component() {
  const { arcId = "" } = useParams();
  const query = useQuery(arcQuery(arcId));
  useDocumentTitle(query.isError ? "Day not found" : query.data?.arc?.title);
  if (query.isPending) {
    return (
      <p role="status" className="text-ink-muted">
        Opening the day…
      </p>
    );
  }
  if (query.isError) {
    if (ConnectError.from(query.error).code === Code.NotFound) return <Missing />;
    return (
      <section role="alert" className="mx-auto max-w-measure py-12">
        <h1 className="text-2xl sm:text-3xl">This day could not be loaded</h1>
        <p className="mt-3 text-ink-muted">Check your connection and try again.</p>
        <Button className="mt-6" variant="secondary" onClick={() => void query.refetch()}>
          Try again
        </Button>
      </section>
    );
  }
  return <ArcView res={query.data} />;
}

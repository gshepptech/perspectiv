import { useQuery } from "@tanstack/react-query";
import { Code, ConnectError } from "@connectrpc/connect";
import { ArrowLeft, Clock, PenLine } from "lucide-react";
import { Fragment, type ReactNode } from "react";
import { Link, useParams } from "react-router";

import type { ViewerRights } from "@/components/actions/PerspectiveActions";
import { ReportDialog } from "@/components/actions/ReportDialog";
import { Button } from "@/components/ui/Button";
import type { Arc, ArcLore, GetArcResponse, HourArc } from "@/gen/perspectiv/v1/arc_pb";
import { ReportTargetKind } from "@/gen/perspectiv/v1/enums_pb";
import type { PerspectiveSummary } from "@/gen/perspectiv/v1/perspective_pb";
import { clients } from "@/lib/connect";
import { useSession, type Session } from "@/lib/session";

import { dateLabel, hourLabel } from "./labels";
import { PerspectiveCard } from "./PerspectiveCard";

export const arcKey = (arcId: string) => ["arc", "getArc", { arcId }] as const;

/** Every perspective filed directly under this arc, in any standing. */
function arcPerspectives(res: GetArcResponse): PerspectiveSummary[] {
  return [
    ...(res.canon?.perspectives ?? []),
    ...res.sublores.flatMap((l) => l.perspectives),
    ...res.tales,
    ...res.unlinked,
  ];
}

/**
 * The viewer's rights in this arc. A participant is anyone with a published
 * perspective here, in any standing: a REMOVED perspective is still published,
 * and the server counts it for votes and claims. A remove vote needs the
 * viewer to be a member of the target's own lore, and the lore 4 members or
 * more.
 */
function rightsIn(res: GetArcResponse, session: Session | null) {
  const mine = session ? arcPerspectives(res).find((p) => p.author?.userId === session.userId) : undefined;
  const participant = !!mine;
  const forLore = (lore: ArcLore | undefined): ViewerRights => ({
    participant,
    loreVoter:
      !!session &&
      !!lore?.lore &&
      lore.lore.memberCount >= 4 &&
      lore.lore.members.some((m) => m.user?.userId === session.userId),
  });
  return { mine, participant, forLore, outsideLore: { participant, loreVoter: false } satisfies ViewerRights };
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
function HourTimeline({ hours }: { hours: HourArc[] }) {
  const sorted = [...hours].sort((a, b) => (a.arc?.hour ?? 0) - (b.arc?.hour ?? 0));
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
    </Section>
  );
}

function ArcView({ res }: { res: GetArcResponse }) {
  const session = useSession();
  const arc = res.arc;
  if (!arc) return <Missing />;
  const rights = rightsIn(res, session);
  const isHour = arc.hour !== undefined;
  // A lore whose group vanished keeps its row with no perspectives; it has
  // nothing to read, so it is not listed. Every lore with a perspective is.
  const sublores = res.sublores.filter((l) => l.perspectives.length > 0);
  return (
    <article aria-labelledby="arc-title" className="min-w-0">
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
          {rights.mine ? (
            <Button asChild variant="secondary" size="sm">
              <Link to={`/perspectives/${rights.mine.id}`}>Read yours</Link>
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
            <p className="mt-2 text-sm text-ink-muted">{loreLine(res.canon)}</p>
            <CardList items={res.canon.perspectives} rights={rights.forLore(res.canon)} />
          </>
        ) : (
          <p className="mt-2 text-ink-muted">
            No canon yet. A lore of 4 or more linked perspectives becomes canon when it ranks highest.
          </p>
        )}
      </Section>

      {!isHour && res.hours.length > 0 && <HourTimeline hours={res.hours} />}

      {sublores.length > 0 && (
        <Section id="arc-sublores" title="Sublores">
          <ol className="mt-4 grid min-w-0 gap-6">
            {sublores.map((l, i) => (
              <li key={l.lore?.id ?? i} className="min-w-0">
                <h3 className="text-xl">
                  Sublore {i + 1} <span className="font-sans text-sm font-normal text-ink-muted">{loreLine(l)}</span>
                </h3>
                <CardList items={l.perspectives} rights={rights.forLore(l)} level={4} />
              </li>
            ))}
          </ol>
        </Section>
      )}

      {res.tales.length > 0 && (
        <Section id="arc-tales" title="Tales">
          <p className="mt-2 text-sm text-ink-muted">Perspectives that stand alone, outside any lore.</p>
          <CardList items={res.tales} rights={rights.outsideLore} />
        </Section>
      )}

      {res.unlinked.length > 0 && (
        <Section id="arc-unlinked" title="Not yet linked">
          <p className="mt-2 text-sm text-ink-muted">
            Published within the last week and waiting for a claim to link them into a lore.
          </p>
          <CardList items={res.unlinked} rights={rights.outsideLore} />
        </Section>
      )}
    </article>
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

import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { useQueries, useQuery } from "@tanstack/react-query";
import { ArrowLeft, BookOpen, BookOpenText, Crown, MessagesSquare, Swords } from "lucide-react";
import { Link, useParams } from "react-router";

import { PerspectiveActions, type ViewerRights } from "@/components/actions/PerspectiveActions";
import { ReportDialog } from "@/components/actions/ReportDialog";
import { perspectiveQuery } from "@/components/actions/queries";
import { AuthorLink } from "@/components/reader/AuthorLink";
import { arcSpan, formatDayBeside, genreLabel, type ArcSpan } from "@/components/reader/format";
import { Genre, LoreStanding, ReportTargetKind } from "@/gen/perspectiv/v1/enums_pb";
import type { Lore } from "@/gen/perspectiv/v1/lore_pb";
import { type Perspective, PerspectiveSummarySchema } from "@/gen/perspectiv/v1/perspective_pb";
import { clients } from "@/lib/connect";
import { useSession } from "@/lib/session";
import { useDocumentTitle } from "@/lib/title";

import { loreTabTitle, readableStories, UNNAMED } from "./loreText";
import { LoreThread } from "./LoreThread";
import { NameLore } from "./NameLore";
import { useThreadTarget } from "./useThreadTarget";
import { WhoWasWho } from "./WhoWasWho";

/** Members a lore needs before it can be canon (CANON_MIN_MEMBERS). */
const CANON_MIN_MEMBERS = 4;

function Missing() {
  return (
    <section className="mx-auto max-w-measure py-12" aria-labelledby="lore-missing">
      <h1 id="lore-missing" className="text-2xl sm:text-3xl">
        Lore not found
      </h1>
      <p className="mt-3 text-ink-muted">It may have been removed, or the link is wrong.</p>
    </section>
  );
}

function Story({ p, rights }: { p: Perspective; rights: ViewerRights }) {
  const parts = [...p.parts].sort((a, b) => a.position - b.position);
  const summary = create(PerspectiveSummarySchema, {
    id: p.id,
    arcId: p.arcId,
    author: p.author,
    genre: p.genre,
    loreStatus: p.loreStatus,
    loreId: p.loreId,
    upvotes: p.upvotes,
    deleted: p.deleted,
    hidden: p.hidden,
  });
  return (
    <article aria-labelledby={`story-${p.id}`} className="min-w-0 border-t border-rule pt-8 first:border-t-0 first:pt-0">
      <header className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
        <h3 id={`story-${p.id}`} className="min-w-0 text-xl wrap-break-word">
          {p.author ? <AuthorLink username={p.author.username} /> : "Someone"}
          {"'s story"}
        </h3>
        <p className="text-sm text-ink-muted">{p.genre !== Genre.UNSPECIFIED ? genreLabel(p.genre) : ""}</p>
      </header>
      <div className="prose-body mt-3">
        {parts.map((part) => (
          <p key={part.position}>{part.body}</p>
        ))}
      </div>
      <p className="mt-3">
        <Link to={`/perspectives/${p.id}`} className="inline-flex min-h-11 items-center gap-1 text-sm text-accent underline underline-offset-4">
          <BookOpen aria-hidden size={16} />
          Read it page by page
        </Link>
      </p>
      <PerspectiveActions summary={summary} rights={rights} />
    </article>
  );
}

function Standing({ lore, span }: { lore: Lore; span: ArcSpan }) {
  if (lore.standing === LoreStanding.CANON) {
    return (
      <span className="inline-flex items-center gap-1 text-sm font-medium text-accent">
        <Crown aria-hidden size={16} />
        {span === "hour" ? "Canon of this hour" : "Canon of the day"}
      </span>
    );
  }
  const short = CANON_MIN_MEMBERS - lore.memberCount;
  return (
    <span className="inline-flex items-start gap-1 text-sm text-ink-muted">
      <Swords aria-hidden size={16} className="mt-0.5 shrink-0" />
      Contender{short > 0 ? ` · ${short} more ${short === 1 ? "member" : "members"} to be eligible for canon` : ""}
    </span>
  );
}

/**
 * One lore in full: its standing, who turned out to be who, every story in
 * it in the order they were written, and the thread its people talk in.
 * Opened from the canon or a contender on the arc page.
 */
export function Component() {
  const { loreId = "" } = useParams();
  const session = useSession();
  const loreQuery = useQuery({
    queryKey: ["lore", "getLore", { loreId }],
    queryFn: ({ signal }) => clients.lore.getLore({ loreId }, { signal }),
    retry: false,
  });
  const lore = loreQuery.data?.lore;
  const arcId = lore?.arcId ?? "";
  // The viewer's standing in the arc decides what they may do on each story.
  const arc = useQuery({
    queryKey: ["arc", "getArc", { arcId, pageSize: 1 }],
    queryFn: ({ signal }) => clients.arc.getArc({ arcId, pageSize: 1 }, { signal }),
    enabled: arcId !== "",
  });
  const stories = useQueries({
    queries: (lore?.members ?? []).map((m) => perspectiveQuery(m.perspectiveId)),
  });
  const titleDay = arc.data?.arc?.title || stories.find((q) => q.data)?.data?.arcTitle || "";
  useDocumentTitle(loreQuery.isError ? "Lore not found" : lore ? loreTabTitle(lore.name, titleDay) : undefined);
  // #thread and #post-<id> land once everything above the thread has rendered.
  const target = useThreadTarget(lore?.id ?? "", !!lore && !arc.isPending && stories.every((q) => !q.isPending));

  if (loreQuery.isPending) {
    return (
      <p role="status" className="text-ink-muted">
        Opening the lore…
      </p>
    );
  }
  if (loreQuery.isError) {
    if (ConnectError.from(loreQuery.error).code === Code.NotFound) return <Missing />;
    return (
      <p role="alert" className="text-error">
        This lore could not be loaded. Try again in a moment.
      </p>
    );
  }
  if (!lore) return <Missing />;

  const loaded = readableStories(stories);
  const first = loaded[0];
  const viewer = arc.data?.viewer;
  const participant = !!session && !!viewer?.perspectiveId;
  const rights: ViewerRights = {
    participant,
    loreVoter: !!session && viewer?.loreId === lore.id && lore.memberCount >= CANON_MIN_MEMBERS,
  };
  const memberIds = new Set(lore.members.map((m) => m.user?.userId ?? ""));
  const dayTitle = arc.data?.arc?.title || first?.arcTitle || "";
  const title = lore.name || UNNAMED;
  const member = !!session && viewer?.loreId === lore.id;
  const span = arcSpan(arc.data?.arc ? arc.data.arc.hour : first?.hour);

  return (
    <article aria-labelledby="lore-title" className="mx-auto min-w-0 max-w-3xl">
      <p>
        <Link to={`/arcs/${arcId}`} className="inline-flex min-h-11 items-center gap-1 text-sm text-accent underline underline-offset-4">
          <ArrowLeft aria-hidden size={16} />
          The whole day
        </Link>
      </p>
      <header className="mt-2">
        <Standing lore={lore} span={span} />
        <h1 id="lore-title" className="mt-3 text-3xl leading-tight wrap-anywhere sm:text-4xl">
          {title}
        </h1>
        <p className="mt-2 text-ink-muted wrap-anywhere">
          {[dayTitle, first?.place?.name, first ? formatDayBeside(dayTitle, first.localDate, first.hour) : ""].filter(Boolean).join(" · ")}
        </p>
        {lore.name && lore.namedBy && (
          <p className="mt-1 text-sm text-ink-muted">
            Named by <AuthorLink username={lore.namedBy.username} />
          </p>
        )}
        <p className="mt-1 text-sm text-ink-muted">
          {lore.memberCount} {lore.memberCount === 1 ? "member" : "members"} · {lore.upvotes}{" "}
          {lore.upvotes === 1 ? "upvote" : "upvotes"}
        </p>
        <p className="mt-1 flex flex-wrap gap-x-6">
          <Link
            to={`/lores/${encodeURIComponent(lore.id)}/book`}
            className="inline-flex min-h-11 items-center gap-1 text-accent underline underline-offset-4"
          >
            <BookOpenText aria-hidden size={16} />
            Read as a book
          </Link>
          <Link to="#thread" className="inline-flex min-h-11 items-center gap-1 text-accent underline underline-offset-4">
            <MessagesSquare aria-hidden size={16} />
            Thread · {lore.postCount === 0 ? "no posts yet" : `${lore.postCount} ${lore.postCount === 1 ? "post" : "posts"}`}
          </Link>
        </p>
        {member && (
          <div className="mt-3">
            <NameLore loreId={lore.id} current={lore.name} />
          </div>
        )}
        {/* Members fix a bad name by renaming; anyone else can report it. */}
        {!member && lore.name && (
          <div className="mt-2">
            <ReportDialog
              targetKind={ReportTargetKind.LORE_NAME}
              targetId={lore.id}
              targetLabel={`the lore name “${lore.name}”`}
              triggerLabel="Report this name"
              flush
            />
          </div>
        )}
      </header>

      <WhoWasWho stories={loaded} memberIds={memberIds} />

      <section aria-labelledby="lore-stories" className="mt-8">
        <h2 id="lore-stories" className="text-xl">
          The stories
        </h2>
        <div className="mt-6 grid gap-10">
          {loaded.map((p) => (
            <Story key={p.id} p={p} rights={rights} />
          ))}
        </div>
      </section>

      <LoreThread loreId={lore.id} canPost={participant} span={span} missedPost={target.missed} />
    </article>
  );
}

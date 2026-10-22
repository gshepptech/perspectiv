import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { useQueries, useQuery } from "@tanstack/react-query";
import { ArrowLeft, BookOpen, Crown } from "lucide-react";
import { Link, useParams } from "react-router";

import { PerspectiveActions, type ViewerRights } from "@/components/actions/PerspectiveActions";
import { ReportDialog } from "@/components/actions/ReportDialog";
import { perspectiveQuery } from "@/components/actions/queries";
import { AuthorLink } from "@/components/reader/AuthorLink";
import { formatDay, genreLabel } from "@/components/reader/format";
import { Genre, LoreStanding, ReportTargetKind } from "@/gen/perspectiv/v1/enums_pb";
import type { Lore } from "@/gen/perspectiv/v1/lore_pb";
import { type Perspective, PerspectiveSummarySchema } from "@/gen/perspectiv/v1/perspective_pb";
import { clients } from "@/lib/connect";
import { useSession } from "@/lib/session";

import { LoreThread } from "./LoreThread";
import { NameLore } from "./NameLore";

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

/** "otter was the guy in the yellow raincoat in wren's story", for every link inside the lore. */
function Cast({ stories, memberIds }: { stories: Perspective[]; memberIds: Set<string> }) {
  const lines = stories.flatMap((p) =>
    p.characters
      .filter((c) => c.claimedBy && memberIds.has(c.claimedBy.userId))
      .map((c) => ({ id: c.id, who: c.claimedBy?.username ?? "", label: c.label, author: p.author?.username ?? "" })),
  );
  if (lines.length === 0) return null;
  return (
    <section aria-labelledby="lore-cast" className="mt-8">
      <h2 id="lore-cast" className="text-xl">
        Who was who
      </h2>
      <ul className="mt-3 grid gap-2">
        {lines.map((l) => (
          <li key={l.id} className="min-w-0 wrap-anywhere text-ink">
            <AuthorLink username={l.who} /> was <span className="font-serif">“{l.label}”</span> in{" "}
            <AuthorLink username={l.author} />’s story
          </li>
        ))}
      </ul>
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
    <article aria-labelledby={`story-${p.id}`} className="min-w-0 rounded-xl border border-rule bg-paper p-5 shadow-sm">
      <header className="flex flex-wrap items-baseline justify-between gap-2">
        <h3 id={`story-${p.id}`} className="font-sans text-base font-medium">
          {p.author ? <AuthorLink username={p.author.username} /> : "Someone"}
          {"'s perspective"}
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

function Standing({ lore }: { lore: Lore }) {
  if (lore.standing === LoreStanding.CANON) {
    return (
      <span className="inline-flex items-center gap-1 rounded-full bg-accent/10 px-3 py-1 text-sm font-medium text-accent">
        <Crown aria-hidden size={16} />
        Canon of the day
      </span>
    );
  }
  const short = CANON_MIN_MEMBERS - lore.memberCount;
  return (
    <span className="inline-flex items-center gap-1 rounded-full border border-rule px-3 py-1 text-sm text-ink-muted">
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

  const loaded = stories.map((s) => s.data).filter((p): p is Perspective => !!p && !p.deleted && !p.hidden);
  const first = loaded[0];
  const viewer = arc.data?.viewer;
  const participant = !!session && !!viewer?.perspectiveId;
  const rights: ViewerRights = {
    participant,
    loreVoter: !!session && viewer?.loreId === lore.id && lore.memberCount >= CANON_MIN_MEMBERS,
  };
  const memberIds = new Set(lore.members.map((m) => m.user?.userId ?? ""));
  const dayTitle = arc.data?.arc?.title || first?.arcTitle || "";
  const title = lore.name || dayTitle || "A lore";
  const member = !!session && viewer?.loreId === lore.id;

  return (
    <article aria-labelledby="lore-title" className="mx-auto min-w-0 max-w-3xl">
      <p>
        <Link to={`/arcs/${arcId}`} className="inline-flex min-h-11 items-center gap-1 text-sm text-accent underline underline-offset-4">
          <ArrowLeft aria-hidden size={16} />
          The whole day
        </Link>
      </p>
      <header className="mt-2">
        <Standing lore={lore} />
        <h1 id="lore-title" className="mt-3 text-3xl leading-tight wrap-anywhere sm:text-4xl">
          {title}
        </h1>
        <p className="mt-2 text-ink-muted">
          {[lore.name ? dayTitle : "", first?.place?.name, first ? formatDay(first.localDate, first.hour) : ""].filter(Boolean).join(" · ")}
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
            />
          </div>
        )}
      </header>

      <Cast stories={loaded} memberIds={memberIds} />

      <section aria-labelledby="lore-stories" className="mt-8">
        <h2 id="lore-stories" className="text-xl">
          The stories
        </h2>
        <div className="mt-4 grid gap-6">
          {loaded.map((p) => (
            <Story key={p.id} p={p} rights={rights} />
          ))}
        </div>
      </section>

      <LoreThread loreId={lore.id} canPost={participant} />
    </article>
  );
}

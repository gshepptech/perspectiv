import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { Link, useLocation, useNavigate, useParams, useSearchParams } from "react-router";

import { InviteDialog } from "@/components/actions/InviteDialog";
import { PerspectiveActions } from "@/components/actions/PerspectiveActions";
import { useStoryRights } from "@/components/actions/rights";
import { OpenCharacters } from "@/routes/arc/OpenCharacters";
import { arcQuery, perspectiveQuery } from "@/components/actions/queries";
import { arcSpan, formatDayBeside, genreLabel, storyStanding, storyStandingNote } from "@/components/reader/format";
import { Cast } from "@/components/reader/Cast";
import { Reader, useWholeStory } from "@/components/reader/Reader";
import { Button } from "@/components/ui/Button";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogTitle, DialogTrigger } from "@/components/ui/Dialog";
import type { GetArcResponse } from "@/gen/perspectiv/v1/arc_pb";
import { Genre, LoreStatus, PerspectiveStatus } from "@/gen/perspectiv/v1/enums_pb";
import { type Perspective, PerspectiveSummarySchema } from "@/gen/perspectiv/v1/perspective_pb";
import { clients } from "@/lib/connect";
import { useSession } from "@/lib/session";
import { useDocumentTitle } from "@/lib/title";
import { AuthorLink } from "@/components/reader/AuthorLink";
import { takeAfterPublish } from "@/routes/compose/links";

import { MoreFromDay } from "./MoreFromDay";

/**
 * A deleted or hidden perspective. Its heading takes focus, so an author who
 * just deleted their story from this page is not left on a vanished button.
 */
function Gone({ p }: { p: Perspective }) {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  return (
    <div className="min-w-0">
      <BackLink p={p} />
      <section className="mx-auto max-w-measure border-y border-rule py-8" aria-labelledby="read-gone">
        <h1 id="read-gone" ref={heading} tabIndex={-1} className="text-2xl wrap-anywhere focus:outline-none sm:text-3xl">
          {p.arcTitle || "A perspective"}
        </h1>
        <p className="mt-3 text-ink-muted">
          {p.hidden ? "This perspective was hidden by a moderator." : "This perspective was deleted by its author."}
        </p>
      </section>
    </div>
  );
}

/**
 * The part in the address, clamped to the perspective: a missing, malformed
 * or zero part opens part 1, and a part past the end opens the last one.
 */
function partFrom(value: string | null, total: number): number {
  const n = Number.parseInt(value ?? "", 10);
  if (!Number.isFinite(n) || n < 1) return 1;
  return Math.min(n, Math.max(total, 1));
}

/** Whether the story's lore is its arc's canon; false until the arc is read. */
function isCanon(p: Perspective, arc: GetArcResponse | undefined): boolean {
  return p.loreId !== "" && arc?.arc?.canonLoreId === p.loreId;
}

function Byline({ p, arc }: { p: Perspective; arc: GetArcResponse | undefined }) {
  const genre = p.genre !== Genre.UNSPECIFIED ? genreLabel(p.genre) : "";
  const standing = storyStanding(p.loreStatus, isCanon(p, arc), arcSpan(p.hour));
  // A story in a lore links to it: the lore page holds the other stories it
  // is linked with and the thread.
  const inLore = p.loreStatus === LoreStatus.IN_LORE && p.loreId !== "";
  return (
    // Every link sits in a 44px row so neighbouring links never crowd on a phone.
    <p className="flex flex-wrap items-center gap-x-3">
      {p.author && (
        <span className="inline-flex min-h-11 items-center gap-1">
          by <AuthorLink username={p.author.username} className="inline-flex min-h-11 items-center" />
        </span>
      )}
      {p.place && (
        <Link to={`/places/${p.place.id}`} className="inline-flex min-h-11 items-center wrap-anywhere text-accent underline underline-offset-4">
          {p.place.name}
        </Link>
      )}
      <span>{formatDayBeside(p.arcTitle || p.titleIfNewArc, p.localDate, p.hour)}</span>
      {genre && <span>{genre}</span>}
      {inLore ? (
        <Link to={`/lores/${p.loreId}`} className="inline-flex min-h-11 items-center text-accent underline underline-offset-4">
          {standing}: read the lore
        </Link>
      ) : (
        standing && <span>{standing}</span>
      )}
    </p>
  );
}

/** What the byline's standing means, in one line, with the way to the arc page's explainer. */
function StandingNote({ p, arc }: { p: Perspective; arc: GetArcResponse | undefined }) {
  const span = arcSpan(p.hour);
  const note = storyStandingNote(p.loreStatus, isCanon(p, arc), span);
  if (!note) return null;
  return (
    <p className="mt-1 text-sm text-ink-muted">
      {note}
      {p.arcId && (
        <>
          {" "}
          <Link to={`/arcs/${encodeURIComponent(p.arcId)}#arc-how`} className="inline-flex min-h-11 items-center text-accent underline underline-offset-4">
            How this {span} works
          </Link>
        </>
      )}
    </p>
  );
}

/** What the story page is handed in history state. */
type ReadState = { justPublished?: boolean; publishedId?: string } | null;

/**
 * Right after publishing from a story the writer wanted to claim a character
 * in ("write your side first"), they go back to that story with the claim
 * dialog up, carrying the new story's id so that page still says it was
 * published. Where they stand in the arc has just changed, so every cached
 * arc read is dropped first and the story reads it fresh.
 */
function ReturnAfterPublish({ p }: { p: Perspective }) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  useEffect(() => {
    const to = takeAfterPublish({ placeId: p.place?.id ?? "", localDate: p.localDate, hour: p.hour });
    if (!to) return;
    queryClient.removeQueries({ queryKey: ["arc"] });
    void navigate(to, { replace: true, state: { publishedId: p.id } satisfies ReadState });
  }, [p, navigate, queryClient]);
  return null;
}

function BackLink({ p }: { p: Perspective }) {
  if (!p.arcId) return null;
  return (
    <p className="mx-auto mb-4 max-w-measure">
      <Link to={`/arcs/${p.arcId}`} className="inline-flex min-h-11 items-center gap-1 text-sm text-accent underline underline-offset-4">
        <ArrowLeft aria-hidden size={16} />
        Back to the arc
      </Link>
    </p>
  );
}

function Missing({ title, detail }: { title: string; detail: string }) {
  return (
    <section className="mx-auto max-w-measure py-12" aria-labelledby="read-missing">
      <h1 id="read-missing" className="text-2xl sm:text-3xl">
        {title}
      </h1>
      <p className="mt-3 text-ink-muted">{detail}</p>
      <p className="mt-6">
        <Link to="/" className="text-accent underline underline-offset-4">
          Search for a place
        </Link>
      </p>
    </section>
  );
}

/**
 * The author's edit link and delete action. Editing opens the composer, which
 * keeps published text editable and locks characters with accepted claims.
 * Deleting a published perspective leaves the removed placeholder in its
 * place; deleting a draft returns the author to the landing page, where their
 * remaining drafts are listed.
 */
function AuthorActions({ p }: { p: Perspective }) {
  const isDraft = p.status === PerspectiveStatus.DRAFT;
  // "Invite who else was there" on the requests page links here with ?invite=1.
  const [params] = useSearchParams();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const remove = useMutation({
    mutationFn: () => clients.perspective.delete({ perspectiveId: p.id }),
    onSuccess: async () => {
      setOpen(false);
      await queryClient.invalidateQueries({ queryKey: ["perspective"] });
      if (isDraft) await navigate("/");
    },
  });
  const noun = isDraft ? "draft" : "perspective";
  return (
    <div className="flex flex-wrap items-center gap-3">
      {!isDraft && <InviteDialog p={p} openOnArrival={params.get("invite") === "1"} />}
      <Link to={`/compose?perspectiveId=${encodeURIComponent(p.id)}`} className="inline-flex min-h-11 items-center text-accent underline underline-offset-4">
        {isDraft ? "Edit draft" : "Edit"}
      </Link>
      <Dialog
        open={open}
        onOpenChange={(next) => {
          setOpen(next);
          if (!next) remove.reset();
        }}
      >
        <DialogTrigger asChild>
          <Button variant="danger" size="sm">
            Delete
          </Button>
        </DialogTrigger>
        <DialogContent>
          <DialogTitle>Delete this {noun}?</DialogTitle>
          <DialogDescription>
            {isDraft
              ? "The draft is gone for good."
              : "Readers see a removed placeholder in its place, and any character claims it is part of are dissolved."}
          </DialogDescription>
          {remove.isError && (
            <p role="alert" className="mt-3 text-sm text-error">
              It could not be deleted. Try again in a moment.
            </p>
          )}
          <div className="mt-6 flex flex-wrap justify-end gap-3">
            <DialogClose asChild>
              <Button variant="secondary">Cancel</Button>
            </DialogClose>
            <Button variant="danger" pending={remove.isPending} onClick={() => remove.mutate()}>
              Delete {noun}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}

/**
 * Reads one perspective a part at a time. The current part is the `part`
 * search parameter, and each page change pushes a history entry, so a page
 * is linkable and the back button returns to the previous part.
 */
export function Component() {
  const { perspectiveId = "" } = useParams();
  const session = useSession();
  // The composer arrives here with justPublished set, which shows the
  // post-publish prompt once: right after publishing is when the author knows
  // who was there. It points at the arc and at the invite button, and opens
  // nothing over the story.
  const state = useLocation().state as ReadState;
  const justPublished = state?.justPublished === true;
  // Sent back here from publishing (ReturnAfterPublish): the writer's own story.
  const publishedId = typeof state?.publishedId === "string" ? state.publishedId : "";
  const [params, setParams] = useSearchParams();
  // The action bar on the arc page reads the same cache entry, so both must
  // store the same shape; perspectiveQuery owns it.
  const query = useQuery(perspectiveQuery(perspectiveId));
  const loaded = query.data;
  const arcId = loaded && loaded.status !== PerspectiveStatus.DRAFT && !loaded.deleted && !loaded.hidden ? loaded.arcId : "";
  // The arc page's own read: what follows the story, whether its lore is the
  // canon, and which story each tagged person wrote that day.
  const arc = useQuery({ ...arcQuery(arcId), enabled: arcId !== "" });
  const bodies = (loaded?.parts ?? []).toSorted((a, b) => a.position - b.position).map((part) => part.body);
  const reading = useWholeStory(bodies);
  const storyTitle = loaded ? loaded.arcTitle || loaded.titleIfNewArc || "Untitled" : "";
  useDocumentTitle(
    query.isError
      ? "Perspective not found"
      : loaded && loaded.author && !loaded.deleted && !loaded.hidden
        ? `${storyTitle} by ${loaded.author.username}`
        : storyTitle,
  );
  const onPartChange = useCallback(
    (n: number) =>
      setParams((prev) => {
        const next = new URLSearchParams(prev);
        next.set("part", String(n));
        return next;
      }),
    [setParams],
  );

  if (query.isPending) {
    return (
      <p role="status" className="mx-auto max-w-measure text-ink-muted">
        Loading…
      </p>
    );
  }
  if (query.isError) {
    if (ConnectError.from(query.error).code === Code.NotFound) {
      return <Missing title="Perspective not found" detail="It may never have existed, or it is a draft that is not yours." />;
    }
    return (
      <p role="alert" className="mx-auto max-w-measure text-error">
        This perspective could not be loaded. Try again in a moment.
      </p>
    );
  }
  const p = query.data;
  if (!p) return <Missing title="Perspective not found" detail="It may have been removed." />;

  if (p.deleted || p.hidden) return <Gone p={p} />;

  const isDraft = p.status === PerspectiveStatus.DRAFT;
  const mine = session !== null && p.author?.userId === session.userId;
  const part = partFrom(params.get("part"), bodies.length);
  // The story's end: the last part, or every part on one page.
  const atEnd = reading.whole || part >= bodies.length;

  return (
    <div className="min-w-0">
      <BackLink p={p} />
      {isDraft && (
        <div className="mx-auto mb-4 flex max-w-measure flex-wrap items-center gap-3 rounded-md border border-warning px-3 py-1 text-sm">
          <span className="font-medium text-warning">Draft</span>
          <span className="text-ink-muted">Only you can see this.</span>
          <AuthorActions p={p} />
        </div>
      )}
      {justPublished && mine && !isDraft && <ReturnAfterPublish p={p} />}
      {justPublished && mine && !isDraft && p.arcId && (
        <p role="status" className="mx-auto mb-4 max-w-measure rounded-xl border border-success p-4 text-ink">
          Published. Others were there too?{" "}
          <Link to={`/arcs/${p.arcId}`} className="font-medium text-accent underline underline-offset-4">
            Open the day
          </Link>{" "}
          and find yourself in their stories: claim the character that was you, and your stories link. Or invite who was there with the button below.
        </p>
      )}

      {publishedId && publishedId !== p.id && (
        <p role="status" className="mx-auto mb-4 max-w-measure rounded-xl border border-success p-4 text-ink">
          Your story is published.{" "}
          <Link to={`/perspectives/${encodeURIComponent(publishedId)}`} className="font-medium text-accent underline underline-offset-4">
            Open your story
          </Link>
        </p>
      )}

      {!isDraft && mine && (
        <div className="mx-auto mb-4 max-w-measure text-sm">
          <AuthorActions p={p} />
        </div>
      )}
      <Reader
        title={storyTitle}
        byline={
          <>
            <Byline p={p} arc={arc.data} />
            <StandingNote p={p} arc={arc.data} />
            <Cast characters={p.characters} className="mt-3" />
          </>
        }
        parts={bodies}
        part={part}
        onPartChange={onPartChange}
        whole={reading.whole}
        onWholeChange={reading.short ? undefined : reading.choose}
      />
      {/* Keyed by story, so a "That was me" arrival is read afresh on each one. */}
      {!isDraft && !mine && <ReaderActions key={p.id} p={p} />}
      {!isDraft && p.arcId && atEnd && <MoreFromDay p={p} res={arc.data} />}
      {/* The moment after publishing is when a writer knows who was there;
          the list follows the story so its heading comes after the title. */}
      {justPublished && mine && !isDraft && p.arcId && (
        <div className="mx-auto mt-6 max-w-measure">
          <OpenCharacters arcId={p.arcId} />
        </div>
      )}
    </div>
  );
}

/**
 * What a reader can do with someone else's story once they have read it:
 * upvote, react, claim the character that was them, vote it out of their
 * lore, or report it.
 */
function ReaderActions({ p }: { p: Perspective }) {
  const rights = useStoryRights(p.arcId, p.loreId);
  const location = useLocation();
  // "That was me" links here with ?claim=<characterId> (or the older bare
  // ?claim=1). It is read once and then dropped from the address, so a
  // reload does not open the claim dialog again.
  const [params, setParams] = useSearchParams();
  const claimParam = params.get("claim");
  const [arrival, setArrival] = useState<string | null>(claimParam);
  useEffect(() => {
    if (claimParam === null) return;
    setArrival(claimParam);
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.delete("claim");
        return next;
      },
      // Keeps "Your story is published." when the writer was sent back here.
      { replace: true, state: location.state as ReadState },
    );
  }, [claimParam, setParams, location.state]);
  const claimCharacterId = arrival && arrival !== "1" ? arrival : undefined;
  const state = location.state as ReadState;
  // Sent back here from publishing (ReturnAfterPublish): the claim dialog says so too.
  const justPublished = typeof state?.publishedId === "string" && state.publishedId !== "" && state.publishedId !== p.id;
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
    <section aria-label="Respond to this story" className="mx-auto mt-6 max-w-measure">
      <PerspectiveActions
        summary={summary}
        rights={rights}
        withCast={false}
        claimOnArrival={arrival !== null}
        claimCharacterId={claimCharacterId}
        justPublished={justPublished}
      />
    </section>
  );
}

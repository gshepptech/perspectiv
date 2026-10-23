import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft } from "lucide-react";
import { useCallback, useState } from "react";
import { Link, useLocation, useNavigate, useParams, useSearchParams } from "react-router";

import { InviteDialog } from "@/components/actions/InviteDialog";
import { PerspectiveActions } from "@/components/actions/PerspectiveActions";
import { useStoryRights } from "@/components/actions/rights";
import { OpenCharacters } from "@/routes/arc/OpenCharacters";
import { perspectiveQuery } from "@/components/actions/queries";
import { formatDay, genreLabel, loreStatusLabel } from "@/components/reader/format";
import { Cast } from "@/components/reader/Cast";
import { Reader } from "@/components/reader/Reader";
import { Button } from "@/components/ui/Button";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogTitle, DialogTrigger } from "@/components/ui/Dialog";
import { Genre, LoreStatus, PerspectiveStatus } from "@/gen/perspectiv/v1/enums_pb";
import { type Perspective, PerspectiveSummarySchema } from "@/gen/perspectiv/v1/perspective_pb";
import { clients } from "@/lib/connect";
import { useSession } from "@/lib/session";
import { AuthorLink } from "@/components/reader/AuthorLink";

/**
 * The part in the address, clamped to the perspective: a missing, malformed
 * or zero part opens part 1, and a part past the end opens the last one.
 */
function partFrom(value: string | null, total: number): number {
  const n = Number.parseInt(value ?? "", 10);
  if (!Number.isFinite(n) || n < 1) return 1;
  return Math.min(n, Math.max(total, 1));
}

function Byline({ p }: { p: Perspective }) {
  const genre = p.genre !== Genre.UNSPECIFIED ? genreLabel(p.genre) : "";
  const standing = loreStatusLabel(p.loreStatus);
  // A story in a lore links to it: the lore page holds the other stories it
  // is linked with and the thread.
  const inLore = p.loreStatus === LoreStatus.IN_LORE && p.loreId !== "";
  return (
    <p className="flex flex-wrap gap-x-3 gap-y-1">
      {p.author && (
        <span>
          by <AuthorLink username={p.author.username} />
        </span>
      )}
      {p.place && (
        <Link to={`/places/${p.place.id}`} className="wrap-anywhere text-accent underline underline-offset-4">
          {p.place.name}
        </Link>
      )}
      <span>{formatDay(p.localDate, p.hour)}</span>
      {genre && <span>{genre}</span>}
      {inLore ? (
        <Link to={`/lores/${p.loreId}`} className="text-accent underline underline-offset-4">
          {standing}: read the lore
        </Link>
      ) : (
        standing && <span>{standing}</span>
      )}
    </p>
  );
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
      {!isDraft && <InviteDialog p={p} />}
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
            <Button variant="danger" disabled={remove.isPending} onClick={() => remove.mutate()}>
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
  const justPublished = (useLocation().state as { justPublished?: boolean } | null)?.justPublished === true;
  const [params, setParams] = useSearchParams();
  // The action bar on the arc page reads the same cache entry, so both must
  // store the same shape; perspectiveQuery owns it.
  const query = useQuery(perspectiveQuery(perspectiveId));
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

  if (p.deleted || p.hidden) {
    return (
      <div className="min-w-0">
        <BackLink p={p} />
        <section className="mx-auto max-w-measure border-y border-rule py-8" aria-labelledby="read-gone">
          <h1 id="read-gone" className="text-2xl wrap-anywhere sm:text-3xl">
            {p.arcTitle || "A perspective"}
          </h1>
          <p className="mt-3 text-ink-muted">
            {p.hidden ? "This perspective was hidden by a moderator." : "This perspective was deleted by its author."}
          </p>
        </section>
      </div>
    );
  }

  const isDraft = p.status === PerspectiveStatus.DRAFT;
  const mine = session !== null && p.author?.userId === session.userId;
  const bodies = p.parts.toSorted((a, b) => a.position - b.position).map((part) => part.body);
  const title = p.arcTitle || p.titleIfNewArc || "Untitled";

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
      {justPublished && mine && !isDraft && p.arcId && (
        <p role="status" className="mx-auto mb-4 max-w-measure rounded-xl border border-success p-4 text-ink">
          Published. Others were there too?{" "}
          <Link to={`/arcs/${p.arcId}`} className="font-medium text-accent underline underline-offset-4">
            Open the day
          </Link>{" "}
          and find yourself in their stories: claim the character that was you, and your stories link. Or invite who was there with the button below.
        </p>
      )}
      {/* The moment after publishing is when a writer knows who was there. */}
      {justPublished && mine && !isDraft && p.arcId && (
        <div className="mx-auto mb-6 max-w-measure">
          <OpenCharacters arcId={p.arcId} />
        </div>
      )}
      {!isDraft && mine && (
        <div className="mx-auto mb-4 max-w-measure text-sm">
          <AuthorActions p={p} />
        </div>
      )}
      <Reader
        title={title}
        byline={
          <>
            <Byline p={p} />
            <Cast characters={p.characters} className="mt-3" />
          </>
        }
        parts={bodies}
        part={partFrom(params.get("part"), bodies.length)}
        onPartChange={onPartChange}
      />
      {!isDraft && !mine && <ReaderActions p={p} />}
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
  // "That was me" on the arc page links here with ?claim=1.
  const [params] = useSearchParams();
  const claimOnArrival = params.get("claim") === "1";
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
      <PerspectiveActions summary={summary} rights={rights} withCast={false} claimOnArrival={claimOnArrival} />
    </section>
  );
}

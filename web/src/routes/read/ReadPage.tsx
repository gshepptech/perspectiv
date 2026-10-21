import { Code, ConnectError } from "@connectrpc/connect";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft } from "lucide-react";
import { useCallback, useState } from "react";
import { Link, useLocation, useNavigate, useParams, useSearchParams } from "react-router";

import { InviteDialog } from "@/components/actions/InviteDialog";
import { perspectiveQuery } from "@/components/actions/queries";
import { formatDay, genreLabel, loreStatusLabel } from "@/components/reader/format";
import { Cast } from "@/components/reader/Cast";
import { Reader } from "@/components/reader/Reader";
import { Button } from "@/components/ui/Button";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogTitle, DialogTrigger } from "@/components/ui/Dialog";
import { Genre, PerspectiveStatus } from "@/gen/perspectiv/v1/enums_pb";
import type { Perspective } from "@/gen/perspectiv/v1/perspective_pb";
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
  const bits = [
    p.genre !== Genre.UNSPECIFIED ? genreLabel(p.genre) : "",
    loreStatusLabel(p.loreStatus),
  ].filter(Boolean);
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
      {bits.map((b) => (
        <span key={b}>{b}</span>
      ))}
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
function AuthorActions({ p, justPublished = false }: { p: Perspective; justPublished?: boolean }) {
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
      {!isDraft && <InviteDialog p={p} defaultOpen={justPublished} />}
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
  // The composer arrives here with justPublished set, which opens the invite
  // dialog once: right after publishing is when the author knows who was there.
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
      {!isDraft && mine && (
        <div className="mx-auto mb-4 max-w-measure text-sm">
          <AuthorActions p={p} justPublished={justPublished} />
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
    </div>
  );
}

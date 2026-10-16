import { Code, ConnectError } from "@connectrpc/connect";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft } from "lucide-react";
import { useCallback } from "react";
import { Link, useParams, useSearchParams } from "react-router";

import { formatDay, genreLabel, loreStatusLabel } from "@/components/reader/format";
import { Reader } from "@/components/reader/Reader";
import { Genre, PerspectiveStatus } from "@/gen/perspectiv/v1/enums_pb";
import type { Perspective } from "@/gen/perspectiv/v1/perspective_pb";
import { clients } from "@/lib/connect";

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
      {p.author && <span>by {p.author.username}</span>}
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
 * Reads one perspective a part at a time. The current part is the `part`
 * search parameter, and each page change pushes a history entry, so a page
 * is linkable and the back button returns to the previous part.
 */
export function Component() {
  const { perspectiveId = "" } = useParams();
  const [params, setParams] = useSearchParams();
  const query = useQuery({
    queryKey: ["perspective", "get", { perspectiveId }],
    queryFn: ({ signal }) => clients.perspective.get({ perspectiveId }, { signal }),
  });
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
  const p = query.data.perspective;
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
  const bodies = p.parts.toSorted((a, b) => a.position - b.position).map((part) => part.body);
  const title = p.arcTitle || p.titleIfNewArc || "Untitled";

  return (
    <div className="min-w-0">
      <BackLink p={p} />
      {isDraft && (
        <p className="mx-auto mb-4 flex max-w-measure flex-wrap items-center gap-3 rounded-md border border-warning px-3 py-2 text-sm">
          <span className="font-medium text-warning">Draft</span>
          <span className="text-ink-muted">Only you can see this.</span>
          <Link to={`/compose?perspectiveId=${encodeURIComponent(p.id)}`} className="text-accent underline underline-offset-4">
            Edit draft
          </Link>
        </p>
      )}
      <Reader
        title={title}
        byline={<Byline p={p} />}
        parts={bodies}
        part={partFrom(params.get("part"), bodies.length)}
        onPartChange={onPartChange}
      />
    </div>
  );
}

import { Code, ConnectError } from "@connectrpc/connect";
import { useQuery } from "@tanstack/react-query";
import { CalendarDays, MapPin, PenLine } from "lucide-react";
import { Link, useParams } from "react-router";

import { formatDayBeside } from "@/components/reader/format";
import { Button } from "@/components/ui/Button";
import type { Invite } from "@/gen/perspectiv/v1/perspective_pb";
import { clients } from "@/lib/connect";
import { useSession } from "@/lib/session";

/** The composer, opened at the invite's place and day with the invite's code. */
function writeHref(inv: Invite): string {
  const q = new URLSearchParams({ placeId: inv.place?.id ?? "", date: inv.localDate, invite: inv.code });
  if (inv.hour !== undefined) q.set("hour", String(inv.hour));
  return `/compose?${q.toString()}`;
}

function Gone() {
  return (
    <section className="mx-auto max-w-measure py-12" aria-labelledby="invite-gone">
      <h1 id="invite-gone" className="text-2xl sm:text-3xl">
        This invite no longer works
      </h1>
      <p className="mt-3 text-ink-muted">The story it pointed to was removed, or the link is wrong.</p>
      <p className="mt-6">
        <Link to="/" className="text-accent underline underline-offset-4">
          Find a place
        </Link>
      </p>
    </section>
  );
}

/**
 * Where an "I was there too" link lands. It works signed out: it says who
 * wrote, where and when, shows the opening of their story, and asks the
 * reader whether they were there, or whether they were the character the
 * inviter named. Writing goes through sign-in and comes back to the composer
 * with the place, day and invite already set.
 */
export function Component() {
  const { code = "" } = useParams();
  const session = useSession();
  const query = useQuery({
    queryKey: ["perspective", "getInvite", { code }],
    queryFn: ({ signal }) => clients.perspective.getInvite({ code }, { signal }),
    retry: false,
  });
  if (query.isPending) {
    return (
      <p role="status" className="mx-auto max-w-measure text-ink-muted">
        Opening the invite…
      </p>
    );
  }
  if (query.isError) {
    if (ConnectError.from(query.error).code === Code.NotFound) return <Gone />;
    return (
      <p role="alert" className="mx-auto max-w-measure text-error">
        This invite could not be opened. Try again in a moment.
      </p>
    );
  }
  const inv = query.data.invite;
  if (!inv) return <Gone />;
  const who = inv.inviter?.username ?? "Someone";
  return (
    <section className="mx-auto min-w-0 max-w-measure pt-6 pb-16 sm:pt-12" aria-labelledby="invite-title">
      <p className="text-ink-muted">
        <span className="font-medium text-ink">{who}</span> wrote about where they were, and thinks you might have been
        there too.
      </p>
      <h1 id="invite-title" className="mt-3 text-3xl leading-tight sm:text-5xl">
        {inv.characterLabel ? `Were you ${inv.characterLabel}?` : "Were you there too?"}
      </h1>
      <p className="mt-4 flex flex-wrap gap-2 text-sm">
        <span className="inline-flex min-w-0 items-center gap-1 rounded-full border border-rule bg-paper px-2.5 py-0.5 text-ink-muted">
          <MapPin aria-hidden size={14} />
          <span className="min-w-0 wrap-anywhere">{inv.place?.name}</span>
        </span>
        <span className="inline-flex items-center gap-1 rounded-full border border-rule bg-paper px-2.5 py-0.5 text-ink-muted">
          <CalendarDays aria-hidden size={14} />
          {formatDayBeside(inv.arcTitle, inv.localDate, inv.hour)}
        </span>
      </p>
      <figure className="mt-6 rounded-xl border border-rule bg-paper p-5 shadow-sm">
        <figcaption className="text-sm font-medium text-ink-muted">
          {who} · {inv.arcTitle}
        </figcaption>
        <blockquote className="mt-2 font-serif text-prose wrap-anywhere text-ink">{inv.excerpt}</blockquote>
      </figure>
      <div className="mt-6 flex flex-wrap items-center gap-3">
        <Button asChild>
          <Link to={writeHref(inv)}>
            <PenLine aria-hidden size={16} />
            Write your side
          </Link>
        </Button>
        <Link to={`/arcs/${encodeURIComponent(inv.arcId)}`} className="inline-flex min-h-11 items-center text-accent underline underline-offset-4">
          Read the whole story first
        </Link>
      </div>
      {!session && (
        <p className="mt-4">
          New here?{" "}
          <Link to={`/signup?code=${encodeURIComponent(inv.code)}`} className="font-medium text-accent underline underline-offset-4">
            Sign up with this invite
          </Link>
        </p>
      )}
      <p className="mt-4 text-sm text-ink-muted">
        {inv.characterLabel
          ? `After you write, claim “${inv.characterLabel}” on ${who}’s story and the two of you link up.`
          : "Anyone can read. You need an account to write, and it takes a minute."}
      </p>
    </section>
  );
}

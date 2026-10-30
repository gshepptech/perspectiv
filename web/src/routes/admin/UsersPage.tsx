import { timestampDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { useQuery } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { Link, useSearchParams } from "react-router";

import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { ReportStatus, Role } from "@/gen/perspectiv/v1/enums_pb";
import type { AdminUserView } from "@/gen/perspectiv/v1/moderation_pb";
import { clients } from "@/lib/connect";
import { useSession } from "@/lib/session";

import { AuditList } from "./AuditList";
import { ReportList } from "./ReportList";

const day = new Intl.DateTimeFormat(undefined, { dateStyle: "medium" });
const dayTime = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });

function Fact({ term, children }: { term: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-sm text-ink-muted">{term}</dt>
      <dd className="[overflow-wrap:anywhere]">{children}</dd>
    </div>
  );
}

function Standing({ u }: { u: AdminUserView }) {
  const marks: { text: string; tone: string }[] = [];
  if (u.role === Role.ADMIN) marks.push({ text: "Admin", tone: "border-accent text-accent" });
  if (u.suspendedAt) marks.push({ text: `Suspended ${day.format(timestampDate(u.suspendedAt))}`, tone: "border-warning text-warning" });
  if (u.deleted) marks.push({ text: "Deleted", tone: "border-rule text-ink-muted" });
  marks.push(u.emailVerified ? { text: "Email verified", tone: "border-rule text-ink-muted" } : { text: "Email not verified", tone: "border-warning text-warning" });
  return (
    <ul aria-label="Standing" className="flex flex-wrap gap-2">
      {marks.map((m) => (
        <li key={m.text} className={`rounded-full border px-2.5 py-0.5 text-sm ${m.tone}`}>
          {m.text}
        </li>
      ))}
    </ul>
  );
}

/** One account as a moderator sees it, with the reports and actions about it. */
function Dossier({ u }: { u: AdminUserView }) {
  const name = u.deleted ? "[deleted]" : u.username;
  return (
    <article aria-labelledby="dossier-name" className="grid min-w-0 gap-6">
      <div className="grid gap-2">
        <h2 id="dossier-name" className="text-2xl [overflow-wrap:anywhere]">
          {name}
        </h2>
        <Standing u={u} />
      </div>
      <dl className="grid gap-x-6 gap-y-3 sm:grid-cols-2">
        <Fact term="Email">{u.emailHint || "None (account deleted)"}</Fact>
        <Fact term="Joined">{u.joinedAt ? day.format(timestampDate(u.joinedAt)) : "Unknown"}</Fact>
        <Fact term="Last sign-in">{u.lastSignInAt ? dayTime.format(timestampDate(u.lastSignInAt)) : "Never"}</Fact>
        <Fact term="Failed sign-ins, last 24 hours">{u.failedSignIns24h}</Fact>
        <Fact term="Stories">
          {u.stories}
          {u.hiddenStories > 0 && `, ${u.hiddenStories} hidden`}
        </Fact>
        <Fact term="Reports against">
          {u.openReportsAgainst} open of {u.reportsAgainst}
        </Fact>
        <Fact term="Reports filed">{u.reportsFiled}</Fact>
        <Fact term="Account id">
          <code className="text-sm">{u.userId}</code>
        </Fact>
      </dl>
      {!u.deleted && (
        <p>
          <Link to={`/users/${encodeURIComponent(u.username)}`} className="text-accent underline underline-offset-4">
            Public profile
          </Link>
        </p>
      )}
      <section aria-labelledby="dossier-reports" className="grid gap-2 border-t border-rule pt-4">
        <h3 id="dossier-reports" className="text-xl">
          Reports against them
        </h3>
        <ReportList
          status={ReportStatus.UNSPECIFIED}
          subjectUserId={u.userId}
          label={`Reports against ${name}`}
          empty="No reports against this account or anything it wrote."
          onResolved={() => undefined}
        />
      </section>
      <section aria-labelledby="dossier-actions" className="grid gap-2 border-t border-rule pt-4">
        <h3 id="dossier-actions" className="text-xl">
          Moderator actions
        </h3>
        <AuditList subjectUserId={u.userId} label={`Actions about ${name}`} empty="No moderator has acted on this account." />
      </section>
    </article>
  );
}

function Lookup({ query }: { query: string }) {
  // Each match writes an audit entry, so a lookup is never retried or
  // refetched on its own.
  const found = useQuery({
    queryKey: ["moderation", "lookupUser", query],
    queryFn: () => clients.moderation.lookupUser({ query }),
    retry: false,
    staleTime: Infinity,
    refetchOnWindowFocus: false,
  });
  if (found.isError) {
    const e = ConnectError.from(found.error);
    const text =
      e.code === Code.NotFound
        ? `No account matches “${query}”.`
        : e.code === Code.PermissionDenied
          ? "Only site admins can look up users."
          : e.code === Code.InvalidArgument
            ? e.rawMessage
            : `The lookup failed: ${e.rawMessage}`;
    return (
      <p role="alert" className="font-medium text-error [overflow-wrap:anywhere]">
        {text}
      </p>
    );
  }
  if (!found.data?.user) {
    return (
      <p role="status" className="text-ink-muted">
        Looking up…
      </p>
    );
  }
  return <Dossier u={found.data.user} />;
}

/**
 * /admin/users: find one account by username, email or id and read its
 * dossier. The query lives in the address, so the report queue and the
 * audit log link straight to a dossier.
 */
export function Component() {
  const session = useSession();
  const [params, setParams] = useSearchParams();
  const query = params.get("q")?.trim() ?? "";
  const [text, setText] = useState(query);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const q = text.trim();
    if (q) setParams({ q });
  };
  return (
    <section aria-labelledby="users-title" className="mx-auto grid w-full max-w-shell min-w-0 gap-6">
      <h1 id="users-title" className="text-2xl sm:text-3xl">
        Look up a user
      </h1>
      {session?.role !== Role.ADMIN ? (
        <p className="text-ink-muted">Only site admins can look up users.</p>
      ) : (
        <>
          <form onSubmit={submit} className="flex min-w-0 flex-wrap items-end gap-3" aria-label="Look up a user">
            <div className="grid min-w-0 flex-1 basis-64 gap-1">
              <label htmlFor="lookup-query" className="text-sm font-medium">
                Username, email or account id
              </label>
              <Input
                id="lookup-query"
                value={text}
                onChange={(e) => setText(e.target.value)}
                autoComplete="off"
                autoCapitalize="none"
                spellCheck={false}
              />
            </div>
            <Button type="submit">Look up</Button>
          </form>
          {query && <Lookup key={query} query={query} />}
        </>
      )}
    </section>
  );
}

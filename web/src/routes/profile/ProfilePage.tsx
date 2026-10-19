import { Code, ConnectError } from "@connectrpc/connect";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router";

import type { Profile } from "@/gen/perspectiv/v1/user_pb";
import { clients } from "@/lib/connect";

const joinedFormat = new Intl.DateTimeFormat("en-US", { month: "long", year: "numeric", timeZone: "UTC" });

function Missing() {
  return (
    <section className="mx-auto max-w-measure py-12" aria-labelledby="profile-missing">
      <h1 id="profile-missing" className="text-2xl sm:text-3xl">
        User not found
      </h1>
      <p className="mt-3 text-ink-muted">No one goes by that username.</p>
      <p className="mt-6">
        <Link to="/" className="text-accent underline underline-offset-4">
          Search for a place
        </Link>
      </p>
    </section>
  );
}

function Header({ profile }: { profile: Profile }) {
  return (
    <header className="flex min-w-0 flex-wrap items-center gap-4">
      {profile.avatarUrl && (
        <img
          src={profile.avatarUrl}
          alt={`${profile.username}'s avatar`}
          className="size-20 shrink-0 rounded-full border border-rule object-cover"
        />
      )}
      <div className="min-w-0">
        <h1 className="text-2xl wrap-anywhere sm:text-3xl">{profile.username}</h1>
        {profile.joinedAt && (
          <p className="mt-1 text-sm text-ink-muted">Joined {joinedFormat.format(timestampDate(profile.joinedAt))}</p>
        )}
        {profile.suspended && <p className="mt-1 text-sm text-warning">This account is suspended.</p>}
      </div>
    </header>
  );
}

/**
 * A user's public profile, looked up by username. It shows only the public
 * Profile message; the Account (and its email) the server returns for the
 * caller's own profile is never rendered here.
 */
export function Component() {
  const { username = "" } = useParams();
  const query = useQuery({
    queryKey: ["user", "getProfile", { username }],
    queryFn: ({ signal }) => clients.user.getProfile({ username }, { signal }),
  });

  if (query.isPending) {
    return (
      <p role="status" className="mx-auto max-w-measure text-ink-muted">
        Loading…
      </p>
    );
  }
  if (query.isError) {
    if (ConnectError.from(query.error).code === Code.NotFound) return <Missing />;
    return (
      <p role="alert" className="mx-auto max-w-measure text-error">
        This profile could not be loaded. Try again in a moment.
      </p>
    );
  }
  const profile = query.data.profile;
  if (!profile) return <Missing />;
  if (profile.deleted) {
    return (
      <section className="mx-auto max-w-measure py-12" aria-labelledby="profile-gone">
        <h1 id="profile-gone" className="text-2xl sm:text-3xl">
          {username}
        </h1>
        <p className="mt-3 text-ink-muted">This account was deleted.</p>
      </section>
    );
  }
  return (
    <article className="mx-auto min-w-0 max-w-measure">
      <Header profile={profile} />
      {profile.bio && <p className="mt-6 whitespace-pre-line wrap-anywhere">{profile.bio}</p>}
    </article>
  );
}

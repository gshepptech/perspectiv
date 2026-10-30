import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";

import { Button } from "@/components/ui/Button";
import { clients } from "@/lib/connect";
import { FormAlert } from "@/routes/auth/AuthPanel";
import { errorMessage } from "@/routes/auth/errors";

import { followedWritersKey } from "./queries";
import { Section } from "./Section";

/**
 * The writers the caller follows, newest follow first, each with an
 * Unfollow. A row leaves once the server confirms the unfollow.
 */
export function FollowedWritersSection() {
  const queryClient = useQueryClient();
  const list = useQuery({
    queryKey: followedWritersKey,
    queryFn: ({ signal }) => clients.user.listFollowedWriters({}, { signal }),
  });
  const unfollow = useMutation({
    mutationFn: (username: string) => clients.user.followUser({ username, on: false }),
    onSuccess: (_res, username) =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: followedWritersKey }),
        queryClient.invalidateQueries({ queryKey: ["user", "getProfile", { username }] }),
      ]),
  });

  const writers = list.data?.writers ?? [];
  return (
    <Section title="Writers you follow">
      <p className="text-sm text-ink-muted">A new story by a writer you follow lands in your inbox.</p>
      {list.isPending && (
        <p role="status" className="mt-4 text-ink-muted">
          Loading…
        </p>
      )}
      {list.isError && (
        <div className="mt-4">
          <FormAlert message={errorMessage(list.error)} />
        </div>
      )}
      {list.isSuccess && writers.length === 0 && <p className="mt-4">You do not follow any writers yet.</p>}
      {writers.length > 0 && (
        <ul className="mt-4 divide-y divide-rule border-y border-rule">
          {writers.map((w) => (
            <li key={w.userId} className="flex min-h-11 min-w-0 items-center justify-between gap-3 py-2">
              <Link
                to={`/users/${encodeURIComponent(w.username)}`}
                className="flex min-w-0 items-center gap-2 text-accent underline-offset-4 hover:underline"
              >
                {w.avatarUrl && <img src={w.avatarUrl} alt="" className="size-8 shrink-0 rounded-full border border-rule object-cover" />}
                <span className="min-w-0 break-words">{w.username}</span>
              </Link>
              <Button
                variant="secondary"
                size="sm"
                aria-label={`Unfollow ${w.username}`}
                pending={unfollow.isPending && unfollow.variables === w.username}
                onClick={() => unfollow.mutate(w.username)}
              >
                Unfollow
              </Button>
            </li>
          ))}
        </ul>
      )}
      {unfollow.isError && (
        <div className="mt-2">
          <FormAlert message={errorMessage(unfollow.error)} />
        </div>
      )}
    </Section>
  );
}

import { Code, ConnectError } from "@connectrpc/connect";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";

import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import type { UserRef } from "@/gen/perspectiv/v1/user_pb";
import { clients } from "@/lib/connect";
import { useSession } from "@/lib/session";
import { FormAlert } from "@/routes/auth/AuthPanel";
import { errorMessage } from "@/routes/auth/errors";

import { followedWritersKey, myProfileKey } from "./queries";
import { Section } from "./Section";

class BlockRefusal extends Error {}

/**
 * Users the caller has blocked, with an unblock per row, and a block by
 * username. Blocking only stops interaction (claims, votes, reacts and
 * notifications from them); reading is unchanged, which the copy says.
 */
export function BlocksSection({ blocked }: { blocked: UserRef[] }) {
  const queryClient = useQueryClient();
  const session = useSession();
  const [username, setUsername] = useState("");
  const refresh = () => queryClient.invalidateQueries({ queryKey: myProfileKey });

  const block = useMutation({
    mutationFn: async (name: string) => {
      let userId: string;
      try {
        userId = (await clients.user.getProfile({ username: name })).profile?.userId ?? "";
      } catch (err) {
        const code = ConnectError.from(err).code;
        if (code === Code.NotFound || code === Code.InvalidArgument) throw new BlockRefusal(`No user is called ${name}.`);
        throw err;
      }
      if (!userId) throw new BlockRefusal(`No user is called ${name}.`);
      if (userId === session?.userId) throw new BlockRefusal("You cannot block yourself.");
      await clients.user.blockUser({ userId });
    },
    onSuccess: async () => {
      setUsername("");
      // A block removes any follow between the two, so the followed list changes too.
      await Promise.all([refresh(), queryClient.invalidateQueries({ queryKey: followedWritersKey })]);
    },
  });
  const unblock = useMutation({
    mutationFn: (userId: string) => clients.user.unblockUser({ userId }),
    onSuccess: refresh,
  });

  function submit(e: FormEvent) {
    e.preventDefault();
    const name = username.trim();
    if (name) block.mutate(name);
  }

  const failure = block.error ?? unblock.error;
  return (
    <Section title="Blocked users">
      <p className="text-sm text-ink-muted">
        A blocked user cannot claim your characters, vote on or react to your perspectives, or set off notifications to
        you. Blocking also ends any follow between you, both ways. You can both still read everything.
      </p>
      {blocked.length === 0 ? (
        <p className="mt-4">You have not blocked anyone.</p>
      ) : (
        <ul className="mt-4 divide-y divide-rule border-y border-rule">
          {blocked.map((u) => (
            <li key={u.userId} className="flex min-w-0 items-center justify-between gap-3 py-2">
              <span className="min-w-0 break-words">{u.username}</span>
              <Button
                variant="secondary"
                size="sm"
                aria-label={`Unblock ${u.username}`}
                pending={unblock.isPending && unblock.variables === u.userId}
                onClick={() => unblock.mutate(u.userId)}
              >
                Unblock
              </Button>
            </li>
          ))}
        </ul>
      )}
      <form noValidate onSubmit={submit} className="mt-4 grid gap-2">
        <label htmlFor="block-username" className="text-sm font-medium">
          Username to block
        </label>
        <div className="flex min-w-0 gap-2">
          <Input
            id="block-username"
            value={username}
            autoCapitalize="none"
            spellCheck={false}
            onChange={(e) => setUsername(e.target.value)}
          />
          <Button type="submit" variant="secondary" pending={block.isPending}>
            Block
          </Button>
        </div>
        <FormAlert message={failure ? (failure instanceof BlockRefusal ? failure.message : errorMessage(failure)) : null} />
      </form>
    </Section>
  );
}

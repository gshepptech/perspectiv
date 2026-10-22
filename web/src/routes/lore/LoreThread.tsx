import { timestampDate } from "@bufbuild/protobuf/wkt";
import { zodResolver } from "@hookform/resolvers/zod";
import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useId } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { actionError } from "@/components/actions/errors";
import { ReportDialog } from "@/components/actions/ReportDialog";
import { AuthorLink } from "@/components/reader/AuthorLink";
import { Button } from "@/components/ui/Button";
import { Textarea } from "@/components/ui/Textarea";
import { ReportTargetKind } from "@/gen/perspectiv/v1/enums_pb";
import { clients } from "@/lib/connect";
import { useSession } from "@/lib/session";

const MAX_POST = 1000;

const schema = z.object({
  body: z
    .string()
    .refine((b) => b.trim() !== "", "Write something first.")
    .refine((b) => [...b.trim()].length <= MAX_POST, `Keep a post to ${MAX_POST} characters.`),
});
type Values = z.infer<typeof schema>;

const when = new Intl.DateTimeFormat("en-US", { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" });

/**
 * The lore's thread: the people in it talking about the day. Anyone can
 * read it; someone with a perspective in the arc can post. The server
 * enforces who may post; hiding the form is usability only.
 */
export function LoreThread({ loreId, canPost }: { loreId: string; canPost: boolean }) {
  const client = useQueryClient();
  const session = useSession();
  const headingId = useId();
  const fieldId = useId();
  const key = ["lore", "listLorePosts", { loreId }] as const;
  const posts = useInfiniteQuery({
    queryKey: key,
    queryFn: ({ pageParam, signal }) => clients.lore.listLorePosts({ loreId, pageToken: pageParam, pageSize: 50 }, { signal }),
    initialPageParam: "",
    getNextPageParam: (last) => last.nextPageToken || undefined,
  });
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: { body: "" } });
  const add = useMutation({
    mutationFn: (v: Values) => clients.lore.addLorePost({ loreId, body: v.body.trim() }),
    onSuccess: async () => {
      form.reset();
      await client.invalidateQueries({ queryKey: key });
    },
  });
  const items = posts.data?.pages.flatMap((p) => p.posts) ?? [];
  const error = form.formState.errors.body?.message;

  return (
    <section aria-labelledby={headingId} className="mt-10 border-t border-rule pt-8">
      <h2 id={headingId} className="text-xl">
        Thread
      </h2>
      {posts.isError && (
        <p role="alert" className="mt-3 text-sm text-error">
          The thread could not be loaded. Try again in a moment.
        </p>
      )}
      {posts.isSuccess && items.length === 0 && (
        <p className="mt-3 text-ink-muted">Nobody has said anything yet. Were you there? Start the conversation.</p>
      )}
      {items.length > 0 && (
        <ol aria-label="Posts" className="mt-4 grid gap-4">
          {items.map((p) => (
            <li key={p.id} className="min-w-0">
              <p className="flex flex-wrap items-baseline gap-x-2 text-sm text-ink-muted">
                {p.author && <AuthorLink username={p.author.username} />}
                {p.byMember && <span className="rounded-full bg-accent/10 px-2 text-xs font-medium text-accent">In this lore</span>}
                {p.createdAt && <time dateTime={timestampDate(p.createdAt).toISOString()}>{when.format(timestampDate(p.createdAt))}</time>}
                {p.author?.userId !== session?.userId && (
                  <span className="ml-auto">
                    <ReportDialog
                      targetKind={ReportTargetKind.LORE_POST}
                      targetId={p.id}
                      targetLabel={`${p.author?.username ?? "this"}'s post`}
                    />
                  </span>
                )}
              </p>
              <p className="mt-1 whitespace-pre-line wrap-anywhere text-ink">{p.body}</p>
            </li>
          ))}
        </ol>
      )}
      {posts.hasNextPage && (
        <Button variant="ghost" size="sm" className="mt-3" disabled={posts.isFetchingNextPage} onClick={() => void posts.fetchNextPage()}>
          Older posts
        </Button>
      )}
      {canPost ? (
        <form noValidate className="mt-6 grid gap-2" onSubmit={form.handleSubmit((v) => add.mutate(v))}>
          <label htmlFor={fieldId} className="text-sm font-medium text-ink">
            Add to the thread
          </label>
          <Textarea
            id={fieldId}
            rows={3}
            maxLength={MAX_POST}
            aria-invalid={error ? true : undefined}
            {...form.register("body")}
          />
          {error && <p className="text-sm font-medium text-error">{error}</p>}
          {add.isError && (
            <p role="alert" className="text-sm text-error">
              {actionError(add.error)}
            </p>
          )}
          <Button type="submit" className="justify-self-start" disabled={add.isPending}>
            {add.isPending ? "Posting…" : "Post"}
          </Button>
        </form>
      ) : (
        <p className="mt-6 text-sm text-ink-muted">Write your perspective in this arc to join the thread.</p>
      )}
    </section>
  );
}

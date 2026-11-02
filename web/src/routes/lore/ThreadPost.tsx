import { timestampDate } from "@bufbuild/protobuf/wkt";
import { useMutation } from "@tanstack/react-query";
import { Pencil, Reply, Users } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { ReportDialog } from "@/components/actions/ReportDialog";
import { AuthorLink } from "@/components/reader/AuthorLink";
import { Button } from "@/components/ui/Button";
import { ReportTargetKind } from "@/gen/perspectiv/v1/enums_pb";
import type { LorePost } from "@/gen/perspectiv/v1/lore_pb";
import { cn } from "@/lib/cn";
import { clients } from "@/lib/connect";

import { DeletePostDialog } from "./DeletePostDialog";
import { PostForm } from "./PostForm";
import { postAnchor, useInvalidateThread } from "./threadQueries";
import { ThreadVote } from "./ThreadVote";

const when = new Intl.DateTimeFormat("en-US", { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" });

/**
 * A post is a link target (`#post-<id>`): useThreadTarget focuses it and
 * sets data-highlight for two seconds, drawn as a still outline so reduced
 * motion changes nothing.
 */
const anchorClass = "rounded-sm outline-offset-4 data-highlight:outline-2 data-highlight:outline-accent data-highlight:outline-solid";

type Props = {
  post: LorePost;
  loreId: string;
  /** The viewer may post, reply and vote here: signed in with a story in the arc. */
  canPost: boolean;
  viewerId: string | undefined;
  /** The server's copy of a reply the viewer just wrote here. */
  onReplied: (reply: LorePost) => void;
  /** The server's copy of this post after the viewer edited it. */
  onEdited: (post: LorePost) => void;
  /** The viewer deleted this post. */
  onGone: () => void;
};

/**
 * One thread post: its byline, text and actions, and the reply or edit form
 * when the viewer opens one. A deleted post that stays for its replies is a
 * bare "[deleted]".
 */
export function ThreadPost({ post, loreId, canPost, viewerId, onReplied, onEdited, onGone }: Props) {
  if (post.deleted) {
    return (
      <article id={postAnchor(post.id)} tabIndex={-1} aria-label="Deleted post" data-post-id={post.id} className={anchorClass}>
        <p className="text-ink-muted italic">[deleted]</p>
      </article>
    );
  }
  return (
    <LivePost post={post} loreId={loreId} canPost={canPost} viewerId={viewerId} onReplied={onReplied} onEdited={onEdited} onGone={onGone} />
  );
}

function LivePost({ post, loreId, canPost, viewerId, onReplied, onEdited, onGone }: Props) {
  const invalidate = useInvalidateThread();
  const name = post.author?.username ?? "someone";
  const mine = !!viewerId && post.author?.userId === viewerId;
  const [open, setOpen] = useState<"reply" | "edit" | null>(null);
  // Where focus goes back to when a form closes.
  const replyButton = useRef<HTMLButtonElement>(null);
  const editButton = useRef<HTMLButtonElement>(null);
  const closed = useRef<"reply" | "edit" | null>(null);
  useEffect(() => {
    if (open !== null || closed.current === null) return;
    (closed.current === "reply" ? replyButton : editButton).current?.focus();
    closed.current = null;
  }, [open]);
  const close = () => {
    closed.current = open;
    setOpen(null);
  };

  const reply = useMutation({
    mutationFn: (body: string) => clients.lore.addLorePost({ loreId, body, replyToId: post.id }),
    onSuccess: async (res) => {
      setOpen(null);
      if (res.post) onReplied(res.post);
      await invalidate();
    },
  });
  const edit = useMutation({
    mutationFn: (body: string) => clients.lore.editLorePost({ postId: post.id, body }),
    onSuccess: async (res) => {
      if (res.post) onEdited(res.post);
      close();
      await invalidate();
    },
  });

  return (
    <article id={postAnchor(post.id)} tabIndex={-1} aria-label={`${name}'s post`} data-post-id={post.id} className={cn("min-w-0", anchorClass)}>
      <p className="flex flex-wrap items-baseline gap-x-2 text-sm text-ink-muted">
        {post.author && <AuthorLink username={post.author.username} className="inline-flex min-h-11 items-center" />}
        {post.byMember && (
          <span className="inline-flex items-center gap-1 self-center font-medium text-ink">
            <Users aria-hidden size={16} className="text-ink-muted" />
            In this lore
          </span>
        )}
        {post.createdAt && <time dateTime={timestampDate(post.createdAt).toISOString()}>{when.format(timestampDate(post.createdAt))}</time>}
        {post.editedAt && (
          <span title={`Edited ${when.format(timestampDate(post.editedAt))}`}>
            <span aria-hidden>· </span>edited
          </span>
        )}
      </p>
      {post.replyingTo && <p className="mt-1 text-sm text-ink-muted">Replying to @{post.replyingTo.username}</p>}
      {open === "edit" ? (
        <div className="mt-2">
          <PostForm
            label="Edit your post"
            submitLabel="Save"
            pendingLabel="Saving…"
            defaultBody={post.body}
            send={(body, done) => edit.mutate(body, { onSuccess: done })}
            pending={edit.isPending}
            error={edit.isError ? edit.error : null}
            onCancel={() => {
              edit.reset();
              close();
            }}
          />
        </div>
      ) : (
        <p className="mt-1 whitespace-pre-line wrap-anywhere text-ink">{post.body}</p>
      )}
      <div className="mt-1 flex flex-wrap items-center gap-x-1">
        <ThreadVote post={post} canVote={canPost && !mine} targetName={`${name}'s post`} />
        {canPost && (
          <Button
            ref={replyButton}
            variant="ghost"
            size="sm"
            className="min-h-11 text-ink-muted"
            aria-expanded={open === "reply"}
            onClick={() => setOpen(open === "reply" ? null : "reply")}
          >
            <Reply aria-hidden size={16} />
            Reply
          </Button>
        )}
        {mine ? (
          <>
            <Button ref={editButton} variant="ghost" size="sm" className="min-h-11 text-ink-muted" onClick={() => setOpen("edit")}>
              <Pencil aria-hidden size={16} />
              Edit
            </Button>
            <DeletePostDialog postId={post.id} hasReplies={post.replyCount > 0} onGone={onGone} />
          </>
        ) : (
          <ReportDialog targetKind={ReportTargetKind.LORE_POST} targetId={post.id} targetLabel={`${name}'s post`} />
        )}
      </div>
      {open === "reply" && (
        <div className="mt-2">
          <PostForm
            label={`Reply to ${name}`}
            submitLabel="Post reply"
            pendingLabel="Posting…"
            send={(body, done) => reply.mutate(body, { onSuccess: done })}
            pending={reply.isPending}
            error={reply.isError ? reply.error : null}
            onCancel={() => {
              reply.reset();
              close();
            }}
          />
        </div>
      )}
    </article>
  );
}

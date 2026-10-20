import { useMutation } from "@tanstack/react-query";
import { Check, Copy, Share2, UserPlus } from "lucide-react";
import { useId, useState } from "react";

import { Button } from "@/components/ui/Button";
import { Dialog, DialogContent, DialogDescription, DialogTitle, DialogTrigger } from "@/components/ui/Dialog";
import type { Perspective } from "@/gen/perspectiv/v1/perspective_pb";
import { clients } from "@/lib/connect";

import { actionError } from "./errors";

/** The address an invite code opens, on this site. */
export function inviteURL(code: string): string {
  return `${window.location.origin}/i/${code}`;
}

/**
 * "Invite who was there": the author picks, if they like, one of their
 * characters to ask about, and gets a link to send to the people who were
 * there too. The phone's share sheet is offered where the browser has one.
 */
export function InviteDialog({
  p,
  defaultOpen = false,
  trigger = "Invite who was there",
}: {
  p: Perspective;
  defaultOpen?: boolean;
  trigger?: string;
}) {
  const [open, setOpen] = useState(defaultOpen);
  const [characterId, setCharacterId] = useState("");
  const [copied, setCopied] = useState(false);
  const groupId = useId();
  const create = useMutation({
    mutationFn: () => clients.perspective.createInvite({ perspectiveId: p.id, characterId }),
  });
  const url = create.data ? inviteURL(create.data.code) : "";
  const asked = p.characters.find((c) => c.id === characterId)?.label;
  const message = asked
    ? `Were you ${asked}? I wrote about ${p.place?.name ?? "it"} on Perspectiv. Write your side:`
    : `I wrote about ${p.place?.name ?? "it"} on Perspectiv. Were you there too? Write your side:`;

  function choose(id: string) {
    setCharacterId(id);
    setCopied(false);
    create.reset();
  }

  async function copy() {
    try {
      await navigator.clipboard.writeText(`${message} ${url}`);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setCopied(false);
      }}
    >
      <DialogTrigger asChild>
        <Button size="sm">
          <UserPlus aria-hidden size={16} />
          {trigger}
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogTitle>Who else was there?</DialogTitle>
        <DialogDescription>
          Send this link to people who were at {p.place?.name ?? "the place"} too. They can read your story and write
          their side, and your stories link when one of you recognises the other.
        </DialogDescription>

        {p.characters.length > 0 && (
          <fieldset className="mt-4 grid gap-1" aria-labelledby={groupId}>
            <legend id={groupId} className="text-sm font-medium text-ink">
              Ask about someone in your story
            </legend>
            {[{ id: "", label: "No one in particular" }, ...p.characters].map((c) => (
              <label key={c.id || "none"} className="inline-flex min-h-11 cursor-pointer items-center gap-3 text-ink">
                <input
                  type="radio"
                  name="invite-character"
                  checked={characterId === c.id}
                  onChange={() => choose(c.id)}
                  className="size-5 shrink-0 accent-accent"
                />
                <span className="min-w-0 wrap-anywhere">{c.id ? `Were you ${c.label}?` : c.label}</span>
              </label>
            ))}
          </fieldset>
        )}

        {create.isError && (
          <p role="alert" className="mt-3 text-sm text-error">
            {actionError(create.error)}
          </p>
        )}

        {url ? (
          <div className="mt-4 grid gap-3">
            <p className="rounded-md border border-rule bg-surface p-3 text-sm wrap-anywhere text-ink">
              {message} <span className="text-accent">{url}</span>
            </p>
            <div className="flex flex-wrap gap-2">
              <Button variant="secondary" onClick={() => void copy()}>
                {copied ? <Check aria-hidden size={16} /> : <Copy aria-hidden size={16} />}
                {copied ? "Copied" : "Copy invite"}
              </Button>
              {typeof navigator.share === "function" && (
                <Button
                  onClick={() => void navigator.share({ text: message, url }).catch(() => undefined)}
                >
                  <Share2 aria-hidden size={16} />
                  Share
                </Button>
              )}
            </div>
          </div>
        ) : (
          <Button className="mt-4" disabled={create.isPending} onClick={() => create.mutate()}>
            {create.isPending ? "Making your link…" : "Make the link"}
          </Button>
        )}
      </DialogContent>
    </Dialog>
  );
}

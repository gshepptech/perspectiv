import { timestampDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { zodResolver } from "@hookform/resolvers/zod";
import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useId, useRef, useState, type RefObject } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { Button } from "@/components/ui/Button";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogTitle, DialogTrigger } from "@/components/ui/Dialog";
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/Form";
import { Input } from "@/components/ui/Input";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/Tabs";
import type { AccessCode, WaitlistEntry } from "@/gen/perspectiv/v1/auth_pb";
import { Role } from "@/gen/perspectiv/v1/enums_pb";
import { clients } from "@/lib/connect";
import { useSession } from "@/lib/session";

const ADMINS_ONLY = "Only site admins can manage beta access.";
const codesKey = ["auth", "listAccessCodes"] as const;
const waitlistKey = ["auth", "listWaitlist"] as const;
const dateFormat = new Intl.DateTimeFormat("en-US", { dateStyle: "medium" });
const INVITED_HEADING = "access-invited";

function failure(err: unknown, what: string): string {
  const e = ConnectError.from(err);
  return e.code === Code.PermissionDenied ? ADMINS_ONLY : `${what} failed: ${e.rawMessage}`;
}

/** Live, Used up, Expired or Revoked: a word, never colour alone. */
export function codeStanding(c: AccessCode, now = new Date()): "Live" | "Used up" | "Expired" | "Revoked" {
  if (c.revoked) return "Revoked";
  if (c.expiresAt && timestampDate(c.expiresAt) <= now) return "Expired";
  if (c.uses >= c.maxUses) return "Used up";
  return "Live";
}

export function signUpLink(code: string): string {
  return `${window.location.origin}/signup?code=${encodeURIComponent(code)}`;
}

const createSchema = z.object({
  code: z
    .string()
    .refine((c) => c.trim() === "" || /^[a-z0-9-]{4,32}$/.test(c.trim().toLowerCase()), "4 to 32 letters, digits and dashes, or leave it blank."),
  note: z.string().refine((n) => [...n.trim()].length <= 200, "Keep the note to 200 characters."),
  maxUses: z.string().refine((u) => /^\d+$/.test(u.trim()) && Number(u) >= 1 && Number(u) <= 10000, "1 to 10,000."),
  expiresInDays: z
    .string()
    .refine((d) => d.trim() === "" || (/^\d+$/.test(d.trim()) && Number(d) >= 1 && Number(d) <= 365), "1 to 365 days, or blank for never."),
});
type CreateValues = z.infer<typeof createSchema>;

function CreateCode({ onCreated }: { onCreated: (c: AccessCode) => void }) {
  const client = useQueryClient();
  const form = useForm<CreateValues>({
    resolver: zodResolver(createSchema),
    defaultValues: { code: "", note: "", maxUses: "1", expiresInDays: "" },
  });
  const create = useMutation({
    mutationFn: (v: CreateValues) =>
      clients.auth.createAccessCode({
        code: v.code.trim().toLowerCase(),
        note: v.note.trim(),
        maxUses: Number(v.maxUses),
        expiresInDays: v.expiresInDays.trim() === "" ? 0 : Number(v.expiresInDays),
      }),
    onSuccess: async (res) => {
      form.reset();
      if (res.code) onCreated(res.code);
      await client.invalidateQueries({ queryKey: codesKey });
    },
  });
  return (
    <Form {...form}>
      <form noValidate className="grid gap-3 rounded-md border border-rule bg-surface p-4 sm:grid-cols-2" onSubmit={form.handleSubmit((v) => create.mutate(v))}>
        <FormField
          control={form.control}
          name="code"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Code</FormLabel>
              <FormControl>
                <Input autoCapitalize="none" spellCheck={false} placeholder="ucf-launch" {...field} />
              </FormControl>
              <FormDescription>Blank makes one up.</FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name="note"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Note</FormLabel>
              <FormControl>
                <Input placeholder="Who it is for" {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name="maxUses"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Uses</FormLabel>
              <FormControl>
                <Input type="number" inputMode="numeric" min={1} max={10000} {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name="expiresInDays"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Expires in days</FormLabel>
              <FormControl>
                <Input type="number" inputMode="numeric" min={1} max={365} {...field} />
              </FormControl>
              <FormDescription>Blank never expires.</FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        {create.isError && (
          <p role="alert" className="text-sm text-error sm:col-span-2">
            {failure(create.error, "Creating the code")}
          </p>
        )}
        <Button type="submit" className="justify-self-start" pending={create.isPending}>
          Create code
        </Button>
      </form>
    </Form>
  );
}

function RevokeCode({ code, returnFocusTo }: { code: string; returnFocusTo: RefObject<HTMLElement | null> }) {
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const revoke = useMutation({
    mutationFn: () => clients.auth.revokeAccessCode({ code }),
    onSuccess: async () => {
      setOpen(false);
      await client.invalidateQueries({ queryKey: codesKey });
    },
  });
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm" variant="ghost" aria-label={`Revoke ${code}`}>
          Revoke
        </Button>
      </DialogTrigger>
      <DialogContent
        onCloseAutoFocus={(e) => {
          // Revoke disappears once the code is revoked; focus its name.
          if (revoke.isSuccess && returnFocusTo.current) {
            e.preventDefault();
            returnFocusTo.current.focus();
          }
        }}
      >
        <DialogTitle>Revoke {code}?</DialogTitle>
        <DialogDescription>Nobody else can sign up with it. People who already joined keep their accounts.</DialogDescription>
        {revoke.isError && (
          <p role="alert" className="mt-3 text-sm text-error">
            {failure(revoke.error, "Revoking")}
          </p>
        )}
        <div className="mt-4 flex gap-2">
          <Button variant="danger" pending={revoke.isPending} onClick={() => revoke.mutate()}>
            Revoke code
          </Button>
          <DialogClose asChild>
            <Button variant="secondary">Keep it</Button>
          </DialogClose>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function CopyLink({ code }: { code: string }) {
  const [state, setState] = useState<"idle" | "copied" | "failed">("idle");
  useEffect(() => {
    if (state === "idle") return;
    const t = setTimeout(() => setState("idle"), 4000);
    return () => clearTimeout(t);
  }, [state]);
  return (
    <>
      <Button
        size="sm"
        variant="secondary"
        aria-label={`Copy invite link for ${code}`}
        onClick={() => {
          if (!navigator.clipboard) {
            setState("failed");
            return;
          }
          navigator.clipboard.writeText(signUpLink(code)).then(
            () => setState("copied"),
            () => setState("failed"),
          );
        }}
      >
        Copy invite link
      </Button>
      <span role="status" className={state === "failed" ? "text-sm text-error" : "text-sm text-success"}>
        {state === "copied" ? "Copied." : state === "failed" ? `Copying failed. The link is ${signUpLink(code)}` : ""}
      </span>
    </>
  );
}

function CodeItem({ c }: { c: AccessCode }) {
  const headingId = useId();
  const name = useRef<HTMLSpanElement>(null);
  const standing = codeStanding(c);
  return (
    <li aria-labelledby={headingId} className="grid min-w-0 gap-1 border-b border-rule py-3">
      <p className="flex flex-wrap items-baseline gap-x-3">
        <span id={headingId} ref={name} tabIndex={-1} className="font-mono text-base wrap-anywhere text-ink">
          {c.code}
        </span>
        <span className={standing === "Live" ? "text-sm font-medium text-success" : "text-sm font-medium text-ink-muted"}>{standing}</span>
      </p>
      <p className="text-sm text-ink-muted">
        {c.uses} of {c.maxUses} used
        {c.expiresAt && <> · expires {dateFormat.format(timestampDate(c.expiresAt))}</>}
        {c.createdAt && <> · made {dateFormat.format(timestampDate(c.createdAt))}</>}
      </p>
      {c.note && <p className="text-sm wrap-anywhere">{c.note}</p>}
      {standing === "Live" && (
        <div className="flex flex-wrap gap-2">
          <CopyLink code={c.code} />
          <RevokeCode code={c.code} returnFocusTo={name} />
        </div>
      )}
    </li>
  );
}

function Codes() {
  const [made, setMade] = useState<AccessCode | null>(null);
  const list = useInfiniteQuery({
    queryKey: codesKey,
    queryFn: ({ pageParam, signal }) => clients.auth.listAccessCodes({ pageToken: pageParam, pageSize: 50 }, { signal }),
    initialPageParam: "",
    getNextPageParam: (last) => last.nextPageToken || undefined,
  });
  const codes = list.data?.pages.flatMap((p) => p.codes) ?? [];
  return (
    <div className="grid gap-4">
      <CreateCode onCreated={setMade} />
      <p role="status" className="text-sm text-success">
        {made && `Created ${made.code}.`}
      </p>
      {list.isError && (
        <p role="alert" className="font-medium text-error">
          {failure(list.error, "Loading codes")}
        </p>
      )}
      {list.isSuccess && codes.length === 0 && <p className="text-ink-muted">No codes yet.</p>}
      {codes.length > 0 && (
        <ul aria-label="Beta codes" className="min-w-0">
          {codes.map((c) => (
            <CodeItem key={c.code} c={c} />
          ))}
        </ul>
      )}
      {list.hasNextPage && (
        <Button variant="secondary" className="justify-self-start" pending={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>
          Show more codes
        </Button>
      )}
    </div>
  );
}

function WaitlistItem({ w }: { w: WaitlistEntry }) {
  const client = useQueryClient();
  const headingId = useId();
  const invite = useMutation({
    mutationFn: () => clients.auth.inviteFromWaitlist({ id: w.id }),
    onSuccess: async () => {
      await Promise.all([client.invalidateQueries({ queryKey: waitlistKey }), client.invalidateQueries({ queryKey: codesKey })]);
      // The entry moves to Invited; focus follows to that list's heading.
      requestAnimationFrame(() => document.getElementById(INVITED_HEADING)?.focus());
    },
  });
  return (
    <li aria-labelledby={headingId} className="grid min-w-0 gap-1 border-b border-rule py-3">
      <p id={headingId} className="font-medium wrap-anywhere">
        {w.emailHint}
      </p>
      {w.note && <p className="text-sm wrap-anywhere">{w.note}</p>}
      <p className="text-sm text-ink-muted">
        Asked {w.createdAt && dateFormat.format(timestampDate(w.createdAt))}
        {w.invitedAt && (
          <>
            {" "}
            · invited {dateFormat.format(timestampDate(w.invitedAt))} with <span className="font-mono">{w.code}</span>
          </>
        )}
      </p>
      {invite.isError && (
        <p role="alert" className="text-sm text-error">
          {failure(invite.error, "Sending the invite")}
        </p>
      )}
      <div>
        <Button
          size="sm"
          variant={w.invitedAt ? "ghost" : "primary"}
          aria-label={`${w.invitedAt ? "Send again to" : "Send invite to"} ${w.emailHint}`}
          pending={invite.isPending}
          onClick={() => invite.mutate()}
        >
          {w.invitedAt ? "Send again" : "Send invite"}
        </Button>
      </div>
    </li>
  );
}

function WaitlistList() {
  const list = useInfiniteQuery({
    queryKey: waitlistKey,
    queryFn: ({ pageParam, signal }) => clients.auth.listWaitlist({ pageToken: pageParam, pageSize: 50 }, { signal }),
    initialPageParam: "",
    getNextPageParam: (last) => last.nextPageToken || undefined,
  });
  const entries = list.data?.pages.flatMap((p) => p.entries) ?? [];
  const waiting = entries.filter((w) => !w.invitedAt);
  const invited = entries.filter((w) => w.invitedAt);
  if (list.isError) {
    return (
      <p role="alert" className="font-medium text-error">
        {failure(list.error, "Loading the waitlist")}
      </p>
    );
  }
  return (
    <div className="grid gap-6">
      <section aria-labelledby="waiting-title">
        <h2 id="waiting-title" className="text-xl">
          Waiting ({waiting.length}
          {list.hasNextPage ? "+" : ""})
        </h2>
        {list.isSuccess && waiting.length === 0 && <p className="mt-2 text-ink-muted">Nobody is waiting.</p>}
        <ul aria-label="Waiting" className="min-w-0">
          {waiting.map((w) => (
            <WaitlistItem key={w.id} w={w} />
          ))}
        </ul>
      </section>
      {invited.length > 0 && (
        <section aria-labelledby={INVITED_HEADING}>
          <h2 id={INVITED_HEADING} tabIndex={-1} className="text-xl focus:outline-none">
            Invited
          </h2>
          <ul aria-label="Invited" className="min-w-0">
            {invited.map((w) => (
              <WaitlistItem key={w.id} w={w} />
            ))}
          </ul>
        </section>
      )}
      {list.hasNextPage && (
        <Button variant="secondary" className="justify-self-start" pending={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>
          Show more
        </Button>
      )}
    </div>
  );
}

/**
 * /admin/access: beta codes and the waitlist. The server refuses every call
 * here to anyone without the ADMIN role; this page does not even ask for
 * them when the signed-in user is not an admin.
 */
export function Component() {
  const session = useSession();
  const [tab, setTab] = useState("codes");
  return (
    <section aria-labelledby="access-title" className="mx-auto grid w-full max-w-shell min-w-0 gap-4">
      <h1 id="access-title" className="text-2xl sm:text-3xl">
        Beta access
      </h1>
      {session?.role !== Role.ADMIN ? (
        <p className="text-ink-muted">{ADMINS_ONLY}</p>
      ) : (
        <Tabs value={tab} onValueChange={setTab}>
          <TabsList aria-label="Beta access">
            <TabsTrigger value="codes">Codes</TabsTrigger>
            <TabsTrigger value="waitlist">Waitlist</TabsTrigger>
          </TabsList>
          <TabsContent value="codes" className="pt-4">
            <Codes />
          </TabsContent>
          <TabsContent value="waitlist" className="pt-4">
            <WaitlistList />
          </TabsContent>
        </Tabs>
      )}
    </section>
  );
}

import { timestampDate } from "@bufbuild/protobuf/wkt";
import { ConnectError } from "@connectrpc/connect";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, CircleDashed, X } from "lucide-react";
import { useEffect, useId, useRef, useState, type RefObject } from "react";
import { Link } from "react-router";

import { invalidateAfterWrite, perspectiveQuery } from "@/components/actions/queries";
import { CANON_MIN_MEMBERS } from "@/components/actions/rights";
import { Button } from "@/components/ui/Button";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/Dialog";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/Tabs";
import { ClaimDirection, ClaimStatus } from "@/gen/perspectiv/v1/enums_pb";
import type { Claim } from "@/gen/perspectiv/v1/lore_pb";
import { clients } from "@/lib/connect";
import { useShowMore } from "@/lib/showMore";

const claimsKey = ["lore", "listClaims"] as const;

const dateFormat = new Intl.DateTimeFormat(undefined, { dateStyle: "medium" });

function StatusLabel({ status }: { status: ClaimStatus }) {
  switch (status) {
    case ClaimStatus.PENDING:
      return (
        <span className="inline-flex items-center gap-1 text-sm font-medium text-warning">
          <CircleDashed aria-hidden size={16} /> Pending
        </span>
      );
    case ClaimStatus.ACCEPTED:
      return (
        <span className="inline-flex items-center gap-1 text-sm font-medium text-success">
          <Check aria-hidden size={16} /> Accepted
        </span>
      );
    case ClaimStatus.REJECTED:
      return (
        <span className="inline-flex items-center gap-1 text-sm font-medium text-ink-muted">
          <X aria-hidden size={16} /> Rejected
        </span>
      );
    case ClaimStatus.UNSPECIFIED:
      return null;
  }
}

const linkClass = "text-sm text-accent underline underline-offset-4";

/**
 * What happens next, once the reader has accepted a request: both stories
 * are in one lore now (the server links them as it accepts), and a lore
 * competes for canon once it has enough members. The lore is read from the
 * reader's own story; until that answers, the day stands in for it.
 */
function Linked({ claim, lineRef }: { claim: Claim; lineRef: RefObject<HTMLParagraphElement | null> }) {
  const story = useQuery(perspectiveQuery(claim.perspectiveId));
  const loreId = story.data?.loreId ?? "";
  const lore = loreId ? `/lores/${encodeURIComponent(loreId)}` : `/arcs/${encodeURIComponent(claim.arcId)}`;
  return (
    <div className="grid gap-1">
      <p ref={lineRef} tabIndex={-1} role="status" className="inline-flex flex-wrap items-center gap-x-1 font-medium text-success">
        <Check aria-hidden size={16} />
        Linked. You are both in this story's lore now:{" "}
        <Link to={lore} className="inline-flex min-h-11 items-center text-accent underline underline-offset-4">
          read it
        </Link>
      </p>
      <p className="text-sm text-ink-muted">
        A lore needs {CANON_MIN_MEMBERS} members to compete for canon.{" "}
        <Link
          to={`/perspectives/${encodeURIComponent(claim.perspectiveId)}?invite=1`}
          className="inline-flex min-h-11 items-center text-accent underline underline-offset-4"
        >
          Invite who else was there
        </Link>
      </p>
    </div>
  );
}

function ClaimItem({ claim, incoming }: { claim: Claim; incoming: boolean }) {
  const headingId = useId();
  const queryClient = useQueryClient();
  const [confirmReject, setConfirmReject] = useState(false);
  // Accepted here, just now: the next step replaces the bare status.
  const [accepted, setAccepted] = useState(false);
  // Accept and Reject vanish with the decision, so focus moves to the
  // item's new status line instead of falling to the page body.
  const linkedLine = useRef<HTMLParagraphElement>(null);
  const statusLine = useRef<HTMLSpanElement>(null);
  const [focusStatus, setFocusStatus] = useState(false);
  useEffect(() => {
    if (!focusStatus) return;
    setFocusStatus(false);
    (linkedLine.current ?? statusLine.current)?.focus();
  }, [focusStatus]);
  const resolve = useMutation({
    mutationFn: (accept: boolean) => clients.lore.resolveClaim({ claimId: claim.id, accept }),
    onSuccess: async (res, accept) => {
      if (accept && res.status === ClaimStatus.ACCEPTED) setAccepted(true);
      // Accepting changes both stories' lore standing.
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: claimsKey }),
        invalidateAfterWrite(queryClient, claim.perspectiveId),
        invalidateAfterWrite(queryClient, claim.claimantPerspectiveId),
      ]);
      setFocusStatus(true);
    },
    onSettled: () => setConfirmReject(false),
  });
  const claimant = claim.claimant?.username ?? "someone";
  const author = claim.author?.username ?? "someone";
  const label = `“${claim.characterLabel}”`;
  const pending = claim.status === ClaimStatus.PENDING;

  return (
    <li aria-labelledby={headingId} className="grid min-w-0 gap-2 border-b border-rule py-4">
      <p id={headingId} className="font-serif text-prose [overflow-wrap:anywhere]">
        {incoming ? (
          <>
            <strong className="font-semibold">{claimant}</strong> says they are {label}
          </>
        ) : (
          <>
            You said you are {label} in <strong className="font-semibold">{author}</strong>'s perspective
          </>
        )}
      </p>
      {accepted && <Linked claim={claim} lineRef={linkedLine} />}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
        {!accepted && (
          <span ref={statusLine} tabIndex={-1}>
            <StatusLabel status={claim.status} />
          </span>
        )}
        {claim.createdAt && (
          <span className="text-sm text-ink-muted">{dateFormat.format(timestampDate(claim.createdAt))}</span>
        )}
        <Link to={`/perspectives/${encodeURIComponent(claim.perspectiveId)}`} className={linkClass}>
          {incoming ? "Your perspective" : `${author}'s perspective`}
        </Link>
        <Link to={`/perspectives/${encodeURIComponent(claim.claimantPerspectiveId)}`} className={linkClass}>
          {incoming ? "Their perspective" : "Your perspective"}
        </Link>
      </div>
      {incoming && pending && (
        <div className="flex flex-wrap gap-2">
          <Button size="sm" pending={resolve.isPending} onClick={() => resolve.mutate(true)}>
            Accept
          </Button>
          <Button size="sm" variant="danger" pending={resolve.isPending} onClick={() => setConfirmReject(true)}>
            Reject
          </Button>
        </div>
      )}
      {resolve.isError && (
        <p role="alert" className="text-sm font-medium text-error">
          {ConnectError.from(resolve.error).rawMessage}
        </p>
      )}
      <Dialog open={confirmReject} onOpenChange={setConfirmReject}>
        <DialogContent
          onCloseAutoFocus={(e) => {
            // The Reject button that opened it is gone once the claim is rejected.
            if (!resolve.isSuccess) return;
            e.preventDefault();
            (linkedLine.current ?? statusLine.current)?.focus();
          }}
        >
          <DialogTitle>Reject this claim?</DialogTitle>
          <DialogDescription className="[overflow-wrap:anywhere]">
            {claimant} will not be able to claim {label} again.
          </DialogDescription>
          <div className="mt-6 flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button variant="secondary">Cancel</Button>
            </DialogClose>
            <Button variant="danger" pending={resolve.isPending} onClick={() => resolve.mutate(false)}>
              Reject claim
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </li>
  );
}

function ClaimList({ direction }: { direction: ClaimDirection }) {
  const incoming = direction === ClaimDirection.INCOMING;
  const list = useInfiniteQuery({
    queryKey: [...claimsKey, direction],
    queryFn: ({ pageParam }) => clients.lore.listClaims({ direction, pageToken: pageParam }),
    initialPageParam: "",
    getNextPageParam: (last) => last.nextPageToken || undefined,
  });
  const claims = list.data?.pages.flatMap((p) => p.claims) ?? [];
  const more = useShowMore(claims.length, list.fetchNextPage);
  if (list.isError) {
    return (
      <p role="alert" className="font-medium text-error">
        Claims could not be loaded: {ConnectError.from(list.error).rawMessage}
      </p>
    );
  }
  if (!list.data) {
    return (
      <p role="status" className="text-ink-muted">
        Loading claims…
      </p>
    );
  }
  if (claims.length === 0) {
    return (
      <p className="text-ink-muted">
        {incoming
          ? "No one has claimed your characters yet."
          : "You have not claimed anyone's character yet. When you recognise yourself in another perspective in your arc, claim that character from it."}
      </p>
    );
  }
  return (
    <>
      <ul ref={more.list} aria-label={incoming ? "Claims on your characters" : "Your claims"}>
        {claims.map((c) => (
          <ClaimItem key={c.id} claim={c} incoming={incoming} />
        ))}
      </ul>
      {list.hasNextPage && (
        <div className="mt-4">
          <Button variant="secondary" pending={list.isFetchingNextPage} onClick={more.showMore}>
            Show more
          </Button>
        </div>
      )}
    </>
  );
}

/**
 * /claims, headed "Requests" like its navigation item: claims other writers
 * made on the caller's characters, which the caller accepts or rejects, and
 * the claims the caller made. Only usernames
 * appear here, never an email or a real name.
 */
export function Component() {
  const [tab, setTab] = useState("incoming");
  return (
    <section aria-labelledby="claims-title" className="mx-auto grid w-full max-w-measure min-w-0 gap-4">
      <h1 id="claims-title" className="text-2xl sm:text-3xl">
        Requests
      </h1>
      <p className="text-ink-muted">
        A request says “that character was me”. Accepting one links both perspectives into the same lore.
      </p>
      <Tabs value={tab} onValueChange={setTab}>
        <TabsList aria-label="Requests">
          <TabsTrigger value="incoming">On your characters</TabsTrigger>
          <TabsTrigger value="outgoing">Your claims</TabsTrigger>
        </TabsList>
        <TabsContent value="incoming">
          <ClaimList direction={ClaimDirection.INCOMING} />
        </TabsContent>
        <TabsContent value="outgoing">
          <ClaimList direction={ClaimDirection.OUTGOING} />
        </TabsContent>
      </Tabs>
    </section>
  );
}

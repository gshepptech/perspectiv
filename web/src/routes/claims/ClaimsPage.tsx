import { timestampDate } from "@bufbuild/protobuf/wkt";
import { ConnectError } from "@connectrpc/connect";
import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Check, CircleDashed, X } from "lucide-react";
import { useId, useState } from "react";
import { Link } from "react-router";

import { Button } from "@/components/ui/Button";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/Dialog";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/Tabs";
import { ClaimDirection, ClaimStatus } from "@/gen/perspectiv/v1/enums_pb";
import type { Claim } from "@/gen/perspectiv/v1/lore_pb";
import { clients } from "@/lib/connect";

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

function ClaimItem({ claim, incoming }: { claim: Claim; incoming: boolean }) {
  const headingId = useId();
  const queryClient = useQueryClient();
  const [confirmReject, setConfirmReject] = useState(false);
  const resolve = useMutation({
    mutationFn: (accept: boolean) => clients.lore.resolveClaim({ claimId: claim.id, accept }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: claimsKey }),
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
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
        <StatusLabel status={claim.status} />
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
          <Button size="sm" disabled={resolve.isPending} onClick={() => resolve.mutate(true)}>
            Accept
          </Button>
          <Button size="sm" variant="danger" disabled={resolve.isPending} onClick={() => setConfirmReject(true)}>
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
        <DialogContent>
          <DialogTitle>Reject this claim?</DialogTitle>
          <DialogDescription>
            {claimant} will not be able to claim {label} again.
          </DialogDescription>
          <div className="mt-6 flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button variant="secondary">Cancel</Button>
            </DialogClose>
            <Button variant="danger" disabled={resolve.isPending} onClick={() => resolve.mutate(false)}>
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
  const claims = list.data.pages.flatMap((p) => p.claims);
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
      <ul aria-label={incoming ? "Claims on your characters" : "Your claims"}>
        {claims.map((c) => (
          <ClaimItem key={c.id} claim={c} incoming={incoming} />
        ))}
      </ul>
      {list.hasNextPage && (
        <div className="mt-4">
          <Button variant="secondary" disabled={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>
            Show more
          </Button>
        </div>
      )}
    </>
  );
}

/**
 * /claims: claims other writers made on the caller's characters, which the
 * caller accepts or rejects, and the claims the caller made. Only usernames
 * appear here, never an email or a real name.
 */
export function Component() {
  const [tab, setTab] = useState("incoming");
  return (
    <section aria-labelledby="claims-title" className="mx-auto grid w-full max-w-measure min-w-0 gap-4">
      <h1 id="claims-title" className="text-2xl sm:text-3xl">
        Claims
      </h1>
      <p className="text-ink-muted">
        A claim says “that character was me”. Accepting one links both perspectives into the same lore.
      </p>
      <Tabs value={tab} onValueChange={setTab}>
        <TabsList aria-label="Claims">
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

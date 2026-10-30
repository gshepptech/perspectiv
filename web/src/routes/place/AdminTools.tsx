import { ConnectError } from "@connectrpc/connect";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useId, useRef, useState } from "react";
import { useNavigate } from "react-router";

import { placeChain, storiesLabel } from "@/components/reader/format";
import { Button } from "@/components/ui/Button";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogTitle, DialogTrigger } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { Textarea } from "@/components/ui/Textarea";
import { PlaceKind, Role } from "@/gen/perspectiv/v1/enums_pb";
import type { Place } from "@/gen/perspectiv/v1/place_pb";
import { clients } from "@/lib/connect";
import { useSession } from "@/lib/session";
import { ReasonDialog } from "@/routes/admin/ReasonDialog";
import { errorMessage } from "@/routes/auth/errors";
import { useDebounced } from "@/routes/compose/search";

/**
 * Merging a duplicate: the admin finds the place everyone should use, picks
 * it, and gives a reason. Only a duplicate with no stories can be merged;
 * the server says so otherwise.
 */
function MergeDialog({ place, onMerged }: { place: Place; onMerged: (into: Place) => void }) {
  const [open, setOpen] = useState(false);
  const [text, setText] = useState(place.name);
  const [into, setInto] = useState<Place | null>(null);
  const [reason, setReason] = useState("");
  const [missing, setMissing] = useState("");
  const query = useDebounced(text.trim(), 250);
  const reasonId = useId();
  const found = useQuery({
    queryKey: ["place", "searchPlaces", query],
    queryFn: ({ signal }) => clients.place.searchPlaces({ query }, { signal }),
    enabled: open && query.length > 0,
  });
  const merge = useMutation({
    mutationFn: (target: Place) => clients.moderation.mergePlace({ placeId: place.id, intoPlaceId: target.id, reason: reason.trim() }),
    onSuccess: (_, target) => {
      setOpen(false);
      onMerged(target);
    },
  });
  const options = (found.data?.places ?? []).filter((p) => p.id !== place.id);
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (next) {
          setInto(null);
          setReason("");
          setMissing("");
          merge.reset();
        }
      }}
    >
      <DialogTrigger asChild>
        <Button variant="secondary" size="sm" className="min-h-11 justify-self-start">
          Merge into another place…
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogTitle>Merge this duplicate</DialogTitle>
        <DialogDescription className="[overflow-wrap:anywhere]">
          “{place.name}” is hidden and the places filed under it move to the one you pick. It must have no stories of its own.
        </DialogDescription>
        <form
          noValidate
          className="mt-4 grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (!into) return setMissing("Pick the place everyone should use.");
            if (!reason.trim()) return setMissing("Say why. The reason is written to the audit log.");
            setMissing("");
            merge.mutate(into);
          }}
        >
          <label className="grid gap-1 text-sm font-medium">
            Find the place to keep
            <Input value={text} onChange={(e) => setText(e.target.value)} autoComplete="off" />
          </label>
          <fieldset className="grid max-h-60 gap-1 overflow-y-auto">
            <legend className="sr-only">Place to keep</legend>
            {options.map((p) => (
              <label key={p.id} className="flex min-h-11 cursor-pointer items-start gap-3 py-1 text-base">
                <input
                  type="radio"
                  name="into"
                  className="mt-1 size-5 shrink-0 accent-accent"
                  checked={into?.id === p.id}
                  onChange={() => setInto(p)}
                />
                <span className="min-w-0 [overflow-wrap:anywhere]">
                  {p.name}
                  <span className="block text-sm text-ink-muted">
                    {placeChain(p.kind, p.ancestors, p.category)} · {storiesLabel(p.stories)}
                  </span>
                </span>
              </label>
            ))}
            {found.isSuccess && options.length === 0 && <p className="text-sm text-ink-muted">No other place matches.</p>}
          </fieldset>
          <label htmlFor={reasonId} className="text-sm font-medium">
            Reason for the audit log
          </label>
          <Textarea id={reasonId} value={reason} onChange={(e) => setReason(e.target.value)} rows={3} className="min-h-20 font-sans text-base" />
          {(missing || merge.isError) && (
            <p role="alert" className="text-sm font-medium text-error">
              {missing || (merge.error instanceof ConnectError ? errorMessage(merge.error) : merge.error?.message)}
            </p>
          )}
          <div className="flex flex-wrap justify-end gap-2">
            <Button type="submit" pending={merge.isPending}>
              Merge
            </Button>
            <DialogClose asChild>
              <Button variant="secondary">Cancel</Button>
            </DialogClose>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/**
 * Admin-only tidying of the place hierarchy, beside the place itself: make
 * an imported store a venue that can hold places, and merge a duplicate
 * into its twin. Both are audited on the server.
 */
export function AdminTools({ place }: { place: Place }) {
  const session = useSession();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const status = useRef<HTMLParagraphElement>(null);
  const [note, setNote] = useState<{ text: string; seq: number } | null>(null);
  useEffect(() => {
    if (note) status.current?.focus();
  }, [note]);
  if (session?.role !== Role.ADMIN) return null;
  return (
    <section aria-labelledby="place-admin" className="grid gap-3 rounded-md border border-dashed border-rule p-4">
      <h2 id="place-admin" className="text-lg">
        Admin
      </h2>
      <p ref={status} tabIndex={-1} role="status" aria-label="Admin status" className="text-sm text-success empty:hidden">
        {note?.text}
      </p>
      {place.kind === PlaceKind.STORE && (
        <ReasonDialog
          trigger="Make this a venue…"
          title={`Make ${place.name} a venue?`}
          description="Members can then add places inside it, like a stadium's stands or a park's rides. Its stories stay where they are."
          confirm="Make a venue"
          act={(reason) => clients.moderation.makeVenue({ placeId: place.id, reason })}
          onDone={() => {
            setNote((n) => ({ text: `${place.name} is a venue now.`, seq: (n?.seq ?? 0) + 1 }));
            void queryClient.invalidateQueries({ queryKey: ["place", "getPlace"] });
          }}
        />
      )}
      <MergeDialog place={place} onMerged={(into) => void navigate(`/places/${into.id}`, { state: { mergedFrom: place.name } })} />
    </section>
  );
}

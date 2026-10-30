import { Code, ConnectError } from "@connectrpc/connect";
import { useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { Link, useLocation, useSearchParams } from "react-router";

import type { Place } from "@/gen/perspectiv/v1/place_pb";
import type { Perspective } from "@/gen/perspectiv/v1/perspective_pb";
import { clients } from "@/lib/connect";
import { useDocumentTitle } from "@/lib/title";

import { blankValues, valuesFromPerspective } from "./composeForm";
import { Composer } from "./Composer";
import { rememberAfterPublish } from "./links";
import { isLocalDate, parseHour, todayIn } from "./localDate";
import { PlacePicker } from "./PlacePicker";

const EXISTING_NOTICE =
  "You already published a perspective here, so it is open for editing. Each person writes one perspective per arc.";

// Composer data is read fresh on every visit: initial form values must never
// come from a cached copy that predates the author's last save.
const fresh = { staleTime: 0, gcTime: 0, retry: false } as const;

function Loading() {
  return (
    <p role="status" className="text-ink-muted">
      Opening the composer…
    </p>
  );
}

function LoadFailed({ error }: { error: unknown }) {
  const e = ConnectError.from(error);
  const text =
    e.code === Code.NotFound
      ? "We could not find that place or perspective. It may have been removed or hidden."
      : e.code === Code.PermissionDenied
        ? "Only the author can edit this perspective."
        : `The composer could not open: ${e.rawMessage}`;
  return (
    <section className="mx-auto grid max-w-measure gap-4">
      <h1 className="text-2xl sm:text-3xl">Write a perspective</h1>
      <p role="alert" className="font-medium text-error">
        {text}
      </p>
      <p>
        <Link to="/compose" className="text-accent underline underline-offset-4">
          Start from a place
        </Link>
      </p>
    </section>
  );
}

type Shared = { notice?: string; onDraftNamed: (id: string) => void; joining?: boolean };

function placeToday(place: Place): string {
  // A place always carries its IANA zone; an empty one falls back to the
  // device's zone rather than failing to open.
  return todayIn(place.timezone || Intl.DateTimeFormat().resolvedOptions().timeZone);
}

function NewForPlace({ placeId, date, hour, ...shared }: Shared & { placeId: string; date?: string; hour?: number }) {
  const place = useQuery({
    queryKey: ["place", "getPlace", placeId],
    queryFn: () => clients.place.getPlace({ placeId }),
    ...fresh,
  });
  if (place.isError) return <LoadFailed error={place.error} />;
  if (!place.data?.place) return <Loading />;
  const p = place.data.place;
  const today = placeToday(p);
  return <Composer place={p} today={today} initial={blankValues(date ?? today, hour)} {...shared} />;
}

function Existing({ perspectiveId, ...shared }: Shared & { perspectiveId: string }) {
  const perspective = useQuery({
    queryKey: ["perspective", "get", perspectiveId],
    queryFn: () => clients.perspective.get({ perspectiveId }),
    ...fresh,
  });
  const placeId = perspective.data?.perspective?.place?.id ?? "";
  const place = useQuery({
    queryKey: ["place", "getPlace", placeId],
    queryFn: () => clients.place.getPlace({ placeId }),
    enabled: placeId !== "",
    ...fresh,
  });
  if (perspective.isError) return <LoadFailed error={perspective.error} />;
  if (place.isError) return <LoadFailed error={place.error} />;
  const p: Perspective | undefined = perspective.data?.perspective;
  if (p && (p.deleted || p.hidden)) {
    return <LoadFailed error={new ConnectError("removed", Code.NotFound)} />;
  }
  if (!p || !place.data?.place) return <Loading />;
  const today = placeToday(place.data.place);
  return (
    <Composer
      place={place.data.place}
      today={today}
      initial={valuesFromPerspective(p, today)}
      perspective={p}
      {...shared}
    />
  );
}

/**
 * /compose. Search parameters: perspectiveId opens a saved draft or a
 * published perspective; otherwise placeId opens a new perspective there,
 * starting from date (YYYY-MM-DD, default today in the place's time zone) and
 * hour (0 to 23, default the whole day); with neither, the author first picks
 * a place. add (a name, possibly empty) and in (a place id) open the picker's
 * add form at once, prefilled: search results and place pages link here when
 * the place someone was at is not listed. return (a same-site path) is where
 * the writer goes once the new perspective is published: the story they
 * wanted to claim a character in.
 */
export function Component() {
  useDocumentTitle("Write a perspective");
  const [params, setParams] = useSearchParams();
  const location = useLocation();
  const perspectiveId = params.get("perspectiveId") ?? "";
  const placeId = params.get("placeId") ?? "";
  const dateParam = params.get("date");
  const hour = parseHour(params.get("hour"));
  const back = params.get("return");

  // The story page acts on this right after publishing, so it has to
  // outlive this page; it only applies to a perspective at this place, day and hour.
  useEffect(() => {
    if (back && placeId && isLocalDate(dateParam)) rememberAfterPublish({ placeId, localDate: dateParam, hour }, back);
  }, [back, placeId, dateParam, hour]);

  // When autosave names a new draft, its id goes into the address so a reload
  // reopens it. That address change must not reload the form the author is
  // typing in, so the composer instance stays keyed by what opened it.
  const [adopted, setAdopted] = useState<{ id: string; key: string } | null>(null);
  const opened = perspectiveId ? `perspective:${perspectiveId}` : placeId ? `place:${placeId}` : "pick";
  const key = adopted && adopted.id === perspectiveId ? adopted.key : opened;

  const notice = (location.state as { notice?: string } | null)?.notice === "existing" ? EXISTING_NOTICE : undefined;
  const shared: Shared = {
    notice,
    joining: Boolean(back),
    onDraftNamed: (id) => {
      setAdopted({ id, key });
      setParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          next.set("perspectiveId", id);
          return next;
        },
        { replace: true },
      );
    },
  };

  if (key.startsWith("perspective:")) {
    return <Existing key={key} perspectiveId={key.slice("perspective:".length)} {...shared} />;
  }
  if (key.startsWith("place:")) {
    return (
      <NewForPlace
        key={key}
        placeId={key.slice("place:".length)}
        date={isLocalDate(dateParam) ? dateParam : undefined}
        hour={hour}
        {...shared}
      />
    );
  }
  return (
    <PlacePicker
      addName={params.get("add") ?? (params.has("in") ? "" : undefined)}
      addIn={params.get("in") ?? ""}
      onPick={(place) =>
        setParams((prev) => {
          const next = new URLSearchParams(prev);
          next.set("placeId", place.id);
          next.delete("add");
          next.delete("in");
          return next;
        })
      }
    />
  );
}

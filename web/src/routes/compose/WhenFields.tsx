import { useQuery } from "@tanstack/react-query";
import { useWatch, type Control } from "react-hook-form";

import { formatDay } from "@/components/reader/format";
import { FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/Form";
import { Input } from "@/components/ui/Input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/Select";
import type { Place } from "@/gen/perspectiv/v1/place_pb";
import { clients } from "@/lib/connect";

import { DAY, hourOf, type ComposeValues } from "./composeForm";
import { hourLabel, isLocalDate } from "./localDate";
import { RecentDays } from "./RecentDays";

const hours = Array.from({ length: 24 }, (_, h) => h);

/**
 * What already exists for the place on the chosen day, so a writer joins
 * the people who were there: the day's title and how many stories it and
 * each of its hours hold. Stories link only within one arc, so the note says
 * where the others are.
 */
function DayArcNote({ place, localDate, hour }: { place: Place; localDate: string; hour: number | undefined }) {
  const valid = isLocalDate(localDate);
  const day = useDayArc(place, localDate);
  if (!valid || !day.data) return null;
  const d = day.data;
  if (!d.arcId) {
    return <p className="text-sm text-ink-muted sm:col-span-2">Nobody has written about {place.name} on this day yet. You start it, so give the day a title.</p>;
  }
  const stories = (n: number) => (n === 1 ? "1 story" : `${n} stories`);
  const hours = d.hours.filter((h) => h.perspectiveCount > 0);
  const atHour = hour === undefined ? undefined : d.hours.find((h) => h.hour === hour);
  let where: string;
  if (hour === undefined) {
    where = "Writing for the whole day puts your story with theirs, where you can find each other.";
  } else if (atHour) {
    where = `You join ${hourLabel(hour)}, “${atHour.title}” (${stories(atHour.perspectiveCount)}). Stories link only within one hour or the whole day.`;
  } else {
    where = `Nobody has written for ${hourLabel(hour)} yet. Stories link only within one hour or the whole day, so choose an hour only if your story belongs to it.`;
  }
  return (
    <div className="grid gap-1 rounded-md border border-rule p-3 text-sm sm:col-span-2" aria-live="polite">
      <p>
        This day is <span className="font-serif italic">“{d.title}”</span>: {stories(d.perspectiveCount)} for the whole day
        {hours.length > 0 && <>, {hours.map((h) => `${stories(h.perspectiveCount)} at ${hourLabel(h.hour)}`).join(", ")}</>}.
      </p>
      <p className="text-ink-muted">{where}</p>
    </div>
  );
}

/** What exists for the place on a day, shared by the note and the publish check. */
export function useDayArc(place: Place, localDate: string) {
  return useQuery({
    queryKey: ["arc", "findDayArc", { placeId: place.id, localDate }],
    queryFn: ({ signal }) => clients.arc.findDayArc({ placeId: place.id, localDate }, { signal }),
    enabled: isLocalDate(localDate),
  });
}

export function WhenFields({
  control,
  place,
  today,
  fixed,
  onChooseDay,
}: {
  control: Control<ComposeValues>;
  place: Place;
  today: string;
  fixed: boolean;
  onChooseDay: (localDate: string, hour: number | undefined) => void;
}) {
  const localDate = useWatch({ control, name: "localDate" }) ?? "";
  const hour = hourOf({ hour: useWatch({ control, name: "hour" }) ?? DAY });
  const day = useDayArc(place, localDate);
  // A day that already has an arc keeps its title and the server ignores a
  // new one, so the title field shows only for a day that has none.
  const dayTitled = Boolean(day.data?.arcId);
  if (fixed) {
    // Joining someone's story: the day and hour are theirs, shown as text
    // rather than as controls that look editable but are not.
    return (
      <div className="grid min-w-0 gap-4 sm:grid-cols-2">
        <p className="sm:col-span-2">
          <span className="text-sm font-medium">When</span>
          <br />
          {isLocalDate(localDate) ? formatDay(localDate) : localDate}, {hour === undefined ? "the whole day" : hourLabel(hour)}
          <span className="block text-sm text-ink-muted">Set to match their story: stories link only within the same day or hour.</span>
        </p>
        <DayArcNote place={place} localDate={localDate} hour={hour} />
      </div>
    );
  }
  return (
    <div className="grid min-w-0 gap-4 sm:grid-cols-2">
      <FormField
        control={control}
        name="localDate"
        render={({ field }) => (
          // content-start: a longer help line beside it never stretches this field (Folio, Composer: day and hour).
          <FormItem className="min-w-0 content-start">
            <FormLabel>Day of your visit</FormLabel>
            <FormControl>
              <Input type="date" max={today} required {...field} />
            </FormControl>
            <FormDescription className="[overflow-wrap:anywhere]">
              Today at {place.name} is {formatDay(today)}.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={control}
        name="hour"
        render={({ field }) => (
          <FormItem className="min-w-0 content-start">
            <FormLabel>Hour</FormLabel>
            <Select value={field.value} onValueChange={field.onChange}>
              <FormControl>
                <SelectTrigger onBlur={field.onBlur} className="h-11">
                  <SelectValue />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                <SelectItem value={DAY}>Whole day</SelectItem>
                {hours.map((h) => (
                  <SelectItem key={h} value={String(h)}>
                    {hourLabel(h)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FormDescription>Optional, in local time. Fixed once published.</FormDescription>
          </FormItem>
        )}
      />
      <RecentDays place={place} today={today} localDate={localDate} hour={hour} onChoose={onChooseDay} />
      <DayArcNote place={place} localDate={localDate} hour={hour} />
      {!dayTitled && (
      <FormField
        control={control}
        name="title"
        render={({ field }) => (
          <FormItem className="sm:col-span-2">
            <FormLabel>Title of the day</FormLabel>
            <FormControl>
              <Input {...field} autoComplete="off" />
            </FormControl>
            <FormDescription>
              You are the first to write about this place on this day: 3 to 80 characters, more than the place name
              or the date.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
      )}
    </div>
  );
}

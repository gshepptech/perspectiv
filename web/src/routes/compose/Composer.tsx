import { Code, ConnectError } from "@connectrpc/connect";
import { zodResolver } from "@hookform/resolvers/zod";
import { Info, Plus, Trash2 } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useFieldArray, useForm, useWatch, type Control } from "react-hook-form";
import { Link, useNavigate, useSearchParams } from "react-router";

import { Button } from "@/components/ui/Button";
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/Form";
import { Input } from "@/components/ui/Input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/Select";
import { Textarea } from "@/components/ui/Textarea";
import { PerspectiveStatus } from "@/gen/perspectiv/v1/enums_pb";
import type { Place } from "@/gen/perspectiv/v1/place_pb";
import type { Perspective } from "@/gen/perspectiv/v1/perspective_pb";
import { clients } from "@/lib/connect";

import { useAutosave, type AutosaveState } from "./autosave";
import {
  characterInputs,
  DAY,
  hasText,
  hourOf,
  MAX_CHARACTERS,
  MAX_PARTS,
  MAX_WORDS_PER_PART,
  publishSchema,
  valuesFromPerspective,
  wordCount,
  type ComposeValues,
} from "./composeForm";
import { resumedNotice, useDraftsFor } from "./drafts";
import { EmptyDraft } from "./EmptyDraft";
import { genreLabel, genres } from "./genres";
import { hourLabel } from "./localDate";
import { useDayArc, WhenFields } from "./WhenFields";

function describe(err: unknown): string {
  return ConnectError.from(err).rawMessage;
}

export type ComposerProps = {
  place: Place;
  /** Today in the place's time zone; the latest day the picker offers. */
  today: string;
  initial: ComposeValues;
  /** The saved draft or published perspective being edited; absent for a new one. */
  perspective?: Perspective;
  notice?: string;
  /** The notice takes focus on open: the control that had it is gone (an emptied draft was deleted). */
  focusNotice?: boolean;
  /** Called once the first autosave gives a new draft its id. */
  onDraftNamed: (perspectiveId: string) => void;
  /** Called once an emptied draft is deleted, with where to start again. */
  onDraftDeleted: (where: { placeId: string; localDate: string; hour: number | undefined }) => void | Promise<void>;
  /**
   * Writing their side of someone else's story ("That was me"): the day and
   * hour are that story's, fixed, because claims link stories in one arc.
   */
  joining?: boolean;
};

/**
 * The writing surface. A draft autosaves through SaveDraft and is published
 * explicitly; a published perspective is edited in place through Update, with
 * its day and hour fixed.
 */
export function Composer({
  place,
  today,
  initial,
  perspective,
  notice,
  focusNotice = false,
  onDraftNamed,
  onDraftDeleted,
  joining = false,
}: ComposerProps) {
  const navigate = useNavigate();
  // An invite link sends its reader here with the inviter's code; publishing
  // with it tells the inviter, and lands on the arc, where the two stories sit
  // side by side and characters can be claimed.
  const [searchParams] = useSearchParams();
  const inviteCode = searchParams.get("invite") ?? "";
  const published = perspective?.status === PerspectiveStatus.PUBLISHED;
  const form = useForm<ComposeValues>({
    defaultValues: initial,
    resolver: zodResolver(useMemo(() => publishSchema(today), [today])),
  });
  const parts = useFieldArray({ control: form.control, name: "parts" });
  // A writer has one story per arc: when the chosen day or hour already
  // holds theirs, publishing another would be refused and its text lost, so
  // the composer says so first and offers that story instead.
  const chosenDate = useWatch({ control: form.control, name: "localDate" }) ?? "";
  const chosenHour = hourOf({ hour: useWatch({ control: form.control, name: "hour" }) ?? DAY });
  const dayArc = useDayArc(place, chosenDate);
  const mineInArc =
    chosenHour === undefined
      ? dayArc.data?.minePerspectiveId
      : dayArc.data?.hours.find((h) => h.hour === chosenHour)?.minePerspectiveId;
  const alreadyWrote = !published && mineInArc && mineInArc !== perspective?.id ? mineInArc : "";
  // Arriving from "Write your side", the page is new to the writer: the
  // heading takes focus so the note under it is read next.
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    if (joining) heading.current?.focus();
  }, [joining]);
  const characters = useFieldArray({ control: form.control, name: "characters", keyName: "key" });
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [shownNotice, setShownNotice] = useState(notice);
  const noticeRef = useRef<HTMLParagraphElement>(null);
  useEffect(() => {
    if (focusNotice) noticeRef.current?.focus();
  }, [focusNotice]);
  const written = hasText(useWatch({ control: form.control, name: "parts" }));

  // The draft this form saves into: a ref for the autosave, state for what
  // the page shows.
  const draftId = useRef(perspective?.id ?? "");
  const [ownDraft, setOwnDraft] = useState(perspective?.id ?? "");
  const namedRef = useRef(onDraftNamed);
  namedRef.current = onDraftNamed;
  // A save that finishes after the composer has gone (the writer followed a
  // link away) must not name its draft in an address that has moved on.
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  // Set while a found draft is loaded into the form, so loading it is not
  // taken for an edit to save.
  const adopting = useRef(false);

  const autosave = useAutosave(async () => {
    const v = form.getValues();
    const res = await clients.perspective.saveDraft({
      perspectiveId: draftId.current,
      placeId: place.id,
      localDate: v.localDate,
      hour: hourOf(v),
      titleIfNewArc: v.title.trim(),
      genre: v.genre,
      parts: v.parts.map((p) => p.body),
      characters: characterInputs(v),
    });
    const id = res.perspective?.id;
    if (id && id !== draftId.current) {
      draftId.current = id;
      if (alive.current) {
        setOwnDraft(id);
        namedRef.current(id);
      }
    }
  }, describe);

  // Every edit to a draft restarts the autosave debounce. A published
  // perspective is only written when the author saves it. A draft with no
  // text in any part is never saved (the server refuses one too): an empty
  // draft beside a real one for the same day would be the newest, and so the
  // one that reopens.
  const { schedule, cancel, setState: setSaveState } = autosave;
  useEffect(() => {
    if (published) return;
    const sub = form.watch((v) => {
      if (adopting.current) return;
      if (hasText(v.parts)) {
        schedule();
      } else {
        cancel();
        setSaveState({ kind: "idle" });
      }
    });
    return () => sub.unsubscribe();
  }, [form, published, schedule, cancel, setSaveState]);

  // The writer's drafts for the day and hour in the form, looked up again
  // whenever either changes, not only on load. A form with nothing written
  // and no draft of its own opens the newest one in place; otherwise the
  // page offers it (otherDraft below) and replaces nothing.
  const drafts = useDraftsFor(place.id, chosenDate, chosenHour, !published);
  const newestDraft = drafts.data?.[0];
  useEffect(() => {
    const found = drafts.data;
    const newest = found?.[0];
    if (!found || !newest || draftId.current !== "" || hasText(form.getValues().parts)) return;
    adopting.current = true;
    form.reset(valuesFromPerspective(newest, today));
    adopting.current = false;
    draftId.current = newest.id;
    setOwnDraft(newest.id);
    setShownNotice(resumedNotice(found.length));
    namedRef.current(newest.id);
  }, [drafts.data, form, today]);
  const otherDraft = newestDraft && newestDraft.id !== ownDraft && (ownDraft !== "" || written) ? newestDraft : undefined;

  // A saved draft whose parts are all cleared still holds its old text on
  // the server (autosave waits for text), so the writer is asked whether to
  // delete it. "Keep writing" puts the question away until a part has had
  // text and is emptied again.
  const [emptyKept, setEmptyKept] = useState(false);
  useEffect(() => {
    if (written) setEmptyKept(false);
  }, [written]);
  const emptied = !published && ownDraft !== "" && !written && !emptyKept;

  async function openExisting(v: ComposeValues): Promise<boolean> {
    const hour = hourOf(v);
    const res = await clients.perspective.listMine({
      status: PerspectiveStatus.PUBLISHED,
      placeId: place.id,
      localDate: v.localDate,
      hour,
    });
    // Match the arc exactly: the day arc and each hour arc are separate arcs.
    const mine = res.perspectives.find(
      (p) => p.place?.id === place.id && p.localDate === v.localDate && p.hour === hour,
    );
    if (!mine) return false;
    await navigate(`/compose?perspectiveId=${encodeURIComponent(mine.id)}`, {
      state: { notice: "existing" },
    });
    return true;
  }

  const publish = form.handleSubmit(async (v) => {
    setError(null);
    setBusy(true);
    try {
      await autosave.flush();
      const res = await clients.perspective.publish({
        perspectiveId: draftId.current,
        placeId: place.id,
        localDate: v.localDate,
        hour: hourOf(v),
        titleIfNewArc: v.title.trim(),
        genre: v.genre,
        parts: v.parts.map((p) => p.body),
        characters: characterInputs(v),
        inviteCode,
      });
      autosave.cancel();
      if (inviteCode) {
        await navigate(`/arcs/${encodeURIComponent(res.arcId)}`, { state: { invited: true } });
      } else {
        await navigate(`/perspectives/${encodeURIComponent(res.perspectiveId)}`, { state: { justPublished: true } });
      }
    } catch (err) {
      // One perspective per person per arc: a second publish opens the first
      // one for editing instead of leaving the author on a dead end.
      if (ConnectError.from(err).code === Code.AlreadyExists) {
        try {
          if (await openExisting(v)) return;
        } catch (lookup) {
          setError(describe(lookup));
          return;
        }
      }
      setError(describe(err));
    } finally {
      setBusy(false);
    }
  });

  const saveChanges = form.handleSubmit(async (v) => {
    if (!perspective) return;
    setError(null);
    setBusy(true);
    autosave.setState({ kind: "saving" });
    try {
      await clients.perspective.update({
        perspectiveId: perspective.id,
        genre: v.genre,
        parts: v.parts.map((p) => p.body),
        characters: characterInputs(v),
      });
      autosave.setState({ kind: "saved" });
    } catch (err) {
      autosave.setState({ kind: "failed", message: describe(err) });
      setError(describe(err));
    } finally {
      setBusy(false);
    }
  });

  return (
    <Form {...form}>
      <form
        noValidate
        onSubmit={published ? saveChanges : publish}
        aria-labelledby="compose-title"
        className="mx-auto grid w-full max-w-measure min-w-0 gap-6"
      >
        <header className="grid gap-1">
          <h1 id="compose-title" ref={heading} tabIndex={-1} className="text-2xl focus:outline-none sm:text-3xl">
            {published ? "Edit your story" : "Write a story"}
          </h1>
          <p className="text-ink-muted [overflow-wrap:anywhere]">
            {place.name}
            {place.ancestors.length > 0 && <>, {place.ancestors.map((a) => a.name).reverse().join(", ")}</>}
          </p>
        </header>

        {joining && !published && (
          <p className="flex gap-2 rounded-md border border-rule bg-surface p-3 text-sm text-ink">
            <Info aria-hidden size={16} className="mt-0.5 shrink-0 text-info" />
            <span>
              Write your side of this day. Once it is published you go back to their story to say which character was
              you, and your stories link when they accept.
            </span>
          </p>
        )}

        {/* A live region from the start, so a draft opened in place as the day changes is announced.
            Empty, it takes back the grid gap after it rather than leave a double space. */}
        <div role="status" aria-label="Composer notice" className="empty:-mb-6">
          {shownNotice && (
            <p ref={noticeRef} tabIndex={-1} className="flex gap-2 rounded-md border border-rule bg-surface p-3 text-sm text-ink">
              <Info aria-hidden size={16} className="mt-0.5 shrink-0 text-info" />
              <span>{shownNotice}</span>
            </p>
          )}
        </div>

        {published ? (
          <p className="text-sm text-ink-muted [overflow-wrap:anywhere]">
            Filed under {perspective?.localDate}, {perspective?.hour === undefined ? "whole day" : hourLabel(perspective.hour)}{" "}
            at {place.name}. The day and hour are fixed once published.
          </p>
        ) : (
          <WhenFields
            control={form.control}
            place={place}
            today={today}
            fixed={joining}
            onChooseDay={(localDate, hour) => {
              form.setValue("localDate", localDate, { shouldDirty: true });
              form.setValue("hour", hour === undefined ? DAY : String(hour), { shouldDirty: true });
            }}
          />
        )}

        {otherDraft && (
          <p role="status" className="rounded-md border border-warning p-3 text-sm">
            You already have a draft for {chosenHour === undefined ? "this day" : "this hour"}.{" "}
            <Link
              to={`/compose?perspectiveId=${encodeURIComponent(otherDraft.id)}`}
              className="inline-flex min-h-11 items-center font-medium text-accent underline underline-offset-4"
            >
              Open that draft
            </Link>
          </p>
        )}

        {alreadyWrote && (
          <p role="alert" className="rounded-md border border-warning p-3 text-sm">
            You already have a story for {chosenHour === undefined ? "this day" : hourLabel(chosenHour)} here, and each
            person has one story per day or hour.{" "}
            <Link to={`/compose?perspectiveId=${encodeURIComponent(alreadyWrote)}`} className="font-medium text-accent underline underline-offset-4">
              Edit yours
            </Link>{" "}
            instead, or pick another hour.
          </p>
        )}


        <FormField
          control={form.control}
          name="genre"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Genre</FormLabel>
              <Select
                value={field.value ? String(field.value) : ""}
                onValueChange={(v) => field.onChange(Number(v))}
              >
                <FormControl>
                  <SelectTrigger onBlur={field.onBlur}>
                    <SelectValue placeholder="Choose…" />
                  </SelectTrigger>
                </FormControl>
                <SelectContent>
                  {genres.map((g) => (
                    <SelectItem key={g} value={String(g)}>
                      {genreLabel(g)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <FormMessage />
            </FormItem>
          )}
        />

        <fieldset className="grid min-w-0 gap-4">
          <legend className="mb-2 font-serif text-xl font-semibold">Parts</legend>
          <p className="text-sm text-ink-muted">
            Up to {MAX_PARTS} parts of up to {MAX_WORDS_PER_PART} words. Each part reads as one page.
          </p>
          {parts.fields.map((item, i) => (
            <PartField
              key={item.id}
              control={form.control}
              index={i}
              onRemove={parts.fields.length > 1 ? () => parts.remove(i) : undefined}
            />
          ))}
          {/* Polite and always mounted, so the question is read as it appears while focus stays in the
              part being cleared. Empty, it takes back the grid gap after it. */}
          <div aria-live="polite" className="empty:-mb-4">
            {emptied && (
              <EmptyDraft
                perspectiveId={ownDraft}
                settle={autosave.flush}
                onKeepWriting={() => {
                  setEmptyKept(true);
                  form.setFocus("parts.0.body");
                }}
                onDeleted={() => {
                  const v = form.getValues();
                  return onDraftDeleted({ placeId: place.id, localDate: v.localDate, hour: hourOf(v) });
                }}
              />
            )}
          </div>
          <div>
            <Button
              variant="secondary"
              size="sm"
              disabled={parts.fields.length >= MAX_PARTS}
              onClick={() => parts.append({ body: "" })}
            >
              <Plus aria-hidden size={16} /> Add part
            </Button>
          </div>
        </fieldset>

        <fieldset className="grid min-w-0 gap-4">
          <legend className="mb-2 font-serif text-xl font-semibold">Characters</legend>
          <p className="text-sm text-ink-muted">
            Describe the people in your story without naming them, like “the clerk with the green scarf”. Another
            writer on this day can claim to be one of them. Up to {MAX_CHARACTERS}.
          </p>
          {characters.fields.map((item, i) => (
            <FormField
              key={item.key}
              control={form.control}
              name={`characters.${i}.label`}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Character {i + 1}</FormLabel>
                  <div className="flex min-w-0 gap-2">
                    <FormControl>
                      <Input {...field} autoComplete="off" />
                    </FormControl>
                    <Button
                      variant="ghost"
                      className="shrink-0 px-3"
                      aria-label={`Remove character ${i + 1}`}
                      disabled={item.claimed}
                      onClick={() => characters.remove(i)}
                    >
                      <Trash2 aria-hidden size={16} />
                    </Button>
                  </div>
                  {item.claimed && (
                    <FormDescription>Claimed by another writer, so it stays. You can still reword it.</FormDescription>
                  )}
                  <FormMessage />
                </FormItem>
              )}
            />
          ))}
          <div>
            <Button
              variant="secondary"
              size="sm"
              disabled={characters.fields.length >= MAX_CHARACTERS}
              onClick={() => characters.append({ id: "", label: "", claimed: false })}
            >
              <Plus aria-hidden size={16} /> Add character
            </Button>
          </div>
        </fieldset>

        {error && (
          <p role="alert" className="rounded-md border border-error p-3 text-sm font-medium text-error">
            {error}
          </p>
        )}

        <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
          <Button type="submit" pending={busy} disabled={Boolean(alreadyWrote)}>
            {published ? "Save changes" : "Publish"}
          </Button>
          {alreadyWrote && <span className="text-sm text-ink-muted">Held: you already have a story for this day or hour.</span>}
          <SaveStatus state={autosave.state} published={published} written={written} />
        </div>
      </form>
    </Form>
  );
}

function PartField({ control, index, onRemove }: { control: Control<ComposeValues>; index: number; onRemove?: () => void }) {
  const body = useWatch({ control, name: `parts.${index}.body` }) ?? "";
  const words = wordCount(body);
  const over = words > MAX_WORDS_PER_PART;
  return (
    <FormField
      control={control}
      name={`parts.${index}.body`}
      render={({ field }) => (
        <FormItem>
          <div className="flex min-w-0 items-center justify-between gap-2">
            <FormLabel>Part {index + 1}</FormLabel>
            {onRemove && (
              <Button variant="ghost" size="sm" aria-label={`Remove part ${index + 1}`} onClick={onRemove}>
                <Trash2 aria-hidden size={16} />
              </Button>
            )}
          </div>
          <FormControl>
            <Textarea {...field} rows={8} />
          </FormControl>
          <FormDescription className={over ? "font-medium text-error" : undefined}>
            {words} / {MAX_WORDS_PER_PART} words
          </FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  );
}

function SaveStatus({ state, published, written }: { state: AutosaveState; published: boolean; written: boolean }) {
  let text = "";
  switch (state.kind) {
    case "idle":
      text = published || written ? "" : "Your draft saves once a part has text.";
      break;
    case "pending":
      text = "Unsaved changes";
      break;
    case "saving":
      text = "Saving…";
      break;
    case "saved":
      text = published ? "Changes saved" : "Draft saved";
      break;
    case "failed":
      text = `Not saved: ${state.message}`;
      break;
  }
  return (
    <p
      role="status"
      aria-label="Save status"
      className={state.kind === "failed" ? "text-sm font-medium text-error" : "text-sm text-ink-muted"}
    >
      {text}
    </p>
  );
}

import { Code, ConnectError } from "@connectrpc/connect";
import { zodResolver } from "@hookform/resolvers/zod";
import { Info, Plus, Trash2 } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useFieldArray, useForm, useWatch, type Control } from "react-hook-form";
import { useNavigate } from "react-router";

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
  hourOf,
  MAX_CHARACTERS,
  MAX_PARTS,
  MAX_WORDS_PER_PART,
  publishSchema,
  wordCount,
  type ComposeValues,
} from "./composeForm";
import { genreLabel, genres } from "./genres";
import { hourLabel } from "./localDate";

const hours = Array.from({ length: 24 }, (_, h) => h);

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
  /** Called once the first autosave gives a new draft its id. */
  onDraftNamed: (perspectiveId: string) => void;
};

/**
 * The writing surface. A draft autosaves through SaveDraft and is published
 * explicitly; a published perspective is edited in place through Update, with
 * its day and hour fixed.
 */
export function Composer({ place, today, initial, perspective, notice, onDraftNamed }: ComposerProps) {
  const navigate = useNavigate();
  const published = perspective?.status === PerspectiveStatus.PUBLISHED;
  const form = useForm<ComposeValues>({
    defaultValues: initial,
    resolver: zodResolver(useMemo(() => publishSchema(today), [today])),
  });
  const parts = useFieldArray({ control: form.control, name: "parts" });
  const characters = useFieldArray({ control: form.control, name: "characters", keyName: "key" });
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const draftId = useRef(perspective?.id ?? "");
  const namedRef = useRef(onDraftNamed);
  namedRef.current = onDraftNamed;

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
      namedRef.current(id);
    }
  }, describe);

  // Every edit to a draft restarts the autosave debounce. A published
  // perspective is only written when the author saves it.
  const { schedule } = autosave;
  useEffect(() => {
    if (published) return;
    const sub = form.watch(() => schedule());
    return () => sub.unsubscribe();
  }, [form, published, schedule]);

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
      });
      autosave.cancel();
      await navigate(`/perspectives/${encodeURIComponent(res.perspectiveId)}`);
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
          <h1 id="compose-title" className="text-2xl sm:text-3xl">
            {published ? "Edit your perspective" : "Write a perspective"}
          </h1>
          <p className="text-ink-muted [overflow-wrap:anywhere]">
            {place.name}
            {place.ancestors.length > 0 && <>, {place.ancestors.map((a) => a.name).reverse().join(", ")}</>}
          </p>
        </header>

        {notice && (
          <p className="flex gap-2 rounded-md border border-rule bg-surface p-3 text-sm text-ink">
            <Info aria-hidden size={16} className="mt-0.5 shrink-0 text-info" />
            <span>{notice}</span>
          </p>
        )}

        {published ? (
          <p className="text-sm text-ink-muted">
            Filed under {perspective?.localDate}, {perspective?.hour === undefined ? "whole day" : hourLabel(perspective.hour)}{" "}
            at {place.name}. The day and hour are fixed once published.
          </p>
        ) : (
          <WhenFields control={form.control} place={place} today={today} />
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
            writer in this arc can claim to be one of them. Up to {MAX_CHARACTERS}.
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
          <Button type="submit" disabled={busy}>
            {published ? "Save changes" : "Publish"}
          </Button>
          <SaveStatus state={autosave.state} published={published} />
        </div>
      </form>
    </Form>
  );
}

function WhenFields({ control, place, today }: { control: Control<ComposeValues>; place: Place; today: string }) {
  return (
    <div className="grid min-w-0 gap-4 sm:grid-cols-2">
      <FormField
        control={control}
        name="localDate"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Day of your visit</FormLabel>
            <FormControl>
              <Input type="date" max={today} required {...field} />
            </FormControl>
            <FormDescription>
              Today in {place.name} is {today}.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={control}
        name="hour"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Hour</FormLabel>
            <Select value={field.value} onValueChange={field.onChange}>
              <FormControl>
                <SelectTrigger onBlur={field.onBlur}>
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
      <FormField
        control={control}
        name="title"
        render={({ field }) => (
          <FormItem className="sm:col-span-2">
            <FormLabel>Arc title</FormLabel>
            <FormControl>
              <Input {...field} autoComplete="off" />
            </FormControl>
            <FormDescription>
              Needed only if you are the first to write about this place on this day: 3 to 80 characters, more than
              the place name or the date.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
    </div>
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

function SaveStatus({ state, published }: { state: AutosaveState; published: boolean }) {
  let text = "";
  switch (state.kind) {
    case "idle":
      text = "";
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

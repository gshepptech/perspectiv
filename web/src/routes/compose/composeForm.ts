import { z } from "zod";

import { Genre } from "@/gen/perspectiv/v1/enums_pb";
import type { CharacterInput, Perspective } from "@/gen/perspectiv/v1/perspective_pb";

import { isLocalDate } from "./localDate";

export const MAX_PARTS = 10;
export const MAX_WORDS_PER_PART = 300;
export const MAX_CHARACTERS = 10;
export const TITLE_MIN = 3;
export const TITLE_MAX = 80;

/** Words as an author counts them: runs of non-space characters. */
export function wordCount(text: string): number {
  const trimmed = text.trim();
  return trimmed ? trimmed.split(/\s+/).length : 0;
}

/**
 * Whether any part holds a word. A draft with none is never saved: the
 * server refuses it, and an empty draft beside a real one would be the
 * newest, so the one that reopens.
 */
export function hasText(parts: readonly ({ body?: string } | undefined)[] | undefined): boolean {
  return (parts ?? []).some((p) => wordCount(p?.body ?? "") > 0);
}

/** The hour field holds "day" for the day arc or "0" to "23" for an hour arc. */
export const DAY = "day";

export type ComposeValues = {
  localDate: string;
  hour: string;
  title: string;
  genre: Genre;
  parts: { body: string }[];
  characters: { id: string; label: string; claimed: boolean }[];
};

/**
 * What a publish (or a published edit) must satisfy before it is sent. The
 * server checks the same limits and more (an arc title that only repeats the
 * place name or the date, say); its answer is shown when it refuses. Drafts
 * are saved without these checks so half-finished work is never lost.
 */
export function publishSchema(today: string) {
  return z.object({
    localDate: z
      .string()
      .refine(isLocalDate, "Pick the day of your visit")
      .refine((d) => d <= today, "Pick a day that has already happened"),
    hour: z.string(),
    title: z
      .string()
      .refine((t) => t.trim() === "" || (t.trim().length >= TITLE_MIN && t.trim().length <= TITLE_MAX), {
        message: `Use ${TITLE_MIN} to ${TITLE_MAX} characters`,
      }),
    genre: z.number().refine((g) => g !== Genre.UNSPECIFIED, "Choose a genre"),
    parts: z
      .array(
        z.object({
          body: z
            .string()
            .refine((b) => b.trim() !== "", "Write this part, or remove it")
            .refine((b) => wordCount(b) <= MAX_WORDS_PER_PART, `Keep each part to ${MAX_WORDS_PER_PART} words or fewer`),
        }),
      )
      .min(1)
      .max(MAX_PARTS),
    characters: z
      .array(
        z.object({
          id: z.string(),
          label: z.string().refine((l) => l.trim() !== "", "Describe this character, or remove them"),
          claimed: z.boolean(),
        }),
      )
      .max(MAX_CHARACTERS),
  });
}

export function hourOf(values: Pick<ComposeValues, "hour">): number | undefined {
  return values.hour === DAY ? undefined : Number(values.hour);
}

export function characterInputs(values: Pick<ComposeValues, "characters">): Pick<CharacterInput, "id" | "label">[] {
  return values.characters.map((c) => ({ id: c.id, label: c.label.trim() }));
}

/** The form's starting values for a saved draft or a published perspective. */
export function valuesFromPerspective(p: Perspective, today: string): ComposeValues {
  const parts = [...p.parts].sort((a, b) => a.position - b.position).map((part) => ({ body: part.body }));
  return {
    localDate: p.localDate || today,
    hour: p.hour === undefined ? DAY : String(p.hour),
    title: p.titleIfNewArc,
    genre: p.genre,
    parts: parts.length ? parts : [{ body: "" }],
    characters: [...p.characters]
      .sort((a, b) => a.position - b.position)
      .map((c) => ({ id: c.id, label: c.label, claimed: c.hasAcceptedClaim })),
  };
}

export function blankValues(localDate: string, hour: number | undefined): ComposeValues {
  return {
    localDate,
    hour: hour === undefined ? DAY : String(hour),
    title: "",
    genre: Genre.UNSPECIFIED,
    parts: [{ body: "" }],
    characters: [],
  };
}

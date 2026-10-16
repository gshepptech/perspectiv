import { Genre, LoreStatus } from "@/gen/perspectiv/v1/enums_pb";

const genreLabels: Record<Exclude<Genre, Genre.UNSPECIFIED>, string> = {
  [Genre.DRAMA]: "Drama",
  [Genre.COMEDY]: "Comedy",
  [Genre.ROMANCE]: "Romance",
  [Genre.MYSTERY]: "Mystery",
  [Genre.HORROR]: "Horror",
  [Genre.SLICE_OF_LIFE]: "Slice of life",
  [Genre.ACTION]: "Action",
};

export function genreLabel(g: Genre): string | null {
  return g === Genre.UNSPECIFIED ? null : genreLabels[g];
}

/** A perspective's lore status in words, and the status colour it carries beside the words. */
export function loreStatusLabel(s: LoreStatus): { text: string; tone: "muted" | "warning" | "success" } | null {
  switch (s) {
    case LoreStatus.UNSPECIFIED:
      return null;
    case LoreStatus.PENDING:
      return { text: "Waiting for a link", tone: "warning" };
    case LoreStatus.IN_LORE:
      return { text: "In lore", tone: "success" };
    case LoreStatus.SOLO:
      return { text: "Tale", tone: "muted" };
    case LoreStatus.REMOVED:
      return { text: "Removed from lore", tone: "muted" };
  }
}

/** "09:00" for hour 9: hours are place-local clock hours 0 to 23. */
export function hourLabel(hour: number): string {
  return `${String(hour).padStart(2, "0")}:00`;
}

/** "October 1, 2026" for "2026-10-01". The date is a calendar day, so it is formatted in UTC and never shifts. */
export function dateLabel(localDate: string): string {
  const d = new Date(`${localDate}T00:00:00Z`);
  if (Number.isNaN(d.getTime())) return localDate;
  return d.toLocaleDateString("en-US", { timeZone: "UTC", year: "numeric", month: "long", day: "numeric" });
}

import { Emoji, ReportReason } from "@/gen/perspectiv/v1/enums_pb";

// Keyed by every non-UNSPECIFIED enum value, so a value added to the proto
// fails the type check here until it has a label.
const emojiLabels: Record<Exclude<Emoji, Emoji.UNSPECIFIED>, { glyph: string; name: string }> = {
  [Emoji.HEART]: { glyph: "❤️", name: "Heart" },
  [Emoji.LAUGH]: { glyph: "\u{1F602}", name: "Laugh" },
  [Emoji.WOW]: { glyph: "\u{1F62E}", name: "Wow" },
  [Emoji.SAD]: { glyph: "\u{1F622}", name: "Sad" },
  [Emoji.FIRE]: { glyph: "\u{1F525}", name: "Fire" },
  [Emoji.CLAP]: { glyph: "\u{1F44F}", name: "Clap" },
};

const reasonLabels: Record<Exclude<ReportReason, ReportReason.UNSPECIFIED>, string> = {
  [ReportReason.REAL_NAME_OR_HANDLE_OF_NON_USER]: "Real name or handle of a non-user",
  [ReportReason.HARASSMENT]: "Harassment",
  [ReportReason.HATEFUL_CONTENT]: "Hateful content",
  [ReportReason.SEXUAL_CONTENT]: "Sexual content",
  [ReportReason.SPAM]: "Spam",
  [ReportReason.OTHER]: "Other",
  [ReportReason.DUPLICATE_PLACE]: "Same place as another listing",
};

function inEnumOrder<K extends number, V>(labels: Record<K, V>): { value: K; label: V }[] {
  return Object.entries(labels)
    .map(([key, label]) => ({ value: Number(key) as K, label: label as V }))
    .sort((a, b) => a.value - b.value);
}

/** The fixed react set in enum order, with the glyph and its spoken name. */
export const emojis: readonly { value: Emoji; glyph: string; name: string }[] = inEnumOrder(emojiLabels).map(
  ({ value, label }) => ({ value, ...label }),
);

/** Every report reason a reporter can pick, in enum order; UNSPECIFIED is never offered. */
export const reportReasons: readonly { value: ReportReason; label: string }[] = inEnumOrder(reasonLabels);

/** The reasons offered for a target: "same place" is for a place only. */
export function reportReasonsFor(isPlace: boolean): readonly { value: ReportReason; label: string }[] {
  return reportReasons.filter((r) => isPlace || r.value !== ReportReason.DUPLICATE_PLACE);
}

/**
 * A report reason in the reporter's own words, so the report queue names it
 * exactly as the reporter chose it.
 */
export function reportReasonLabel(reason: ReportReason): string {
  return reason === ReportReason.UNSPECIFIED ? "No reason given" : reasonLabels[reason];
}

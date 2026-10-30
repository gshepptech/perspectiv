import { describe, expect, it } from "vitest";

import { ReportReason } from "@/gen/perspectiv/v1/enums_pb";

import { reportReasonLabel, reportReasonsFor } from "./labels";

describe("report reasons", () => {
  it("offer 'same place' only when reporting a place", () => {
    const place = reportReasonsFor(true).map((r) => r.value);
    const story = reportReasonsFor(false).map((r) => r.value);
    expect(place).toContain(ReportReason.DUPLICATE_PLACE);
    expect(story).not.toContain(ReportReason.DUPLICATE_PLACE);
    expect(place).toHaveLength(story.length + 1);
    expect(reportReasonLabel(ReportReason.DUPLICATE_PLACE)).toBe("Same place as another listing");
  });
});

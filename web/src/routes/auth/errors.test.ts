import { Code, ConnectError } from "@connectrpc/connect";
import { describe, expect, it } from "vitest";

import { asSentence, errorMessage } from "./errors";

describe("errorMessage", () => {
  it.each([
    ["password must be at least 10 characters", "Password must be at least 10 characters."],
    ["That name is taken.", "That name is taken."],
    ["  is it here?  ", "Is it here?"],
  ])("shows the server's %j as a sentence", (raw, shown) => {
    expect(errorMessage(new ConnectError(raw, Code.InvalidArgument))).toBe(shown);
  });

  it("falls back when the server says nothing", () => {
    expect(errorMessage(new ConnectError("", Code.FailedPrecondition))).toBe("That did not work. Check the form and try again.");
  });

  it("keeps an empty message empty", () => {
    expect(asSentence("   ")).toBe("");
  });
});

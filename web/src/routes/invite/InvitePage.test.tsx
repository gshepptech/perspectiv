import { screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { connectError } from "@/lib/fakeApi.testutil";
import { renderApp } from "@/routes/read/renderApp.testutil";

afterEach(() => vi.unstubAllGlobals());

const invite = {
  code: "abcdefgh23",
  inviter: { userId: "u-1", username: "wren" },
  arcId: "arc-1",
  arcTitle: "Open mic night",
  place: { id: "pl-1", kind: "PLACE_KIND_STORE", name: "Moonbean Coffee" },
  localDate: "2026-10-09",
  hour: 21,
  excerpt: "The power went out halfway through open mic.",
  characterLabel: "the guy in the yellow raincoat",
};

describe("invite page", () => {
  it("asks a signed-out reader whether they were the character, and opens writing at that place and hour", async () => {
    const { calls } = renderApp("/i/abcdefgh23", { "PerspectiveService/GetInvite": () => ({ body: { invite } }) });
    expect(await screen.findByRole("heading", { level: 1, name: "Were you the guy in the yellow raincoat?" })).toBeInTheDocument();
    expect(screen.getByText("The power went out halfway through open mic.")).toBeInTheDocument();
    expect(screen.getByText("Moonbean Coffee")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Write your side" })).toHaveAttribute(
      "href",
      "/compose?placeId=pl-1&date=2026-10-09&invite=abcdefgh23&hour=21",
    );
    expect(screen.getByRole("link", { name: "Read the whole story first" })).toHaveAttribute("href", "/arcs/arc-1");
    expect(calls("PerspectiveService/GetInvite")).toEqual([{ code: "abcdefgh23" }]);
  });

  it("asks plainly when no character is named", async () => {
    renderApp("/i/abcdefgh23", { "PerspectiveService/GetInvite": () => ({ body: { invite: { ...invite, characterLabel: "" } } }) });
    expect(await screen.findByRole("heading", { level: 1, name: "Were you there too?" })).toBeInTheDocument();
  });

  it("says when the link no longer works", async () => {
    renderApp("/i/zzzzzzzzzz", { "PerspectiveService/GetInvite": () => connectError("not_found") });
    expect(await screen.findByRole("heading", { name: "This invite no longer works" })).toBeInTheDocument();
  });
});

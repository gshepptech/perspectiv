import { QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { createQueryClient } from "@/app/queryClient";
import { installFakeApi } from "@/lib/fakeApi.testutil";

import { PlacePicker } from "./PlacePicker";

afterEach(() => vi.unstubAllGlobals());

const tampa = [
  { id: "p-fl", kind: "PLACE_KIND_STATE", name: "Florida" },
  { id: "p-tampa", kind: "PLACE_KIND_CITY", name: "Tampa" },
];
// Two listings of one stadium: the server puts the one with stories first.
const busy = { id: "p-busy", kind: "PLACE_KIND_MALL", name: "Raymond James Stadium", category: "PLACE_CATEGORY_SPORTS_AND_RECREATION", stories: 12, ancestors: tampa };
const quiet = { id: "p-quiet", kind: "PLACE_KIND_STORE", name: "Raymond James Stadium", stories: 0, ancestors: tampa };

function renderPicker(reply: unknown) {
  installFakeApi({ "PlaceService/SearchPlaces": () => ({ body: reply }) });
  const client = createQueryClient();
  client.setDefaultOptions({ queries: { retry: false } });
  const onPick = vi.fn();
  render(
    <QueryClientProvider client={client}>
      <PlacePicker onPick={onPick} />
    </QueryClientProvider>,
  );
  return onPick;
}

describe("PlacePicker results", () => {
  it("says what and where each place is and how many stories it holds, in the server's order", async () => {
    const user = userEvent.setup();
    const onPick = renderPicker({ places: [busy, quiet] });
    await user.type(screen.getByRole("searchbox", { name: "Where were you?" }), "raymond james");
    const rows = within(await screen.findByRole("list", { name: "Matching places" })).getAllByRole("button");
    expect(rows.map((r) => r.textContent)).toEqual([
      "Raymond James StadiumSports in Tampa, Florida · 12 stories",
      "Raymond James StadiumStore in Tampa, Florida · No stories yet",
    ]);
    expect(screen.queryByText("More places match: add a town to narrow it")).not.toBeInTheDocument();
    await user.click(rows[0]!);
    expect(onPick).toHaveBeenCalledWith(expect.objectContaining({ id: "p-busy" }));
  });

  it("stops the search at the 200 characters the server takes", () => {
    renderPicker({ places: [] });
    expect(screen.getByRole("searchbox", { name: "Where were you?" })).toHaveAttribute("maxlength", "200");
  });

  it("tells the writer to add a town when the search hit its cap", async () => {
    const user = userEvent.setup();
    renderPicker({ places: [busy], more: true });
    await user.type(screen.getByRole("searchbox", { name: "Where were you?" }), "stadium");
    expect(await screen.findByText("More places match: add a town to narrow it")).toBeInTheDocument();
  });
});

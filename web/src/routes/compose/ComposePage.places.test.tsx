import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

import { resetSessionForTests } from "@/lib/session";

import { apiError, installApi, ok, polyfillRadix, renderRoute, type ApiHandler } from "./api.testutil";
import { LATE_EVENING_LA, nyStore, getPlace, openComposer } from "./composer.testutil";

beforeAll(() => polyfillRadix());
beforeEach(() => resetSessionForTests());
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("choosing a place", () => {
  it("searches places and opens the composer for the one picked", async () => {
    const user = userEvent.setup();
    const api = installApi({
      "PlaceService/SearchPlaces": () => ok({ places: [nyStore] }),
      "PlaceService/GetPlace": getPlace,
    });
    renderRoute("/compose");
    const search = await screen.findByRole("searchbox", { name: "Where were you?" });
    await user.type(search, "corner");
    const results = await screen.findByRole("list", { name: "Matching places" });
    await user.click(within(results).getByRole("button", { name: /Corner Books/ }));
    expect(await screen.findByRole("heading", { name: "Write a story" })).toBeInTheDocument();
    expect(api.callsTo("PlaceService/SearchPlaces")[0]?.body).toMatchObject({ query: "corner" });
    expect(screen.getByLabelText("Day of your visit")).toBeInTheDocument();
  });

  const nyCity = {
    id: "p-city",
    kind: "PLACE_KIND_CITY",
    name: "New York City",
    timezone: "America/New_York",
    parentId: "p-state",
    ancestors: [{ id: "p-state", kind: "PLACE_KIND_STATE", name: "New York" }],
  };

  async function startAdding(user: ReturnType<typeof userEvent.setup>, routes: Record<string, ApiHandler>) {
    const api = installApi({
      "PlaceService/SearchPlaces": (body) => ok({ places: body.query === "new york" ? [nyCity, nyStore] : [] }),
      ...routes,
    });
    renderRoute("/compose");
    await user.type(await screen.findByRole("searchbox", { name: "Where were you?" }), "Tiny Tea");
    expect(await screen.findByText("No places match “Tiny Tea”.")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Add a place" }));
    const form = await screen.findByRole("form", { name: "Add a place" });
    expect(within(form).getByRole("textbox", { name: "Name" })).toHaveValue("Tiny Tea");
    const where = within(form).getByRole("searchbox", { name: "Where is it?" });
    expect(where).toHaveAttribute("maxlength", "200");
    await user.type(where, "new york");
    const parents = await within(form).findByRole("radiogroup", { name: "Places it can be in" });
    // A store cannot sit inside another store, so only the city is offered.
    expect(within(parents).queryByRole("radio", { name: /Corner Books/ })).toBeNull();
    await user.click(within(parents).getByRole("radio", { name: /New York City/ }));
    return { api, form };
  }

  it("opens the add form with the name searched for elsewhere", async () => {
    installApi({ "PlaceService/SearchPlaces": () => ok({ places: [] }) });
    renderRoute("/compose?add=Dollar%20General");
    const form = await screen.findByRole("form", { name: "Add a place" });
    expect(within(form).getByRole("textbox", { name: "Name" })).toHaveValue("Dollar General");
    expect(within(form).getByText(/More than one in town\? Add the street/)).toBeInTheDocument();
  });

  it.each([
    {
      name: "the search's town fills Where, and a lower-case name is title-cased",
      path: "/compose?add=joes+crab+shack&in=p-city",
      want: "Joes Crab Shack",
      where: /New York City/,
    },
    { name: "a name with a capital stays as typed", path: "/compose?add=McDonald%27s&in=p-city", want: "McDonald's", where: /New York City/ },
    { name: "no town leaves Where to choose", path: "/compose?add=joes+crab+shack", want: "Joes Crab Shack", where: null },
  ])("starts the add form from the front page's link: $name", async ({ path, want, where }) => {
    installApi({
      "PlaceService/SearchPlaces": () => ok({ places: [] }),
      "PlaceService/GetPlace": () => ok({ place: nyCity }),
    });
    renderRoute(path);
    const form = await screen.findByRole("form", { name: "Add a place" });
    expect(within(form).getByRole("textbox", { name: "Name" })).toHaveValue(want);
    if (where) {
      expect(await within(form).findByRole("radio", { name: where })).toBeChecked();
    } else {
      expect(within(form).queryByRole("radiogroup", { name: "Places it can be in" })).toBeNull();
    }
  });

  it("starts the picker's add form from the split the search read", async () => {
    const user = userEvent.setup();
    installApi({
      "PlaceService/SearchPlaces": () =>
        ok({ places: [], split: { name: "tiny tea", localityId: "p-city", localityLabel: "New York City, New York" } }),
      "PlaceService/GetPlace": () => ok({ place: nyCity }),
    });
    renderRoute("/compose");
    await user.type(await screen.findByRole("searchbox", { name: "Where were you?" }), "tiny tea new york city");
    expect(await screen.findByText("No places match “tiny tea new york city”.")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Add a place" }));
    const form = await screen.findByRole("form", { name: "Add a place" });
    expect(within(form).getByRole("textbox", { name: "Name" })).toHaveValue("Tiny Tea");
    expect(await within(form).findByRole("radio", { name: /New York City/ })).toBeChecked();
  });

  it("opens the add form with the place it was added from already chosen", async () => {
    const user = userEvent.setup();
    const added = { ...nyStore, id: "p-new", name: "Tiny Tea", parentId: "p-city", userAdded: true };
    const api = installApi({
      "PlaceService/SearchPlaces": () => ok({ places: [] }),
      "PlaceService/GetPlace": (body) => ok({ place: body.placeId === "p-city" ? nyCity : added }),
      "PlaceService/CreatePlace": () => ok({ placeId: "p-new", place: added }),
    });
    renderRoute("/compose?in=p-city");
    const form = await screen.findByRole("form", { name: "Add a place" });
    expect(await within(form).findByRole("radio", { name: /New York City/ })).toBeChecked();
    await user.type(within(form).getByRole("textbox", { name: "Name" }), "Tiny Tea");
    await user.click(within(form).getByRole("button", { name: "Add and write" }));
    expect(await screen.findByLabelText("Day of your visit")).toBeInTheDocument();
    expect(api.callsTo("PlaceService/CreatePlace")[0]?.body).toEqual({ name: "Tiny Tea", kind: "PLACE_KIND_STORE", parentId: "p-city" });
  });

  it("adds a store that is not listed and opens the composer there", async () => {
    const user = userEvent.setup();
    const added = { ...nyStore, id: "p-new", name: "Tiny Tea", parentId: "p-city", userAdded: true };
    const { api, form } = await startAdding(user, {
      "PlaceService/CreatePlace": () => ok({ placeId: "p-new", place: added }),
      "PlaceService/GetPlace": () => ok({ place: added }),
    });
    await user.click(within(form).getByRole("button", { name: "Add and write" }));
    expect(await screen.findByLabelText("Day of your visit")).toBeInTheDocument();
    expect(api.callsTo("PlaceService/CreatePlace")[0]?.body).toEqual({
      name: "Tiny Tea",
      kind: "PLACE_KIND_STORE",
      parentId: "p-city",
    });
    expect(api.callsTo("PlaceService/GetPlace")[0]?.body).toEqual({ placeId: "p-new" });
  });

  it.each([
    ["already_exists", "a place with this name already exists there", /already a place called “Tiny Tea” there/],
    ["not_found", "no such place", /no longer available/],
    ["invalid_argument", "a mall cannot be added under a store", /a mall cannot be added under a store/],
  ])("explains a %s refusal inline and stays on the form", async (code, message, shown) => {
    const user = userEvent.setup();
    const { api, form } = await startAdding(user, { "PlaceService/CreatePlace": () => apiError(code, message) });
    await user.click(within(form).getByRole("button", { name: "Add and write" }));
    expect(await within(form).findByRole("alert")).toHaveTextContent(shown);
    expect(api.callsTo("PlaceService/GetPlace")).toHaveLength(0);
    expect(screen.getByRole("form", { name: "Add a place" })).toBeInTheDocument();
  });

  it("shows a not-found message for a place that does not exist", async () => {
    installApi({ "PlaceService/GetPlace": () => apiError("not_found", "no such place") });
    renderRoute("/compose?placeId=gone");
    expect(await screen.findByRole("alert")).toHaveTextContent(/could not find that place/i);
  });
});

describe("composer: days already written about", () => {
  const epic = {
    place: nyStore,
    arcs: [
      {
        arcId: "arc-oct3",
        title: "Fourth quarter, sixty thousand people",
        place: { id: nyStore.id, kind: nyStore.kind, name: nyStore.name },
        localDate: "2026-10-03",
        perspectiveCount: 14,
      },
      {
        arcId: "arc-under",
        title: "At the shop under this place",
        place: { id: "p-other", kind: "PLACE_KIND_STORE", name: "Another shop" },
        localDate: "2026-10-02",
        perspectiveCount: 3,
      },
      {
        arcId: "arc-sep30-14",
        title: "The fire drill · 14:00",
        place: { id: nyStore.id, kind: nyStore.kind, name: nyStore.name },
        localDate: "2026-09-30",
        hour: 14,
        perspectiveCount: 1,
      },
      {
        arcId: "arc-2025",
        title: "Last year's flood",
        place: { id: nyStore.id, kind: nyStore.kind, name: nyStore.name },
        localDate: "2025-08-01",
        perspectiveCount: 2,
      },
    ],
  };

  it("offers the place's recent days with stories, and choosing one sets the day and hour", async () => {
    vi.useFakeTimers({ toFake: ["Date"], now: LATE_EVENING_LA });
    const user = userEvent.setup();
    const api = await openComposer("/compose?placeId=p-ny", { "PlaceService/GetEpic": () => ok(epic) });
    const list = await screen.findByRole("list", { name: "Days already written about here" });
    const choices = within(list).getAllByRole("button");
    // Only days filed at this place itself; the year only when it is not this one.
    expect(choices.map((b) => b.textContent)).toEqual([
      "Oct 3 · Fourth quarter, sixty thousand people · 14 stories",
      "Sep 30 · The fire drill · 14:00 · 1 story",
      "Aug 1, 2025 · Last year's flood · 2 stories",
    ]);
    expect(api.callsTo("PlaceService/GetEpic")[0]?.body).toMatchObject({ placeId: "p-ny", tab: "EPIC_TAB_NEWEST" });
    for (const b of choices) expect(b).toHaveAttribute("aria-pressed", "false");

    await user.click(choices[1]!);
    expect(screen.getByLabelText("Day of your visit")).toHaveValue("2026-09-30");
    expect(screen.getByRole("combobox", { name: "Hour" })).toHaveTextContent("14:00");
    expect(choices[1]).toHaveAttribute("aria-pressed", "true");

    await user.click(choices[0]!);
    expect(screen.getByLabelText("Day of your visit")).toHaveValue("2026-10-03");
    expect(screen.getByRole("combobox", { name: "Hour" })).toHaveTextContent("Whole day");
    expect(choices[0]).toHaveAttribute("aria-pressed", "true");
    expect(choices[1]).toHaveAttribute("aria-pressed", "false");
  });

  it("offers nothing when the place has no days yet", async () => {
    await openComposer("/compose?placeId=p-ny", { "PlaceService/GetEpic": () => ok({ place: nyStore, arcs: [] }) });
    await waitFor(() => expect(screen.getByRole("textbox", { name: "Part 1" })).toBeInTheDocument());
    expect(screen.queryByRole("list", { name: "Days already written about here" })).toBeNull();
  });
});

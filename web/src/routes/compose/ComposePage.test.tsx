import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

import { resetSessionForTests } from "@/lib/session";

import { apiError, installApi, ok, polyfillRadix, renderRoute, type ApiHandler } from "./api.testutil";
import { AUTOSAVE_DEBOUNCE_MS } from "./autosave";

// The author's device is in Los Angeles; the store is in New York.
process.env.TZ = "America/Los_Angeles";

// 00:30 on 16 October in New York is still 21:30 on 15 October in Los Angeles.
const LATE_EVENING_LA = new Date("2026-10-16T04:30:00Z");

const nyStore = {
  id: "p-ny",
  kind: "PLACE_KIND_STORE",
  name: "Corner Books",
  timezone: "America/New_York",
  parentId: "p-city",
  ancestors: [
    { id: "p-state", kind: "PLACE_KIND_STATE", name: "New York" },
    { id: "p-city", kind: "PLACE_KIND_CITY", name: "New York City" },
  ],
};

const getPlace: ApiHandler = () => ok({ place: nyStore });

function draftReply(id: string, body: Record<string, unknown>) {
  return ok({
    perspective: {
      id,
      status: "PERSPECTIVE_STATUS_DRAFT",
      place: { id: nyStore.id, kind: nyStore.kind, name: nyStore.name },
      localDate: body.localDate,
      parts: ((body.parts as string[] | undefined) ?? []).map((text, i) => ({ position: i + 1, body: text })),
    },
  });
}

function publishedPerspective(over: Record<string, unknown> = {}) {
  return {
    id: "pub-1",
    arcId: "arc-1",
    arcTitle: "The rainy Thursday",
    author: { userId: "0b9e0a52-5d0f-4a8e-9a43-1f0f7f3c2a10", username: "wren" },
    genre: "GENRE_MYSTERY",
    status: "PERSPECTIVE_STATUS_PUBLISHED",
    loreStatus: "LORE_STATUS_PENDING",
    place: { id: nyStore.id, kind: nyStore.kind, name: nyStore.name },
    localDate: "2026-10-15",
    hour: 14,
    parts: [{ position: 1, body: "The bell over the door rang twice." }],
    characters: [
      { id: "c-1", position: 1, label: "the clerk with the green scarf", hasAcceptedClaim: true },
      { id: "c-2", position: 2, label: "a man reading maps", hasAcceptedClaim: false },
    ],
    ...over,
  };
}

beforeAll(() => polyfillRadix());
beforeEach(() => resetSessionForTests());
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

async function openComposer(path = "/compose?placeId=p-ny", routes: Record<string, ApiHandler> = {}) {
  const api = installApi({ "PlaceService/GetPlace": getPlace, ...routes });
  renderRoute(path);
  await screen.findByRole("heading", { name: "Write a perspective" });
  return api;
}

async function pickGenre(user: ReturnType<typeof userEvent.setup>, name: string) {
  await user.click(screen.getByRole("combobox", { name: "Genre" }));
  await user.click(await screen.findByRole("option", { name }));
}

describe("composer date picker", () => {
  it("defaults to today in the place's time zone, not the device's (OT-014, AC-016)", async () => {
    vi.useFakeTimers({ toFake: ["Date"], now: LATE_EVENING_LA });
    // The device really is on the previous calendar day.
    expect(new Date().getDate()).toBe(15);
    await openComposer();
    expect(screen.getByLabelText("Day of your visit")).toHaveValue("2026-10-16");
    expect(screen.getByText(/today in Corner Books is/i)).toHaveTextContent("2026-10-16");
  });

  it("files a perspective under the day the author picks, not the publish day (OT-014, AC-016)", async () => {
    vi.useFakeTimers({ toFake: ["Date"], now: LATE_EVENING_LA });
    const user = userEvent.setup();
    const api = await openComposer("/compose?placeId=p-ny", {
      "PerspectiveService/SaveDraft": (body) => draftReply("d-1", body),
      "PerspectiveService/Publish": () => ok({ perspectiveId: "d-1", arcId: "arc-9" }),
    });
    const date = screen.getByLabelText("Day of your visit");
    await user.clear(date);
    await user.type(date, "2026-10-15");
    await user.type(screen.getByRole("textbox", { name: "Arc title" }), "The rainy Thursday");
    await pickGenre(user, "Slice of life");
    await user.type(screen.getByRole("textbox", { name: "Part 1" }), "We met by the poetry shelf.");
    await user.click(screen.getByRole("button", { name: "Publish" }));

    await waitFor(() => expect(api.callsTo("PerspectiveService/Publish")).toHaveLength(1));
    const [publish] = api.callsTo("PerspectiveService/Publish");
    expect(publish?.body).toMatchObject({
      placeId: "p-ny",
      localDate: "2026-10-15",
      titleIfNewArc: "The rainy Thursday",
      genre: "GENRE_SLICE_OF_LIFE",
      parts: ["We met by the poetry shelf."],
    });
    expect(publish?.body).not.toHaveProperty("hour");
  });

  it("starts from the date and hour in the address when they are given", async () => {
    await openComposer("/compose?placeId=p-ny&date=2026-09-30&hour=14");
    expect(screen.getByLabelText("Day of your visit")).toHaveValue("2026-09-30");
    expect(screen.getByRole("combobox", { name: "Hour" })).toHaveTextContent("14:00");
  });

  it("does not offer a day after today in the place", async () => {
    vi.useFakeTimers({ toFake: ["Date"], now: LATE_EVENING_LA });
    await openComposer();
    expect(screen.getByLabelText("Day of your visit")).toHaveAttribute("max", "2026-10-16");
  });
});

describe("composer hour", () => {
  it("publishes into the hour the author chose with the keyboard", async () => {
    const user = userEvent.setup();
    const api = await openComposer("/compose?placeId=p-ny&date=2026-10-01", {
      "PerspectiveService/SaveDraft": (body) => draftReply("d-1", body),
      "PerspectiveService/Publish": () => ok({ perspectiveId: "d-1", arcId: "arc-9" }),
    });
    screen.getByRole("combobox", { name: "Hour" }).focus();
    await user.keyboard("{Enter}");
    await user.click(await screen.findByRole("option", { name: "09:00" }));
    await pickGenre(user, "Drama");
    await user.type(screen.getByRole("textbox", { name: "Part 1" }), "Morning rush.");
    await user.click(screen.getByRole("button", { name: "Publish" }));
    await waitFor(() => expect(api.callsTo("PerspectiveService/Publish")).toHaveLength(1));
    expect(api.callsTo("PerspectiveService/Publish")[0]?.body).toMatchObject({ localDate: "2026-10-01", hour: 9 });
  });
});

describe("composer autosave (FR-044)", () => {
  it(`saves a draft through SaveDraft once edits pause for ${AUTOSAVE_DEBOUNCE_MS}ms, and not before`, async () => {
    const api = await openComposer("/compose?placeId=p-ny&date=2026-10-01", {
      "PerspectiveService/SaveDraft": (body) => draftReply("d-1", body),
    });
    // Only the debounce clock is faked. Edits go through fireEvent because
    // Testing Library's async helpers wait on setTimeout themselves.
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    const part = screen.getByRole("textbox", { name: "Part 1" });

    fireEvent.change(part, { target: { value: "First" } });
    await act(() => vi.advanceTimersByTimeAsync(AUTOSAVE_DEBOUNCE_MS - 1));
    expect(api.callsTo("PerspectiveService/SaveDraft")).toHaveLength(0);

    // Another edit inside the window restarts it.
    fireEvent.change(part, { target: { value: "First line" } });
    await act(() => vi.advanceTimersByTimeAsync(AUTOSAVE_DEBOUNCE_MS - 1));
    expect(api.callsTo("PerspectiveService/SaveDraft")).toHaveLength(0);

    await act(() => vi.advanceTimersByTimeAsync(1));
    vi.useRealTimers();
    await waitFor(() => expect(api.callsTo("PerspectiveService/SaveDraft")).toHaveLength(1));
    expect(api.callsTo("PerspectiveService/SaveDraft")[0]?.body).toMatchObject({
      placeId: "p-ny",
      localDate: "2026-10-01",
      parts: ["First line"],
    });
    expect(api.callsTo("PerspectiveService/SaveDraft")[0]?.body).not.toHaveProperty("perspectiveId");
    expect(await screen.findByRole("status", { name: "Save status" })).toHaveTextContent("Draft saved");
  });

  it("keeps saving into the same draft once the first save names it", async () => {
    const user = userEvent.setup();
    let n = 0;
    const api = await openComposer("/compose?placeId=p-ny&date=2026-10-01", {
      "PerspectiveService/SaveDraft": (body) => {
        n += 1;
        return draftReply("d-7", body);
      },
    });
    const part = screen.getByRole("textbox", { name: "Part 1" });
    await user.type(part, "One");
    await waitFor(() => expect(n).toBe(1), { timeout: AUTOSAVE_DEBOUNCE_MS + 2000 });
    await user.type(part, " two");
    await waitFor(() => expect(n).toBe(2), { timeout: AUTOSAVE_DEBOUNCE_MS + 2000 });
    expect(api.callsTo("PerspectiveService/SaveDraft")[1]?.body).toMatchObject({
      perspectiveId: "d-7",
      parts: ["One two"],
    });
    // The text the author typed is still there: the save did not reload the form.
    expect(part).toHaveValue("One two");
  });

  it("never runs two saves at once, so a slow first save cannot fork a second draft", async () => {
    const user = userEvent.setup();
    let release: () => void = () => undefined;
    const bodies: Record<string, unknown>[] = [];
    let inFlight = 0;
    let maxInFlight = 0;
    await openComposer("/compose?placeId=p-ny&date=2026-10-01", {
      "PerspectiveService/SaveDraft": async (body) => {
        bodies.push(body);
        inFlight += 1;
        maxInFlight = Math.max(maxInFlight, inFlight);
        if (bodies.length === 1) await new Promise<void>((r) => (release = r));
        inFlight -= 1;
        return draftReply("d-slow", body);
      },
    });
    const part = screen.getByRole("textbox", { name: "Part 1" });
    await user.type(part, "a");
    await waitFor(() => expect(bodies).toHaveLength(1), { timeout: AUTOSAVE_DEBOUNCE_MS + 2000 });
    await user.type(part, "b");
    // The second debounce elapses while the first save is still in flight.
    await new Promise((r) => setTimeout(r, AUTOSAVE_DEBOUNCE_MS + 200));
    expect(bodies).toHaveLength(1);
    release();
    await waitFor(() => expect(bodies).toHaveLength(2));
    expect(maxInFlight).toBe(1);
    expect(bodies[1]).toMatchObject({ perspectiveId: "d-slow", parts: ["ab"] });
  });

  it("says when a save failed and keeps the text", async () => {
    const user = userEvent.setup();
    await openComposer("/compose?placeId=p-ny&date=2026-10-01", {
      "PerspectiveService/SaveDraft": () => apiError("invalid_argument", "part 1 is longer than 300 words"),
    });
    await user.type(screen.getByRole("textbox", { name: "Part 1" }), "x");
    const status = screen.getByRole("status", { name: "Save status" });
    await waitFor(() => expect(status).toHaveTextContent("Not saved: part 1 is longer than 300 words"), {
      timeout: AUTOSAVE_DEBOUNCE_MS + 2000,
    });
    expect(screen.getByRole("textbox", { name: "Part 1" })).toHaveValue("x");
  });

  it("flushes a pending edit into the draft before publishing it", async () => {
    const api = installApi({
      "PlaceService/GetPlace": getPlace,
      "PerspectiveService/Get": () =>
        ok({
          perspective: {
            id: "d-3",
            status: "PERSPECTIVE_STATUS_DRAFT",
            place: { id: nyStore.id, kind: nyStore.kind, name: nyStore.name },
            localDate: "2026-10-01",
            genre: "GENRE_COMEDY",
            parts: [{ position: 1, body: "Quick" }],
          },
        }),
      "PerspectiveService/SaveDraft": (body) => draftReply("d-3", body),
      "PerspectiveService/Publish": () => ok({ perspectiveId: "d-3", arcId: "arc-3" }),
    });
    renderRoute("/compose?perspectiveId=d-3");
    await screen.findByRole("heading", { name: "Write a perspective" });
    // The debounce never elapses on its own here: only publish can save.
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    fireEvent.change(screen.getByRole("textbox", { name: "Part 1" }), { target: { value: "Quick one." } });
    fireEvent.click(screen.getByRole("button", { name: "Publish" }));
    vi.useRealTimers();

    await waitFor(() => expect(api.callsTo("PerspectiveService/Publish")).toHaveLength(1));
    const order = api.calls
      .map((c) => c.procedure)
      .filter((p) => p === "PerspectiveService/SaveDraft" || p === "PerspectiveService/Publish");
    expect(order).toEqual(["PerspectiveService/SaveDraft", "PerspectiveService/Publish"]);
    expect(api.callsTo("PerspectiveService/SaveDraft")[0]?.body).toMatchObject({
      perspectiveId: "d-3",
      parts: ["Quick one."],
    });
    expect(api.callsTo("PerspectiveService/Publish")[0]?.body).toMatchObject({
      perspectiveId: "d-3",
      genre: "GENRE_COMEDY",
      parts: ["Quick one."],
    });
  });
});

describe("publishing", () => {
  it("opens the published perspective after a successful publish", async () => {
    const user = userEvent.setup();
    await openComposer("/compose?placeId=p-ny&date=2026-10-01", {
      "PerspectiveService/SaveDraft": (body) => draftReply("d-1", body),
      "PerspectiveService/Publish": () => ok({ perspectiveId: "d-1", arcId: "arc-9" }),
    });
    await pickGenre(user, "Drama");
    await user.type(screen.getByRole("textbox", { name: "Part 1" }), "Rain.");
    await user.click(screen.getByRole("button", { name: "Publish" }));
    // The read route belongs to another work stream; reaching it is the outcome.
    await waitFor(() => expect(screen.queryByRole("heading", { name: "Write a perspective" })).toBeNull());
  });

  it("on ALREADY_EXISTS opens the author's existing perspective for that place and day (OT-016, AC-012)", async () => {
    const user = userEvent.setup();
    const api = await openComposer("/compose?placeId=p-ny&date=2026-10-15&hour=14", {
      "PerspectiveService/SaveDraft": (body) => draftReply("d-2", body),
      "PerspectiveService/Publish": () => apiError("already_exists", "you already published in this arc"),
      "PerspectiveService/ListMine": () =>
        ok({
          perspectives: [
            publishedPerspective({ id: "pub-other-hour", hour: 9 }),
            publishedPerspective(),
          ],
        }),
      "PerspectiveService/Get": (body) => ok({ perspective: publishedPerspective({ id: body.perspectiveId }) }),
    });
    await pickGenre(user, "Mystery");
    await user.type(screen.getByRole("textbox", { name: "Part 1" }), "A second go.");
    await user.click(screen.getByRole("button", { name: "Publish" }));

    expect(await screen.findByRole("heading", { name: "Edit your perspective" })).toBeInTheDocument();
    expect(api.callsTo("PerspectiveService/ListMine")[0]?.body).toMatchObject({
      status: "PERSPECTIVE_STATUS_PUBLISHED",
      placeId: "p-ny",
      localDate: "2026-10-15",
      hour: 14,
    });
    expect(api.callsTo("PerspectiveService/Get").at(-1)?.body).toEqual({ perspectiveId: "pub-1" });
    expect(screen.getByRole("textbox", { name: "Part 1" })).toHaveValue("The bell over the door rang twice.");
    expect(screen.getByText(/already published a perspective here/i)).toBeInTheDocument();
  });

  it("shows the server's reason when publish is refused for another cause", async () => {
    const user = userEvent.setup();
    await openComposer("/compose?placeId=p-ny&date=2026-10-01", {
      "PerspectiveService/SaveDraft": (body) => draftReply("d-1", body),
      "PerspectiveService/Publish": () => apiError("failed_precondition", "verify your email first"),
    });
    await pickGenre(user, "Drama");
    await user.type(screen.getByRole("textbox", { name: "Part 1" }), "Rain.");
    await user.click(screen.getByRole("button", { name: "Publish" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("verify your email first");
    expect(screen.getByRole("heading", { name: "Write a perspective" })).toBeInTheDocument();
  });

  it("refuses to publish a part over 300 words or without a genre, naming the problem", async () => {
    const user = userEvent.setup();
    const api = await openComposer("/compose?placeId=p-ny&date=2026-10-01", {
      "PerspectiveService/SaveDraft": (body) => draftReply("d-1", body),
    });
    const part = screen.getByRole("textbox", { name: "Part 1" });
    await user.click(part);
    await user.paste(Array.from({ length: 301 }, () => "word").join(" "));
    expect(screen.getByText("301 / 300 words")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Publish" }));
    expect(await screen.findByText("Keep each part to 300 words or fewer")).toBeInTheDocument();
    expect(screen.getByText("Choose a genre")).toBeInTheDocument();
    expect(api.callsTo("PerspectiveService/Publish")).toHaveLength(0);
  });

  it("stops at 10 parts and 10 characters", async () => {
    const user = userEvent.setup();
    await openComposer("/compose?placeId=p-ny&date=2026-10-01", {
      "PerspectiveService/SaveDraft": (body) => draftReply("d-1", body),
    });
    for (let i = 0; i < 9; i++) await user.click(screen.getByRole("button", { name: "Add part" }));
    expect(screen.getByRole("textbox", { name: "Part 10" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add part" })).toBeDisabled();
    for (let i = 0; i < 10; i++) await user.click(screen.getByRole("button", { name: "Add character" }));
    expect(screen.getByRole("textbox", { name: "Character 10" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add character" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Remove part 10" }));
    expect(screen.getByRole("button", { name: "Add part" })).toBeEnabled();
  });
});

describe("editing a published perspective", () => {
  it("fixes the day and hour, keeps claimed characters and saves through Update", async () => {
    const user = userEvent.setup();
    const api = installApi({
      "PlaceService/GetPlace": getPlace,
      "PerspectiveService/Get": () => ok({ perspective: publishedPerspective() }),
      "PerspectiveService/Update": (body) =>
        ok({ perspective: publishedPerspective({ parts: [{ position: 1, body: (body.parts as string[])[0] }] }) }),
    });
    renderRoute("/compose?perspectiveId=pub-1");
    expect(await screen.findByRole("heading", { name: "Edit your perspective" })).toBeInTheDocument();
    expect(screen.queryByLabelText("Day of your visit")).toBeNull();
    expect(screen.getByText(/2026-10-15, 14:00/)).toBeInTheDocument();
    expect(screen.queryByRole("textbox", { name: "Arc title" })).toBeNull();

    expect(screen.getByRole("button", { name: "Remove character 1" })).toBeDisabled();
    expect(screen.getByText(/claimed by another writer, so it stays/i)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Remove character 2" }));

    const part = screen.getByRole("textbox", { name: "Part 1" });
    await user.type(part, " Then silence.");
    await user.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect(api.callsTo("PerspectiveService/Update")).toHaveLength(1));
    expect(api.callsTo("PerspectiveService/Update")[0]?.body).toEqual({
      perspectiveId: "pub-1",
      genre: "GENRE_MYSTERY",
      parts: ["The bell over the door rang twice. Then silence."],
      characters: [{ id: "c-1", label: "the clerk with the green scarf" }],
    });
    expect(api.callsTo("PerspectiveService/SaveDraft")).toHaveLength(0);
    expect(await screen.findByRole("status", { name: "Save status" })).toHaveTextContent("Changes saved");
  });

  it("resumes a draft with its saved fields", async () => {
    installApi({
      "PlaceService/GetPlace": getPlace,
      "PerspectiveService/Get": () =>
        ok({
          perspective: {
            id: "d-5",
            status: "PERSPECTIVE_STATUS_DRAFT",
            place: { id: nyStore.id, kind: nyStore.kind, name: nyStore.name },
            localDate: "2026-10-12",
            titleIfNewArc: "Closing time",
            genre: "GENRE_ROMANCE",
            parts: [{ position: 1, body: "Draft text" }],
          },
        }),
    });
    renderRoute("/compose?perspectiveId=d-5");
    expect(await screen.findByRole("heading", { name: "Write a perspective" })).toBeInTheDocument();
    expect(screen.getByLabelText("Day of your visit")).toHaveValue("2026-10-12");
    expect(screen.getByRole("textbox", { name: "Arc title" })).toHaveValue("Closing time");
    expect(screen.getByRole("combobox", { name: "Genre" })).toHaveTextContent("Romance");
    expect(screen.getByRole("textbox", { name: "Part 1" })).toHaveValue("Draft text");
  });
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
    expect(await screen.findByRole("heading", { name: "Write a perspective" })).toBeInTheDocument();
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
    await user.click(screen.getByRole("button", { name: "Add a mall or store" }));
    const form = await screen.findByRole("form", { name: "Add a place" });
    expect(within(form).getByRole("textbox", { name: "Name" })).toHaveValue("Tiny Tea");
    await user.type(within(form).getByRole("searchbox", { name: "Where is it?" }), "new york");
    const parents = await within(form).findByRole("radiogroup", { name: "Places it can be in" });
    // A store cannot sit inside another store, so only the city is offered.
    expect(within(parents).queryByRole("radio", { name: /Corner Books/ })).toBeNull();
    await user.click(within(parents).getByRole("radio", { name: /New York City/ }));
    return { api, form };
  }

  it("adds a store that is not listed and opens the composer there (FR-008)", async () => {
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

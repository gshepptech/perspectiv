import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

import { resetSessionForTests } from "@/lib/session";

import { apiError, installApi, ok, polyfillRadix, renderRoute, type ApiHandler } from "./api.testutil";
import { AUTOSAVE_DEBOUNCE_MS } from "./autosave";
import { LATE_EVENING_LA, nyStore, getPlace, draftReply, publishedPerspective, openComposer, pickGenre } from "./composer.testutil";

beforeAll(() => polyfillRadix());
beforeEach(() => resetSessionForTests());
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("composer autosave", () => {
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

  it("never saves a draft whose parts are all empty, and says it waits for text", async () => {
    const user = userEvent.setup();
    const api = await openComposer("/compose?placeId=p-ny&date=2026-10-01", {
      "PerspectiveService/SaveDraft": (body) => draftReply("d-1", body),
    });
    const status = screen.getByRole("status", { name: "Save status" });
    expect(status).toHaveTextContent("Your draft saves once a part has text.");
    // Changing the day, the genre or the title alone saves nothing.
    const date = screen.getByLabelText("Day of your visit");
    await user.clear(date);
    await user.type(date, "2026-09-26");
    await pickGenre(user, "Comedy");
    // Text typed and then removed again before the pause saves nothing either.
    const part = screen.getByRole("textbox", { name: "Part 1" });
    await user.type(part, "Bubbles");
    await user.clear(part);
    await user.type(part, "   ");
    await new Promise((r) => setTimeout(r, AUTOSAVE_DEBOUNCE_MS + 300));
    expect(api.callsTo("PerspectiveService/SaveDraft")).toHaveLength(0);
    expect(status).toHaveTextContent("Your draft saves once a part has text.");
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
    await screen.findByRole("heading", { name: "Write a story" });
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

describe("composer: resuming a draft", () => {
  const savedDraft = {
    id: "d-old",
    status: "PERSPECTIVE_STATUS_DRAFT",
    place: { id: nyStore.id, kind: nyStore.kind, name: nyStore.name },
    localDate: "2026-10-01",
    hour: 14,
    genre: "GENRE_COMEDY",
    parts: [{ position: 1, body: "I came back for the cat." }],
  };

  it("opens the writer's draft for that place, day and hour instead of starting another", async () => {
    const user = userEvent.setup();
    const api = installApi({
      "PlaceService/GetPlace": getPlace,
      "PerspectiveService/ListMine": () => ok({ perspectives: [savedDraft] }),
      "PerspectiveService/SaveDraft": (body) => draftReply("d-old", body),
    });
    const router = renderRoute("/compose?placeId=p-ny&date=2026-10-01&hour=14");
    const part = await screen.findByRole("textbox", { name: "Part 1" });
    expect(part).toHaveValue("I came back for the cat.");
    expect(screen.getByText("You started a draft for this day, so it is open again.")).toBeInTheDocument();
    expect(api.callsTo("PerspectiveService/ListMine")[0]?.body).toMatchObject({
      status: "PERSPECTIVE_STATUS_DRAFT",
      placeId: "p-ny",
      localDate: "2026-10-01",
      hour: 14,
    });
    // The address names the draft, so a reload reopens it.
    await waitFor(() => expect(router.state.location.search).toContain("perspectiveId=d-old"));
    // Edits save into that draft, never a new one.
    await user.type(part, " Again.");
    await waitFor(() => expect(api.callsTo("PerspectiveService/SaveDraft")).toHaveLength(1), { timeout: AUTOSAVE_DEBOUNCE_MS + 2000 });
    expect(api.callsTo("PerspectiveService/SaveDraft")[0]?.body).toMatchObject({
      perspectiveId: "d-old",
      parts: ["I came back for the cat. Again."],
    });
    expect(part).toHaveValue("I came back for the cat. Again.");
  });

  it("looks for a draft on today in the place when the address names no day, and starts fresh when there is none", async () => {
    vi.useFakeTimers({ now: LATE_EVENING_LA, toFake: ["Date"] });
    const api = await openComposer("/compose?placeId=p-ny");
    expect(await screen.findByRole("textbox", { name: "Part 1" })).toHaveValue("");
    expect(api.callsTo("PerspectiveService/ListMine")[0]?.body).toMatchObject({
      status: "PERSPECTIVE_STATUS_DRAFT",
      placeId: "p-ny",
      localDate: "2026-10-16",
    });
    expect(api.callsTo("PerspectiveService/ListMine")[0]?.body).not.toHaveProperty("hour");
    expect(screen.queryByText(/open again/)).toBeNull();
  });
});

describe("composer: finding the draft as the day and hour change", () => {
  const sep26 = {
    id: "d-sep26",
    status: "PERSPECTIVE_STATUS_DRAFT",
    place: { id: nyStore.id, kind: nyStore.kind, name: nyStore.name },
    localDate: "2026-09-26",
    genre: "GENRE_COMEDY",
    parts: [{ position: 1, body: "Bubbles the size of cars." }],
  };
  // ListMine answers the writer's drafts for the day asked about.
  const draftsOn =
    (byDate: Record<string, unknown[]>): ApiHandler =>
    (body) =>
      ok({ perspectives: body.status === "PERSPECTIVE_STATUS_DRAFT" ? (byDate[body.localDate as string] ?? []) : [] });

  it("opens the writer's draft in place when the day changes to one they started, and saves no other", async () => {
    vi.useFakeTimers({ toFake: ["Date"], now: LATE_EVENING_LA });
    const user = userEvent.setup();
    const api = await openComposer("/compose?placeId=p-ny", {
      "PerspectiveService/ListMine": draftsOn({ "2026-09-26": [sep26] }),
      "PerspectiveService/SaveDraft": (body) => draftReply("d-new", body),
    });
    // The composer opened on today, where there is no draft.
    expect(screen.getByRole("textbox", { name: "Part 1" })).toHaveValue("");
    const date = screen.getByLabelText("Day of your visit");
    fireEvent.change(date, { target: { value: "2026-09-26" } });

    expect(await screen.findByDisplayValue("Bubbles the size of cars.")).toBeInTheDocument();
    expect(screen.getByRole("status", { name: "Composer notice" })).toHaveTextContent(
      "You started a draft for this day, so it is open again.",
    );
    expect(api.callsTo("PerspectiveService/ListMine").map((c) => c.body.localDate)).toContain("2026-09-26");
    // Loading the draft is not an edit: nothing is saved, and no second draft is made.
    await new Promise((r) => setTimeout(r, AUTOSAVE_DEBOUNCE_MS + 300));
    expect(api.callsTo("PerspectiveService/SaveDraft")).toHaveLength(0);
    // The next edit saves into the draft found.
    await user.type(screen.getByRole("textbox", { name: "Part 1" }), " Again.");
    await waitFor(() => expect(api.callsTo("PerspectiveService/SaveDraft")).toHaveLength(1), { timeout: AUTOSAVE_DEBOUNCE_MS + 2000 });
    expect(api.callsTo("PerspectiveService/SaveDraft")[0]?.body).toMatchObject({
      perspectiveId: "d-sep26",
      localDate: "2026-09-26",
      parts: ["Bubbles the size of cars. Again."],
    });
  });

  it("resumes the draft changed most recently when there are several, and says so", async () => {
    const older = { ...sep26, id: "d-older", parts: [{ position: 1, body: "An older start." }] };
    await openComposer("/compose?placeId=p-ny&date=2026-10-01", {
      "PerspectiveService/ListMine": draftsOn({ "2026-09-26": [sep26, older] }),
    });
    fireEvent.change(screen.getByLabelText("Day of your visit"), { target: { value: "2026-09-26" } });
    expect(await screen.findByDisplayValue("Bubbles the size of cars.")).toBeInTheDocument();
    expect(screen.getByRole("status", { name: "Composer notice" })).toHaveTextContent(
      "You started more than one draft for this day, so the one you changed last is open. The others are under Your stories on the front page.",
    );
  });

  it("says so on load too when the address opens a day with several drafts", async () => {
    const older = { ...sep26, id: "d-older", parts: [{ position: 1, body: "An older start." }] };
    await openComposer("/compose?placeId=p-ny&date=2026-09-26", {
      "PerspectiveService/ListMine": draftsOn({ "2026-09-26": [sep26, older] }),
    });
    expect(screen.getByRole("textbox", { name: "Part 1" })).toHaveValue("Bubbles the size of cars.");
    expect(screen.getByText(/You started more than one draft for this day/)).toBeInTheDocument();
  });

  it("looks again when the hour changes", async () => {
    const user = userEvent.setup();
    const atTwo = { ...sep26, id: "d-two", localDate: "2026-10-01", hour: 14, parts: [{ position: 1, body: "At two." }] };
    const api = await openComposer("/compose?placeId=p-ny&date=2026-10-01", {
      "PerspectiveService/ListMine": (body) =>
        ok({ perspectives: body.status === "PERSPECTIVE_STATUS_DRAFT" && body.hour === 14 ? [atTwo] : [] }),
    });
    await user.click(screen.getByRole("combobox", { name: "Hour" }));
    await user.click(await screen.findByRole("option", { name: "14:00" }));
    expect(await screen.findByDisplayValue("At two.")).toBeInTheDocument();
    expect(api.callsTo("PerspectiveService/ListMine").at(-1)?.body).toMatchObject({ localDate: "2026-10-01", hour: 14 });
  });

  it("replaces nothing already written, and offers the other draft instead", async () => {
    const user = userEvent.setup();
    const api = await openComposer("/compose?placeId=p-ny&date=2026-10-01", {
      "PerspectiveService/ListMine": draftsOn({ "2026-09-26": [sep26] }),
      "PerspectiveService/SaveDraft": (body) => draftReply("d-mine", body),
    });
    await user.type(screen.getByRole("textbox", { name: "Part 1" }), "Something new");
    fireEvent.change(screen.getByLabelText("Day of your visit"), { target: { value: "2026-09-26" } });
    const note = await screen.findByText(/You already have a draft for this day/);
    expect(within(note).getByRole("link", { name: "Open that draft" })).toHaveAttribute("href", "/compose?perspectiveId=d-sep26");
    expect(screen.getByRole("textbox", { name: "Part 1" })).toHaveValue("Something new");
    // Their own text keeps saving as its own draft.
    await waitFor(() => expect(api.callsTo("PerspectiveService/SaveDraft").length).toBeGreaterThan(0), {
      timeout: AUTOSAVE_DEBOUNCE_MS + 2000,
    });
    expect(api.callsTo("PerspectiveService/SaveDraft").at(-1)?.body).not.toMatchObject({ perspectiveId: "d-sep26" });
  });
});

describe("composer: clearing a draft offers to delete it", () => {
  const stored = {
    id: "d-old",
    status: "PERSPECTIVE_STATUS_DRAFT",
    place: { id: nyStore.id, kind: nyStore.kind, name: nyStore.name },
    localDate: "2026-10-01",
    hour: 14,
    genre: "GENRE_COMEDY",
    parts: [{ position: 1, body: "I came back for the cat." }],
  };
  const question = () => screen.queryByText("This draft is empty. Delete it?");

  /** The writer's draft for the day until it is deleted; ListMine and Get read the same store. */
  function draftStore() {
    let live = true;
    const routes: Record<string, ApiHandler> = {
      "PlaceService/GetPlace": getPlace,
      "PerspectiveService/ListMine": (body) =>
        ok({ perspectives: live && body.status === "PERSPECTIVE_STATUS_DRAFT" && body.localDate === stored.localDate ? [stored] : [] }),
      "PerspectiveService/Get": () => (live ? ok({ perspective: stored }) : apiError("not_found", "story not found")),
      "PerspectiveService/Delete": () => {
        live = false;
        return ok({});
      },
    };
    return routes;
  }

  it("asks once every part of a saved draft is empty, saves nothing, and leaves focus in the part", async () => {
    const user = userEvent.setup();
    const api = installApi(draftStore());
    renderRoute("/compose?perspectiveId=d-old");
    const part = await screen.findByRole("textbox", { name: "Part 1" });
    expect(part).toHaveValue("I came back for the cat.");
    expect(question()).toBeNull();

    await user.clear(part);
    expect(question()).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Delete" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Keep writing" })).toBeInTheDocument();
    expect(part).toHaveFocus();
    // Announced as it appears, from a region that was already there.
    expect(question()?.closest("[aria-live='polite']")).not.toBeNull();
    await new Promise((r) => setTimeout(r, AUTOSAVE_DEBOUNCE_MS + 300));
    expect(api.callsTo("PerspectiveService/SaveDraft")).toHaveLength(0);
    expect(api.callsTo("PerspectiveService/Delete")).toHaveLength(0);
  });

  it("puts the question away on Keep writing, and asks again only after the parts are emptied again", async () => {
    const user = userEvent.setup();
    installApi({ ...draftStore(), "PerspectiveService/SaveDraft": (body) => draftReply("d-old", body) });
    renderRoute("/compose?perspectiveId=d-old");
    const part = await screen.findByRole("textbox", { name: "Part 1" });
    await user.clear(part);
    await user.click(screen.getByRole("button", { name: "Keep writing" }));
    expect(question()).toBeNull();
    expect(part).toHaveFocus();

    await user.type(part, "x");
    expect(question()).toBeNull();
    await user.clear(part);
    expect(question()).toBeInTheDocument();
  });

  it("deletes the draft and opens the composer clean on the same place, day and hour", async () => {
    const user = userEvent.setup();
    const api = installApi(draftStore());
    const router = renderRoute("/compose?placeId=p-ny&date=2026-10-01&hour=14");
    const part = await screen.findByRole("textbox", { name: "Part 1" });
    expect(part).toHaveValue("I came back for the cat.");
    await waitFor(() => expect(router.state.location.search).toContain("perspectiveId=d-old"));

    await user.clear(part);
    await user.click(screen.getByRole("button", { name: "Delete" }));

    const notice = await screen.findByText("Your empty draft is deleted.");
    expect(api.callsTo("PerspectiveService/Delete")).toEqual([
      { procedure: "PerspectiveService/Delete", body: { perspectiveId: "d-old" } },
    ]);
    // The button that had focus is gone, so the notice takes it.
    expect(notice.closest("p")).toHaveFocus();
    const params = new URLSearchParams(router.state.location.search);
    expect(params.get("perspectiveId")).toBeNull();
    expect(params.get("placeId")).toBe("p-ny");
    expect(params.get("date")).toBe("2026-10-01");
    expect(params.get("hour")).toBe("14");
    expect(screen.getByRole("textbox", { name: "Part 1" })).toHaveValue("");
    expect(screen.getByLabelText("Day of your visit")).toHaveValue("2026-10-01");
    expect(screen.getByRole("combobox", { name: "Hour" })).toHaveTextContent("14:00");
    expect(question()).toBeNull();
    expect(screen.queryByText(/open again/)).toBeNull();
    expect(screen.getByRole("status", { name: "Save status" })).toHaveTextContent("Your draft saves once a part has text.");
  });

  it("names the day in the address when the draft was opened by its id alone", async () => {
    const user = userEvent.setup();
    installApi(draftStore());
    const router = renderRoute("/compose?perspectiveId=d-old");
    await user.clear(await screen.findByRole("textbox", { name: "Part 1" }));
    await user.click(screen.getByRole("button", { name: "Delete" }));
    expect(await screen.findByText("Your empty draft is deleted.")).toBeInTheDocument();
    expect(router.state.location.search).toBe("?placeId=p-ny&date=2026-10-01&hour=14");
    expect(screen.getByRole("textbox", { name: "Part 1" })).toHaveValue("");
  });

  it("says so and keeps the question when the delete fails", async () => {
    const user = userEvent.setup();
    const api = installApi({ ...draftStore(), "PerspectiveService/Delete": () => apiError("unavailable") });
    const router = renderRoute("/compose?perspectiveId=d-old");
    await user.clear(await screen.findByRole("textbox", { name: "Part 1" }));
    await user.click(screen.getByRole("button", { name: "Delete" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Not deleted. Try again in a moment.");
    expect(api.callsTo("PerspectiveService/Delete")).toHaveLength(1);
    expect(question()).toBeInTheDocument();
    expect(router.state.location.search).toBe("?perspectiveId=d-old");
  });

  it("never asks about a draft that was never saved", async () => {
    const user = userEvent.setup();
    await openComposer("/compose?placeId=p-ny&date=2026-10-01");
    const part = screen.getByRole("textbox", { name: "Part 1" });
    await user.type(part, "Gone");
    await user.clear(part);
    expect(question()).toBeNull();
  });

  it("never asks about a published story whose parts are cleared", async () => {
    const user = userEvent.setup();
    installApi({ "PlaceService/GetPlace": getPlace, "PerspectiveService/Get": () => ok({ perspective: publishedPerspective() }) });
    renderRoute("/compose?perspectiveId=pub-1");
    await screen.findByRole("heading", { name: "Edit your story" });
    await user.clear(screen.getByRole("textbox", { name: "Part 1" }));
    expect(question()).toBeNull();
  });
});

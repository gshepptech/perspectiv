import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

import { resetSessionForTests } from "@/lib/session";

import { apiError, installApi, ok, polyfillRadix, renderRoute } from "./api.testutil";
import { LATE_EVENING_LA, nyStore, getPlace, draftReply, publishedPerspective, openComposer, pickGenre } from "./composer.testutil";

beforeAll(() => polyfillRadix());
beforeEach(() => resetSessionForTests());
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("composer date picker", () => {
  it("defaults to today in the place's time zone, not the device's", async () => {
    vi.useFakeTimers({ toFake: ["Date"], now: LATE_EVENING_LA });
    // The device really is on the previous calendar day.
    expect(new Date().getDate()).toBe(15);
    await openComposer();
    expect(screen.getByLabelText("Day of your visit")).toHaveValue("2026-10-16");
    expect(screen.getByText(/today at Corner Books is/i)).toHaveTextContent("October 16, 2026");
  });

  it("files a perspective under the day the author picks, not the publish day", async () => {
    vi.useFakeTimers({ toFake: ["Date"], now: LATE_EVENING_LA });
    const user = userEvent.setup();
    const api = await openComposer("/compose?placeId=p-ny", {
      "PerspectiveService/SaveDraft": (body) => draftReply("d-1", body),
      "PerspectiveService/Publish": () => ok({ perspectiveId: "d-1", arcId: "arc-9" }),
    });
    const date = screen.getByLabelText("Day of your visit");
    await user.clear(date);
    await user.type(date, "2026-10-15");
    await user.type(screen.getByRole("textbox", { name: "Title of the day" }), "The rainy Thursday");
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
    await waitFor(() => expect(screen.queryByRole("heading", { name: "Write a story" })).toBeNull());
  });

  it("on ALREADY_EXISTS opens the author's existing perspective for that place and day", async () => {
    const user = userEvent.setup();
    const api = await openComposer("/compose?placeId=p-ny&date=2026-10-15&hour=14", {
      "PerspectiveService/SaveDraft": (body) => draftReply("d-2", body),
      "PerspectiveService/Publish": () => apiError("already_exists", "you already published in this arc"),
      "PerspectiveService/ListMine": (body) =>
        body.status === "PERSPECTIVE_STATUS_DRAFT"
          ? ok({ perspectives: [] })
          : ok({
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

    expect(await screen.findByRole("heading", { name: "Edit your story" })).toBeInTheDocument();
    expect(api.callsTo("PerspectiveService/ListMine").find((c) => c.body.status === "PERSPECTIVE_STATUS_PUBLISHED")?.body).toMatchObject({
      status: "PERSPECTIVE_STATUS_PUBLISHED",
      placeId: "p-ny",
      localDate: "2026-10-15",
      hour: 14,
    });
    expect(api.callsTo("PerspectiveService/Get").at(-1)?.body).toEqual({ perspectiveId: "pub-1" });
    expect(screen.getByRole("textbox", { name: "Part 1" })).toHaveValue("The bell over the door rang twice.");
    expect(screen.getByText(/already published a story here/i)).toBeInTheDocument();
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
    expect(screen.getByRole("heading", { name: "Write a story" })).toBeInTheDocument();
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
    expect(await screen.findByRole("heading", { name: "Edit your story" })).toBeInTheDocument();
    expect(screen.queryByLabelText("Day of your visit")).toBeNull();
    expect(screen.getByText(/2026-10-15, 14:00/)).toBeInTheDocument();
    expect(screen.queryByRole("textbox", { name: "Title of the day" })).toBeNull();

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
    expect(await screen.findByRole("heading", { name: "Write a story" })).toBeInTheDocument();
    expect(screen.getByLabelText("Day of your visit")).toHaveValue("2026-10-12");
    expect(screen.getByRole("textbox", { name: "Title of the day" })).toHaveValue("Closing time");
    expect(screen.getByRole("combobox", { name: "Genre" })).toHaveTextContent("Romance");
    expect(screen.getByRole("textbox", { name: "Part 1" })).toHaveValue("Draft text");
  });
});

describe("writing through an invite", () => {
  it("sends the invite code with the publish and lands on the arc", async () => {
    const user = userEvent.setup();
    const api = await openComposer("/compose?placeId=p-ny&date=2026-10-01&invite=abcdefgh23", {
      "PerspectiveService/SaveDraft": (body) => draftReply("d-1", body),
      "PerspectiveService/Publish": () => ok({ perspectiveId: "d-1", arcId: "arc-9" }),
      "ArcService/GetArc": () => ok({}),
    });
    await user.type(screen.getByRole("textbox", { name: "Title of the day" }), "Open mic night");
    await pickGenre(user, "Drama");
    await user.type(screen.getByRole("textbox", { name: "Part 1" }), "I was the one singing.");
    await user.click(screen.getByRole("button", { name: "Publish" }));
    await waitFor(() => expect(api.callsTo("PerspectiveService/Publish")).toHaveLength(1));
    expect(api.callsTo("PerspectiveService/Publish")[0]?.body).toMatchObject({ inviteCode: "abcdefgh23" });
    await waitFor(() => expect(api.callsTo("ArcService/GetArc").length).toBeGreaterThan(0));
  });
});

describe("composer: who already wrote this day", () => {
  it("says the day's title and where the others are, and asks for no title", async () => {
    const user = userEvent.setup();
    await openComposer("/compose?placeId=p-ny", {
      "ArcService/FindDayArc": () =>
        ok({
          arcId: "arc-1",
          title: "Bucs comeback in the rain",
          perspectiveCount: 2,
          hours: [{ hour: 13, arcId: "arc-13", title: "Bucs comeback in the rain · 13:00", perspectiveCount: 1 }],
        }),
    });
    expect(await screen.findByText(/This day is/)).toHaveTextContent(
      "This day is “Bucs comeback in the rain”: 2 stories for the whole day, 1 story at 13:00.",
    );
    expect(screen.getByText(/Writing for the whole day puts your story with theirs/)).toBeInTheDocument();
    expect(screen.queryByLabelText("Title of the day")).not.toBeInTheDocument();
    await user.click(screen.getByRole("combobox", { name: "Hour" }));
    await user.click(await screen.findByRole("option", { name: "13:00" }));
    expect(await screen.findByText(/You join 13:00/)).toBeInTheDocument();
  });

  it("asks for a title when nobody has written that day", async () => {
    await openComposer("/compose?placeId=p-ny", { "ArcService/FindDayArc": () => ok({}) });
    expect(await screen.findByText(/Nobody has written about Corner Books on this day yet/)).toBeInTheDocument();
    expect(screen.getByLabelText("Title of the day")).toBeInTheDocument();
  });
});

describe("composer: one story per day, and writing your side", () => {
  it("warns before typing when the writer already has a story in that arc, and offers it", async () => {
    await openComposer("/compose?placeId=p-ny", {
      "ArcService/FindDayArc": () => ok({ arcId: "arc-1", title: "Rain on the square", perspectiveCount: 2, minePerspectiveId: "p-mine" }),
    });
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("You already have a story for this day here");
    expect(within(alert).getByRole("link", { name: "Edit yours" })).toHaveAttribute("href", "/compose?perspectiveId=p-mine");
    expect(screen.getByRole("button", { name: "Publish" })).toBeDisabled();
    expect(screen.getByText(/Held: you already have a story/)).toBeInTheDocument();
  });

  it("fixes the day and hour to the story being joined, and says why", async () => {
    await openComposer(`/compose?placeId=p-ny&date=2026-10-11&hour=13&return=${encodeURIComponent("/perspectives/p-otter?claim=1")}`, {
      "ArcService/FindDayArc": () => ok({}),
    });
    expect(await screen.findByText(/Write your side of this day/)).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 1, name: "Write a story" })).toHaveFocus();
    // The day and hour are theirs, shown as text, not as controls.
    expect(screen.queryByLabelText("Day of your visit")).not.toBeInTheDocument();
    expect(screen.queryByRole("combobox", { name: "Hour" })).not.toBeInTheDocument();
    expect(screen.getByText(/October 11, 2026, 13:00/)).toBeInTheDocument();
    expect(screen.getByText(/Set to match their story/)).toBeInTheDocument();
  });
});

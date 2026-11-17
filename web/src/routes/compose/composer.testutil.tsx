import { screen } from "@testing-library/react";
import type userEvent from "@testing-library/user-event";

import { installApi, ok, renderRoute, type ApiHandler } from "./api.testutil";

// Fixtures and steps shared by the composer's page tests. Only tests import it.

// The author's device is in Los Angeles; the store is in New York.
process.env.TZ = "America/Los_Angeles";

// 00:30 on 16 October in New York is still 21:30 on 15 October in Los Angeles.
export const LATE_EVENING_LA = new Date("2026-10-16T04:30:00Z");

export const nyStore = {
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

export const getPlace: ApiHandler = () => ok({ place: nyStore });

export function draftReply(id: string, body: Record<string, unknown>) {
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

export function publishedPerspective(over: Record<string, unknown> = {}) {
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

export async function openComposer(path = "/compose?placeId=p-ny", routes: Record<string, ApiHandler> = {}) {
  const api = installApi({ "PlaceService/GetPlace": getPlace, ...routes });
  renderRoute(path);
  await screen.findByRole("heading", { name: "Write a story" });
  return api;
}

export async function pickGenre(user: ReturnType<typeof userEvent.setup>, name: string) {
  await user.click(screen.getByRole("combobox", { name: "Genre" }));
  await user.click(await screen.findByRole("option", { name }));
}

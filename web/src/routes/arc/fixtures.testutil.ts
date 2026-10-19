import { ME } from "@/components/actions/testing.testutil";

/** Connect JSON fixtures for an arc page, shaped as ArcService/GetArc answers. */

export const user = (userId: string, username: string) => ({ userId, username });

export const WREN = user(ME, "wren");
export const OTTER = user("u-otter", "otter");
export const HERON = user("u-heron", "heron");
export const LARK = user("u-lark", "lark");
export const MOTH = user("u-moth", "moth");
export const FINCH = user("u-finch", "finch");
export const VOLE = user("u-vole", "vole");

type Summary = {
  id: string;
  author?: { userId: string; username: string };
  loreStatus?: string;
  loreId?: string;
  excerpt?: string;
  upvotes?: number;
  deleted?: boolean;
  hidden?: boolean;
  arcId?: string;
};

export function summary(s: Summary) {
  return {
    id: s.id,
    arcId: s.arcId ?? "arc-day",
    author: s.author,
    genre: "GENRE_DRAMA",
    loreStatus: s.loreStatus ?? "LORE_STATUS_IN_LORE",
    loreId: s.loreId ?? "",
    excerpt: s.excerpt ?? `Opening of ${s.id}`,
    partCount: 3,
    upvotes: s.upvotes ?? 0,
    publishedAt: "2026-10-02T10:00:00Z",
    deleted: s.deleted ?? false,
    hidden: s.hidden ?? false,
  };
}

export function lore(id: string, standing: "CANON" | "SUBLORE", members: { perspectiveId: string; user: { userId: string; username: string } }[], upvotes = 0) {
  return {
    id,
    arcId: "arc-day",
    standing: `LORE_STANDING_${standing}`,
    memberCount: members.length,
    upvotes,
    rankScore: 1.5,
    computedAt: "2026-10-02T10:00:00Z",
    members,
    encounters: [],
  };
}

const place = { id: "pl-store", kind: "PLACE_KIND_STORE", name: "Moonbean Coffee" };

export function arc(over: Record<string, unknown> = {}) {
  return {
    id: "arc-day",
    title: "Closing night at Moonbean",
    place,
    placeChain: [
      { id: "pl-state", kind: "PLACE_KIND_STATE", name: "Oregon" },
      { id: "pl-city", kind: "PLACE_KIND_CITY", name: "Portland" },
      place,
    ],
    localDate: "2026-10-01",
    parentArcId: "",
    canonLoreId: "lore-canon",
    canonScore: 2.4,
    memberCount: 4,
    createdBy: OTTER,
    createdAt: "2026-10-01T20:00:00Z",
    ...over,
  };
}

/**
 * A day arc where wren (the signed-in user in tests) is a member of the
 * 4-member canon lore, a demoted former canon sits in the sublores, and the
 * tales hold a SOLO and a REMOVED perspective.
 */
export function dayArcResponse() {
  const canonMembers = [
    { perspectiveId: "p-wren", user: WREN },
    { perspectiveId: "p-otter", user: OTTER },
    { perspectiveId: "p-heron", user: HERON },
    { perspectiveId: "p-lark", user: LARK },
  ];
  const subMembers = [
    { perspectiveId: "p-moth", user: MOTH },
    { perspectiveId: "p-finch", user: FINCH },
    { perspectiveId: "p-vole", user: VOLE },
  ];
  return {
    arc: arc(),
    // Where the server says the signed-in user (wren) stands: in the canon.
    viewer: { perspectiveId: "p-wren", loreId: "lore-canon" },
    canon: {
      lore: lore("lore-canon", "CANON", canonMembers, 9),
      perspectives: canonMembers.map((m) =>
        summary({ id: m.perspectiveId, author: m.user, loreId: "lore-canon", excerpt: `Canon text by ${m.user.username}`, upvotes: 2 }),
      ),
    },
    sublores: [
      {
        // The former canon, demoted after a vote-out took it to 3 members.
        lore: lore("lore-former", "SUBLORE", subMembers, 20),
        perspectives: subMembers.map((m) =>
          summary({ id: m.perspectiveId, author: m.user, loreId: "lore-former", excerpt: `Demoted text by ${m.user.username}` }),
        ),
      },
    ],
    tales: [
      summary({ id: "p-solo", author: user("u-solo", "solo_teller"), loreStatus: "LORE_STATUS_SOLO", excerpt: "A tale nobody claimed" }),
      summary({ id: "p-removed", author: user("u-gone", "voted_out"), loreStatus: "LORE_STATUS_REMOVED", excerpt: "A story the lore voted out" }),
    ],
    unlinked: [summary({ id: "p-new", author: user("u-new", "newcomer"), loreStatus: "LORE_STATUS_PENDING", excerpt: "Just published" })],
    hours: [
      {
        arc: arc({ id: "arc-h15", title: "The afternoon rush", hour: 15, parentArcId: "arc-day", canonLoreId: "lore-h15" }),
        canon: {
          lore: lore("lore-h15", "CANON", [{ perspectiveId: "p-h15", user: FINCH }]),
          perspectives: [summary({ id: "p-h15", author: FINCH, arcId: "arc-h15", excerpt: "Hour fifteen canon text" })],
        },
        perspectiveCount: 5,
      },
      {
        arc: arc({ id: "arc-h9", title: "The morning queue", hour: 9, parentArcId: "arc-day", canonLoreId: "" }),
        perspectiveCount: 1,
      },
      {
        arc: arc({ id: "arc-h11", title: "Late breakfast", hour: 11, parentArcId: "arc-day", canonLoreId: "lore-h11" }),
        canon: {
          lore: lore("lore-h11", "CANON", [{ perspectiveId: "p-h11", user: MOTH }]),
          perspectives: [summary({ id: "p-h11", author: MOTH, arcId: "arc-h11", excerpt: "Hour eleven canon text" })],
        },
        perspectiveCount: 4,
      },
    ],
  };
}

/** A full Perspective as PerspectiveService/Get answers it. */
export function perspective(id: string, over: Record<string, unknown> = {}) {
  return {
    perspective: {
      id,
      arcId: "arc-day",
      arcTitle: "Closing night at Moonbean",
      genre: "GENRE_DRAMA",
      status: "PERSPECTIVE_STATUS_PUBLISHED",
      loreStatus: "LORE_STATUS_IN_LORE",
      parts: [{ position: 1, body: "text" }],
      characters: [],
      upvotes: 2,
      voted: false,
      reacts: ["EMOJI_HEART", "EMOJI_LAUGH", "EMOJI_WOW", "EMOJI_SAD", "EMOJI_FIRE", "EMOJI_CLAP"].map((emoji) => ({
        emoji,
        count: 0,
        mine: false,
      })),
      deleted: false,
      hidden: false,
      ...over,
    },
  };
}

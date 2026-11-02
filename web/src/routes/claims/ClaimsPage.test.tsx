import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { resetSessionForTests } from "@/lib/session";
import { apiError, installApi, ok, renderRoute, type ApiHandler } from "@/routes/compose/api.testutil";

beforeEach(() => resetSessionForTests());
afterEach(() => vi.unstubAllGlobals());

function claim(over: Record<string, unknown> = {}) {
  return {
    id: "cl-1",
    arcId: "arc-1",
    characterId: "c-1",
    characterLabel: "the clerk with the green scarf",
    perspectiveId: "p-mine",
    author: { userId: "u-me", username: "wren" },
    claimant: { userId: "u-them", username: "marlo" },
    claimantPerspectiveId: "p-theirs",
    status: "CLAIM_STATUS_PENDING",
    createdAt: "2026-10-14T18:00:00Z",
    placeName: "UCF Stadium",
    localDate: "2026-10-03",
    ...over,
  };
}

function listClaims(pages: Record<string, Record<string, unknown>[][]>): ApiHandler {
  return (body) => {
    const dir = String(body.direction);
    const list = pages[dir] ?? [[]];
    const page = body.pageToken ? Number(body.pageToken) : 0;
    const next = page + 1 < list.length ? String(page + 1) : "";
    return ok({ claims: list[page] ?? [], nextPageToken: next });
  };
}

describe("claims inbox", () => {
  it("lists claims on the author's characters with usernames only, and names the story each is about", async () => {
    const api = installApi({
      "LoreService/ListClaims": listClaims({ CLAIM_DIRECTION_INCOMING: [[claim({ claimantExcerpt: "I wore the green scarf." })]] }),
    });
    renderRoute("/claims");
    const item = await screen.findByRole("listitem", { name: /marlo/ });
    expect(item).toHaveTextContent("marlo says they are “the clerk with the green scarf”");
    expect(item).toHaveTextContent("Pending");
    expect(within(item).getByRole("link", { name: "About your story at UCF Stadium, October 3, 2026" })).toHaveAttribute(
      "href",
      "/perspectives/p-mine",
    );
    expect(within(item).queryByRole("link", { name: "Your story" })).toBeNull();
    expect(within(item).getByRole("link", { name: "Read their story" })).toHaveAttribute("href", "/perspectives/p-theirs");
    expect(api.callsTo("LoreService/ListClaims")[0]?.body).toMatchObject({ direction: "CLAIM_DIRECTION_INCOMING" });
  });

  it("quotes the claimant's story on an incoming request, so it can be judged here", async () => {
    const excerpt =
      "The power went out halfway through open mic. Someone by the window started singing anyway, and by the second verse the whole room had joined in. I was the one in the yellow raincoat, and I still do not know why I…";
    installApi({
      "LoreService/ListClaims": listClaims({ CLAIM_DIRECTION_INCOMING: [[claim({ claimantExcerpt: excerpt })]] }),
    });
    renderRoute("/claims");
    const item = await screen.findByRole("listitem", { name: /marlo/ });
    const quote = item.querySelector("blockquote");
    expect(quote).toHaveTextContent(excerpt);
    expect(within(item).getByRole("link", { name: "Read their story" })).toHaveAttribute("href", "/perspectives/p-theirs");
    expect(item).not.toHaveTextContent("can no longer be read");
  });

  it("says when the claimant's story can no longer be read, and offers no link to it", async () => {
    installApi({
      "LoreService/ListClaims": listClaims({ CLAIM_DIRECTION_INCOMING: [[claim({ claimantExcerpt: "" })]] }),
    });
    renderRoute("/claims");
    const item = await screen.findByRole("listitem", { name: /marlo/ });
    expect(item).toHaveTextContent("Their story can no longer be read.");
    expect(item.querySelector("blockquote")).toBeNull();
    expect(within(item).queryByRole("link", { name: "Read their story" })).toBeNull();
  });

  it("accepts a pending claim and says what comes next: the lore to read and who else to invite", async () => {
    const user = userEvent.setup();
    let status = "CLAIM_STATUS_PENDING";
    let loreId = "";
    const api = installApi({
      "LoreService/ListClaims": () => ok({ claims: [claim({ status })] }),
      "LoreService/ResolveClaim": () => {
        status = "CLAIM_STATUS_ACCEPTED";
        loreId = "lore-9";
        return ok({ status });
      },
      "PerspectiveService/Get": () => ok({ perspective: { id: "p-mine", arcId: "arc-1", loreId } }),
    });
    renderRoute("/claims");
    const item = await screen.findByRole("listitem", { name: /marlo/ });
    await user.click(within(item).getByRole("button", { name: "Accept" }));
    const linked = await within(item).findByRole("status");
    expect(linked).toHaveTextContent("Linked. You are both in this story's lore now: read it");
    await waitFor(() => expect(within(linked).getByRole("link", { name: "read it" })).toHaveAttribute("href", "/lores/lore-9"));
    expect(item).toHaveTextContent("A lore needs 4 members to compete for canon.");
    expect(within(item).getByRole("link", { name: "Invite who else was there" })).toHaveAttribute(
      "href",
      "/perspectives/p-mine?invite=1",
    );
    expect(api.callsTo("LoreService/ResolveClaim")[0]?.body).toEqual({ claimId: "cl-1", accept: true });
    expect(within(item).queryByRole("button", { name: "Accept" })).toBeNull();
    // The button went with the decision; focus lands on what replaced it.
    await waitFor(() => expect(linked).toHaveFocus());
  });

  it("names an hour's story by its hour", async () => {
    installApi({ "LoreService/ListClaims": listClaims({ CLAIM_DIRECTION_INCOMING: [[claim({ hour: 21 })]] }) });
    renderRoute("/claims");
    const item = await screen.findByRole("listitem", { name: /marlo/ });
    expect(within(item).getByRole("link", { name: "About your story at UCF Stadium, October 3, 2026, 21:00" })).toBeInTheDocument();
  });

  it("after accepting, offers the claimant's characters still open to the reader, each with That was me", async () => {
    const user = userEvent.setup();
    let status = "CLAIM_STATUS_PENDING";
    const character = (id: string, label: string, over: Record<string, unknown> = {}) => ({ id, position: 1, label, ...over });
    installApi({
      "LoreService/ListClaims": () => ok({ claims: [claim({ status })] }),
      "LoreService/ResolveClaim": () => {
        status = "CLAIM_STATUS_ACCEPTED";
        return ok({ status });
      },
      "PerspectiveService/Get": (body) =>
        body.perspectiveId === "p-theirs"
          ? ok({
              perspective: {
                id: "p-theirs",
                arcId: "arc-1",
                characters: [
                  character("c-open", "the woman with the clipboard"),
                  character("c-taken", "the usher", { hasAcceptedClaim: true }),
                  character("c-asked", "the drummer", { hasPendingClaim: true }),
                  character("c-refused", "the mascot", { myClaim: "CLAIM_STATUS_REJECTED" }),
                ],
              },
            })
          : ok({ perspective: { id: "p-mine", arcId: "arc-1", loreId: "lore-9" } }),
    });
    renderRoute("/claims");
    const item = await screen.findByRole("listitem", { name: /marlo/ });
    expect(within(item).queryByText("Are you in marlo's story too?")).toBeNull();
    await user.click(within(item).getByRole("button", { name: "Accept" }));
    const open = await within(item).findByRole("list", { name: "Are you in marlo's story too?" });
    const rows = within(open).getAllByRole("listitem");
    expect(rows).toHaveLength(1);
    expect(rows[0]).toHaveTextContent("“the woman with the clipboard”");
    expect(within(open).getByRole("link", { name: "That was me: the woman with the clipboard" })).toHaveAttribute(
      "href",
      "/perspectives/p-theirs?claim=c-open",
    );
  });

  it("points at the day while the lore is not known yet", async () => {
    const user = userEvent.setup();
    let status = "CLAIM_STATUS_PENDING";
    installApi({
      "LoreService/ListClaims": () => ok({ claims: [claim({ status })] }),
      "LoreService/ResolveClaim": () => {
        status = "CLAIM_STATUS_ACCEPTED";
        return ok({ status });
      },
      "PerspectiveService/Get": () => apiError("unavailable", "down"),
    });
    renderRoute("/claims");
    const item = await screen.findByRole("listitem", { name: /marlo/ });
    await user.click(within(item).getByRole("button", { name: "Accept" }));
    const linked = await within(item).findByRole("status");
    expect(within(linked).getByRole("link", { name: "read it" })).toHaveAttribute("href", "/arcs/arc-1");
  });

  it("is headed Requests, like its navigation item", async () => {
    installApi({ "LoreService/ListClaims": listClaims({}) });
    renderRoute("/claims");
    expect(await screen.findByRole("heading", { level: 1, name: "Requests" })).toBeInTheDocument();
  });

  it("asks before rejecting, can be backed out of with Escape, and rejects on confirm", async () => {
    const user = userEvent.setup();
    let status = "CLAIM_STATUS_PENDING";
    const api = installApi({
      "LoreService/ListClaims": () => ok({ claims: [claim({ status })] }),
      "LoreService/ResolveClaim": () => {
        status = "CLAIM_STATUS_REJECTED";
        return ok({ status });
      },
    });
    renderRoute("/claims");
    const item = await screen.findByRole("listitem", { name: /marlo/ });
    await user.click(within(item).getByRole("button", { name: "Reject" }));
    const dialog = await screen.findByRole("dialog", { name: "Reject this claim?" });
    expect(dialog).toHaveTextContent("marlo will not be able to claim “the clerk with the green scarf” again.");
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(api.callsTo("LoreService/ResolveClaim")).toHaveLength(0);

    await user.click(within(item).getByRole("button", { name: "Reject" }));
    await user.click(await screen.findByRole("button", { name: "Reject claim" }));
    await waitFor(() => expect(screen.getByRole("listitem", { name: /marlo/ })).toHaveTextContent("Rejected"));
    await waitFor(() => expect(within(item).getByText("Rejected").closest("[tabindex]")).toHaveFocus());
    // accept=false is the proto3 default, so the JSON body carries only the id.
    expect(api.callsTo("LoreService/ResolveClaim")[0]?.body).toEqual({ claimId: "cl-1" });
  });

  it("shows why a claim could not be resolved", async () => {
    const user = userEvent.setup();
    installApi({
      "LoreService/ListClaims": () => ok({ claims: [claim()] }),
      "LoreService/ResolveClaim": () => apiError("failed_precondition", "claim is no longer pending"),
    });
    renderRoute("/claims");
    const item = await screen.findByRole("listitem", { name: /marlo/ });
    await user.click(within(item).getByRole("button", { name: "Accept" }));
    expect(await within(item).findByRole("alert")).toHaveTextContent("claim is no longer pending");
  });

  it("switches to the author's own claims from the keyboard", async () => {
    const user = userEvent.setup();
    const api = installApi({
      "LoreService/ListClaims": listClaims({
        CLAIM_DIRECTION_INCOMING: [[]],
        CLAIM_DIRECTION_OUTGOING: [
          [
            claim({
              id: "cl-2",
              author: { userId: "u-other", username: "juniper" },
              claimant: { userId: "u-me", username: "wren" },
              status: "CLAIM_STATUS_ACCEPTED",
            }),
          ],
        ],
      }),
    });
    renderRoute("/claims");
    expect(await screen.findByText("No one has claimed your characters yet.")).toBeInTheDocument();
    const incomingTab = screen.getByRole("tab", { name: "On your characters" });
    incomingTab.focus();
    await user.keyboard("{ArrowRight}");
    const outgoingTab = screen.getByRole("tab", { name: "Your claims" });
    expect(outgoingTab).toHaveAttribute("aria-selected", "true");
    const item = await screen.findByRole("listitem", { name: /juniper/ });
    expect(item).toHaveTextContent("You said you are “the clerk with the green scarf” in juniper's story");
    expect(item).toHaveTextContent("Accepted");
    expect(within(item).getByRole("link", { name: "About juniper's story at UCF Stadium, October 3, 2026" })).toHaveAttribute(
      "href",
      "/perspectives/p-mine",
    );
    expect(within(item).getByRole("link", { name: "Your story of that day" })).toHaveAttribute("href", "/perspectives/p-theirs");
    expect(within(item).queryByRole("button")).toBeNull();
    expect(api.callsTo("LoreService/ListClaims").at(-1)?.body).toMatchObject({ direction: "CLAIM_DIRECTION_OUTGOING" });
  });

  it("pages with the server's token", async () => {
    const user = userEvent.setup();
    const api = installApi({
      "LoreService/ListClaims": listClaims({
        CLAIM_DIRECTION_INCOMING: [
          [claim()],
          [claim({ id: "cl-9", claimant: { userId: "u-x", username: "ottoline" } })],
        ],
      }),
    });
    renderRoute("/claims");
    await screen.findByRole("listitem", { name: /marlo/ });
    await user.click(screen.getByRole("button", { name: "Show more" }));
    expect(await screen.findByRole("listitem", { name: /ottoline/ })).toBeInTheDocument();
    expect(screen.getByRole("listitem", { name: /marlo/ })).toBeInTheDocument();
    expect(api.callsTo("LoreService/ListClaims").at(-1)?.body).toMatchObject({ pageToken: "1" });
    expect(screen.queryByRole("button", { name: "Show more" })).toBeNull();
  });

  it("says so when the claims cannot be loaded", async () => {
    installApi({ "LoreService/ListClaims": () => apiError("permission_denied", "nope") });
    renderRoute("/claims");
    expect(await screen.findByRole("alert")).toHaveTextContent("Claims could not be loaded: nope");
  });
});

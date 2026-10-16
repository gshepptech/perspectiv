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
  it("lists claims on the author's characters with usernames only, and links both perspectives", async () => {
    const api = installApi({
      "LoreService/ListClaims": listClaims({ CLAIM_DIRECTION_INCOMING: [[claim()]] }),
    });
    renderRoute("/claims");
    const item = await screen.findByRole("listitem", { name: /marlo/ });
    expect(item).toHaveTextContent("marlo says they are “the clerk with the green scarf”");
    expect(item).toHaveTextContent("Pending");
    expect(within(item).getByRole("link", { name: "Your perspective" })).toHaveAttribute("href", "/perspectives/p-mine");
    expect(within(item).getByRole("link", { name: "Their perspective" })).toHaveAttribute(
      "href",
      "/perspectives/p-theirs",
    );
    expect(api.callsTo("LoreService/ListClaims")[0]?.body).toMatchObject({ direction: "CLAIM_DIRECTION_INCOMING" });
  });

  it("accepts a pending claim and shows the new status", async () => {
    const user = userEvent.setup();
    let status = "CLAIM_STATUS_PENDING";
    const api = installApi({
      "LoreService/ListClaims": () => ok({ claims: [claim({ status })] }),
      "LoreService/ResolveClaim": () => {
        status = "CLAIM_STATUS_ACCEPTED";
        return ok({ status });
      },
    });
    renderRoute("/claims");
    const item = await screen.findByRole("listitem", { name: /marlo/ });
    await user.click(within(item).getByRole("button", { name: "Accept" }));
    await waitFor(() => expect(screen.getByRole("listitem", { name: /marlo/ })).toHaveTextContent("Accepted"));
    expect(api.callsTo("LoreService/ResolveClaim")[0]?.body).toEqual({ claimId: "cl-1", accept: true });
    expect(within(screen.getByRole("listitem", { name: /marlo/ })).queryByRole("button", { name: "Accept" })).toBeNull();
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
    expect(item).toHaveTextContent("You said you are “the clerk with the green scarf” in juniper's perspective");
    expect(item).toHaveTextContent("Accepted");
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

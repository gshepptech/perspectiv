import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { resetSessionForTests } from "@/lib/session";
import { installApi, ok, polyfillRadix, renderRoute } from "@/routes/compose/api.testutil";

beforeEach(() => {
  resetSessionForTests();
  polyfillRadix();
});
afterEach(() => vi.unstubAllGlobals());

const admin = { role: "ROLE_ADMIN", username: "warden" };

function code(over: Record<string, unknown> = {}) {
  return { code: "ucf-launch", note: "launch night", maxUses: 25, uses: 3, createdAt: "2026-10-20T12:00:00Z", revoked: false, ...over };
}

describe("beta access admin", () => {
  it("tells a non-admin it is for site admins and never asks the server", async () => {
    const api = installApi({});
    renderRoute("/admin/access");
    expect(await screen.findByText("Only site admins can manage beta access.")).toBeInTheDocument();
    expect(api.callsTo("AuthService/ListAccessCodes")).toHaveLength(0);
  });

  it("lists codes with their standing in words, and creates one", async () => {
    const user = userEvent.setup();
    let codes = [
      code(),
      code({ code: "used-up", uses: 25, maxUses: 25 }),
      code({ code: "gone-code", revoked: true }),
      code({ code: "old-code", expiresAt: "2020-01-01T00:00:00Z" }),
    ];
    const api = installApi(
      {
        "AuthService/ListAccessCodes": () => ok({ codes }),
        "AuthService/CreateAccessCode": (body) => {
          const made = code({ code: String(body.code) || "beta-abcd2345", note: body.note, maxUses: body.maxUses, uses: 0 });
          codes = [made, ...codes];
          return ok({ code: made });
        },
      },
      admin,
    );
    renderRoute("/admin/access");
    const list = await screen.findByRole("list", { name: "Beta codes" });
    const items = within(list).getAllByRole("listitem");
    expect(items.map((i) => within(i).getByText(/^(Live|Used up|Revoked|Expired)$/).textContent)).toEqual(["Live", "Used up", "Revoked", "Expired"]);
    expect(items[0]).toHaveTextContent("3 of 25 used");
    expect(within(items[0]!).getByRole("button", { name: /^Copy invite link/ })).toBeInTheDocument();
    expect(within(items[1]!).queryByRole("button", { name: "Revoke" })).toBeNull();

    await user.clear(screen.getByLabelText("Uses"));
    await user.click(screen.getByRole("button", { name: "Create code" }));
    expect(await screen.findByText("1 to 10,000.")).toBeInTheDocument();
    await user.type(screen.getByLabelText("Uses"), "50");
    await user.type(screen.getByLabelText("Code"), "Gator-Night");
    await user.type(screen.getByLabelText("Note"), " Florida launch ");
    await user.type(screen.getByLabelText("Expires in days"), "30");
    await user.click(screen.getByRole("button", { name: "Create code" }));
    expect(await screen.findByText("Created gator-night.")).toBeInTheDocument();
    expect(api.callsTo("AuthService/CreateAccessCode")[0]?.body).toEqual({
      code: "gator-night",
      note: "Florida launch",
      maxUses: 50,
      expiresInDays: 30,
    });
  });

  it("revokes a live code after confirming", async () => {
    const user = userEvent.setup();
    let revoked = false;
    const api = installApi(
      {
        "AuthService/ListAccessCodes": () => ok({ codes: [code({ revoked })] }),
        "AuthService/RevokeAccessCode": () => {
          revoked = true;
          return ok({ code: code({ revoked: true }) });
        },
      },
      admin,
    );
    renderRoute("/admin/access");
    await user.click(await screen.findByRole("button", { name: "Revoke ucf-launch" }));
    const dialog = await screen.findByRole("dialog", { name: "Revoke ucf-launch?" });
    await user.click(within(dialog).getByRole("button", { name: "Revoke code" }));
    expect(await screen.findByText("Revoked")).toBeInTheDocument();
    expect(api.callsTo("AuthService/RevokeAccessCode")[0]?.body).toEqual({ code: "ucf-launch" });
  });

  it("sends a waiting person an invite and moves them to Invited", async () => {
    const user = userEvent.setup();
    let invited = false;
    const entry = () => ({
      id: "w-1",
      emailHint: "pa•••@example.test",
      note: "UCF games",
      createdAt: "2026-10-21T12:00:00Z",
      ...(invited ? { invitedAt: "2026-10-22T12:00:00Z", code: "wl-abcd2345" } : {}),
    });
    const api = installApi(
      {
        "AuthService/ListAccessCodes": () => ok({ codes: [] }),
        "AuthService/ListWaitlist": () => ok({ entries: [entry()] }),
        "AuthService/InviteFromWaitlist": () => {
          invited = true;
          return ok({ entry: entry() });
        },
      },
      admin,
    );
    renderRoute("/admin/access");
    await user.click(await screen.findByRole("tab", { name: "Waitlist" }));
    const waiting = await screen.findByRole("list", { name: "Waiting" });
    expect(waiting).toHaveTextContent("pa•••@example.test");
    await user.click(within(waiting).getByRole("button", { name: /^Send invite/ }));
    const done = await screen.findByRole("list", { name: "Invited" });
    expect(done).toHaveTextContent("wl-abcd2345");
    await waitFor(() => expect(screen.getByRole("heading", { name: /Waiting \(0\)/ })).toBeInTheDocument());
    expect(api.callsTo("AuthService/InviteFromWaitlist")[0]?.body).toEqual({ id: "w-1" });
  });
});

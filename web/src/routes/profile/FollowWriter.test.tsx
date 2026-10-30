import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { connectError, sessionReply } from "@/lib/fakeApi.testutil";
import { renderApp } from "@/routes/read/renderApp.testutil";

afterEach(() => vi.unstubAllGlobals());

const wren = { userId: "u-1", username: "wren", joinedAt: "2026-03-02T15:00:00Z" };
// sessionReply signs in as this user.
const me = { userId: "0b9e0a52-5d0f-4a8e-9a43-1f0f7f3c2a10", username: "teller", joinedAt: "2026-03-02T15:00:00Z" };
const signedIn = { refresh: () => sessionReply({ token: "tok" }) };
const noStories = { "PerspectiveService/ListByAuthor": () => ({ body: { perspectives: [] } }) };

describe("following a writer from their profile", () => {
  it("a signed-in reader follows and unfollows; the toggle is pressed and the count moves", async () => {
    const user = userEvent.setup();
    let following = false;
    const { calls } = renderApp(
      "/users/wren",
      {
        ...noStories,
        "UserService/GetProfile": () => ({ body: { profile: wren, following, followers: following ? 3 : 2 } }),
        "UserService/FollowUser": (body) => {
          following = body.on === true;
          return { body: { following } };
        },
      },
      signedIn,
    );
    const follow = await screen.findByRole("button", { name: "Follow wren" });
    expect(follow).toHaveAttribute("aria-pressed", "false");
    expect(screen.getByText("2 followers")).toBeInTheDocument();
    await user.click(follow);
    expect(await screen.findByRole("button", { name: "Following wren" })).toHaveAttribute("aria-pressed", "true");
    expect(await screen.findByText("3 followers")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Following wren" }));
    expect(await screen.findByRole("button", { name: "Follow wren" })).toHaveAttribute("aria-pressed", "false");
    expect(await screen.findByText("2 followers")).toBeInTheDocument();
    expect(calls("UserService/FollowUser")).toEqual([{ username: "wren", on: true }, { username: "wren" }]);
  });

  it("sends a signed-out reader to sign in without calling the server, and shows the count", async () => {
    const user = userEvent.setup();
    const { router, calls } = renderApp("/users/wren", {
      ...noStories,
      "UserService/GetProfile": () => ({ body: { profile: wren, followers: 1 } }),
    });
    expect(await screen.findByText("1 follower")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Follow wren" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(calls("UserService/FollowUser")).toEqual([]);
  });

  it("shows the server's reason when the follow is refused", async () => {
    const user = userEvent.setup();
    renderApp(
      "/users/wren",
      {
        ...noStories,
        "UserService/GetProfile": () => ({ body: { profile: wren } }),
        "UserService/FollowUser": () => connectError("permission_denied", "you cannot follow this writer"),
      },
      signedIn,
    );
    await user.click(await screen.findByRole("button", { name: "Follow wren" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("you cannot follow this writer");
  });

  it("has no toggle on the member's own profile, only the count", async () => {
    renderApp(
      "/users/teller",
      { ...noStories, "UserService/GetProfile": () => ({ body: { profile: me, followers: 0 } }) },
      signedIn,
    );
    expect(await screen.findByText("0 followers")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Follow/ })).toBeNull();
  });
});

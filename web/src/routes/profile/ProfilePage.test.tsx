import { screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { connectError } from "@/lib/fakeApi.testutil";
import { renderApp } from "@/routes/read/renderApp.testutil";

afterEach(() => vi.unstubAllGlobals());

const profile = {
  userId: "u-1",
  username: "wren",
  bio: "I write about the corner bookshop.",
  avatarUrl: "/avatars/u-1.png",
  joinedAt: "2026-03-02T15:00:00Z",
};

describe("public profile page", () => {
  it("shows a signed-out reader the username, avatar, bio and join date, looked up by username", async () => {
    const { calls } = renderApp("/users/wren", {
      "UserService/GetProfile": () => ({ body: { profile, account: { email: "wren@example.com" } } }),
    });
    expect(await screen.findByRole("heading", { level: 1, name: "wren" })).toBeInTheDocument();
    expect(screen.getByText("I write about the corner bookshop.")).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "wren's avatar" })).toHaveAttribute("src", "/avatars/u-1.png");
    expect(screen.getByText(/Joined March 2026/)).toBeInTheDocument();
    expect(screen.queryByText(/wren@example\.com/)).toBeNull();
    expect(calls("UserService/GetProfile")).toEqual([{ username: "wren" }]);
  });

  it("says so when no user has the username", async () => {
    renderApp("/users/nobody", { "UserService/GetProfile": () => connectError("not_found") });
    expect(await screen.findByRole("heading", { level: 1, name: "User not found" })).toBeInTheDocument();
  });

  it("shows a deleted account without its details", async () => {
    renderApp("/users/gone", {
      "UserService/GetProfile": () => ({ body: { profile: { userId: "u-2", deleted: true } } }),
    });
    expect(await screen.findByText("This account was deleted.")).toBeInTheDocument();
  });
});

import { zodResolver } from "@hookform/resolvers/zod";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useForm } from "react-hook-form";
import { describe, expect, it, vi } from "vitest";
import { z } from "zod";

import { Button } from "@/components/ui/Button";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogTitle, DialogTrigger } from "@/components/ui/Dialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/DropdownMenu";
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/Form";
import { Input } from "@/components/ui/Input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/Select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/Tabs";
import { Textarea } from "@/components/ui/Textarea";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/Tooltip";

describe("Button", () => {
  it("is a real button that activates from the keyboard", async () => {
    const onClick = vi.fn();
    render(<Button onClick={onClick}>Publish</Button>);
    const button = screen.getByRole("button", { name: "Publish" });
    expect(button).toHaveAttribute("type", "button");
    button.focus();
    await userEvent.keyboard("{Enter}");
    await userEvent.keyboard(" ");
    expect(onClick).toHaveBeenCalledTimes(2);
  });

  it("renders its child element with asChild", () => {
    render(
      <Button asChild variant="secondary">
        <a href="/login">Sign in</a>
      </Button>,
    );
    const link = screen.getByRole("link", { name: "Sign in" });
    expect(link).toHaveAttribute("href", "/login");
    expect(link.className).toContain("border-control");
  });

  it("merges caller classes over variant classes", () => {
    render(<Button className="px-8">Go</Button>);
    const cls = screen.getByRole("button").className;
    expect(cls).toContain("px-8");
    expect(cls).not.toMatch(/\bpx-4\b/);
  });
});

describe("Input and Textarea", () => {
  it("forward refs and mark invalid state for assistive tech", () => {
    render(
      <>
        <Input aria-label="Email" aria-invalid />
        <Textarea aria-label="Part" />
      </>,
    );
    expect(screen.getByRole("textbox", { name: "Email" })).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByRole("textbox", { name: "Part" }).tagName).toBe("TEXTAREA");
  });
});

describe("Dialog", () => {
  it("opens from the keyboard, is labelled, and closes on Escape returning focus", async () => {
    const user = userEvent.setup();
    render(
      <Dialog>
        <DialogTrigger asChild>
          <Button>Report</Button>
        </DialogTrigger>
        <DialogContent>
          <DialogTitle>Report this perspective</DialogTitle>
          <DialogDescription>Tell the moderators what is wrong.</DialogDescription>
          <DialogClose asChild>
            <Button>Cancel</Button>
          </DialogClose>
        </DialogContent>
      </Dialog>,
    );
    const trigger = screen.getByRole("button", { name: "Report" });
    trigger.focus();
    await user.keyboard("{Enter}");
    const dialog = await screen.findByRole("dialog", { name: "Report this perspective" });
    expect(dialog).toHaveAccessibleDescription("Tell the moderators what is wrong.");
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(trigger).toHaveFocus();
  });
});

describe("Tabs", () => {
  it("moves between tabs with arrow keys and marks the selected tab", async () => {
    const user = userEvent.setup();
    render(
      <Tabs defaultValue="ranked">
        <TabsList aria-label="Epic">
          <TabsTrigger value="ranked">Ranked</TabsTrigger>
          <TabsTrigger value="newest">Newest</TabsTrigger>
          <TabsTrigger value="tales">Tales</TabsTrigger>
        </TabsList>
        <TabsContent value="ranked">ranked list</TabsContent>
        <TabsContent value="newest">newest list</TabsContent>
        <TabsContent value="tales">tales list</TabsContent>
      </Tabs>,
    );
    await user.tab();
    expect(screen.getByRole("tab", { name: "Ranked" })).toHaveFocus();
    await user.keyboard("{ArrowRight}");
    const newest = screen.getByRole("tab", { name: "Newest" });
    expect(newest).toHaveFocus();
    expect(newest).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tabpanel")).toHaveTextContent("newest list");
  });
});

describe("DropdownMenu", () => {
  it("opens from the keyboard and runs the chosen item", async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    render(
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button>Account</Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent>
          <DropdownMenuItem onSelect={onSelect}>Sign out</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>,
    );
    screen.getByRole("button", { name: "Account" }).focus();
    await user.keyboard("{Enter}");
    const item = await screen.findByRole("menuitem", { name: "Sign out" });
    await waitFor(() => expect(item).toHaveFocus());
    await user.keyboard("{Enter}");
    expect(onSelect).toHaveBeenCalledOnce();
  });
});

describe("Select", () => {
  it("exposes a labelled combobox showing the current value", () => {
    render(
      <Select defaultValue="drama">
        <SelectTrigger aria-label="Genre">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="drama">Drama</SelectItem>
          <SelectItem value="comedy">Comedy</SelectItem>
        </SelectContent>
      </Select>,
    );
    expect(screen.getByRole("combobox", { name: "Genre" })).toHaveTextContent("Drama");
  });
});

describe("Tooltip", () => {
  it("shows supplementary text when the trigger takes keyboard focus", async () => {
    const user = userEvent.setup();
    render(
      <TooltipProvider delayDuration={0}>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button>Canon</Button>
          </TooltipTrigger>
          <TooltipContent>The arc's main thread</TooltipContent>
        </Tooltip>
      </TooltipProvider>,
    );
    await user.tab();
    expect(await screen.findByRole("tooltip")).toHaveTextContent("The arc's main thread");
  });
});

const schema = z.object({ username: z.string().min(3, "Use at least 3 characters") });

function UsernameForm({ onValid }: { onValid: (v: z.infer<typeof schema>) => void }) {
  const form = useForm<z.infer<typeof schema>>({ resolver: zodResolver(schema), defaultValues: { username: "" } });
  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(onValid)}>
        <FormField
          control={form.control}
          name="username"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Username</FormLabel>
              <FormControl>
                <Input {...field} />
              </FormControl>
              <FormDescription>Letters, digits and underscores.</FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        <Button type="submit">Save</Button>
      </form>
    </Form>
  );
}

describe("Form", () => {
  it("renders the schema error inline and wires it to the field", async () => {
    const user = userEvent.setup();
    const onValid = vi.fn();
    render(<UsernameForm onValid={onValid} />);
    const input = screen.getByLabelText("Username");
    expect(input).toHaveAccessibleDescription("Letters, digits and underscores.");
    await user.type(input, "ab");
    await user.click(screen.getByRole("button", { name: "Save" }));
    const message = await screen.findByText("Use at least 3 characters");
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(input.getAttribute("aria-describedby")).toContain(message.id);
    expect(onValid).not.toHaveBeenCalled();
  });

  it("submits a valid value and clears the error", async () => {
    const user = userEvent.setup();
    const onValid = vi.fn();
    render(<UsernameForm onValid={onValid} />);
    await user.type(screen.getByLabelText("Username"), "teller_1");
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(onValid).toHaveBeenCalledWith({ username: "teller_1" }, expect.anything()));
    expect(screen.getByLabelText("Username")).toHaveAttribute("aria-invalid", "false");
  });
});

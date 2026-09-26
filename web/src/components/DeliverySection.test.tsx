// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import type { Delivery } from "../api/client";
import DeliverySection from "./DeliverySection";

const SHA_API_1 = "48a4afb6e1c2d9f07a3b5c8e4d2f1a0b9c8d7e6f";
const SHA_API_2 = "6bafa19c0ffee51d2a7b3e4f5a6b7c8d9e0f1a2b";
const SHA_TAP = "a198cc5d4e3f2a1b0c9d8e7f6a5b4c3d2e1f0a9b";

const full: Delivery = {
  branch: "acp-150-delivery-fields",
  worktree: "/Users/bilal/Projects/taskboard-worktrees/acp-150-delivery-fields",
  prUrl: "https://github.com/bilal-fazlani/taskboard/pull/42",
  landedCommits: [
    { sha: SHA_API_1, repo: "bilal-fazlani/taskboard" },
    { sha: SHA_TAP, repo: "bilal-fazlani/homebrew-tap" },
    { sha: SHA_API_2, repo: "bilal-fazlani/taskboard" },
  ],
};

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("DeliverySection", () => {
  it("shows every field, read only, with the footnote", () => {
    render(<DeliverySection delivery={full} />);
    const section = screen.getByTestId("delivery-section");
    expect(within(section).getByRole("heading", { name: "Delivery" })).toBeTruthy();
    expect(within(section).getByText("Branch")).toBeTruthy();
    expect(within(section).getByText("acp-150-delivery-fields")).toBeTruthy();

    const worktree = screen.getByTestId("delivery-worktree");
    expect(worktree.textContent).toContain(full.worktree);
    expect(worktree.getAttribute("title")).toBe(full.worktree);
    // The path is cut at its start, so its last folder stays in view.
    expect(worktree.className).toMatch(/\[direction:rtl\]/);
    expect(worktree.className).toMatch(/\btruncate\b/);

    const pr = within(section).getByRole("link", { name: "bilal-fazlani/taskboard#42" });
    expect(pr.getAttribute("href")).toBe(full.prUrl);
    expect(pr.getAttribute("target")).toBe("_blank");
    expect(pr.getAttribute("rel")).toBe("noreferrer");

    expect(within(section).getByText("Read only. Agents set these through MCP, the CLI or the API.")).toBeTruthy();
    // Nothing in it is editable.
    expect(within(section).queryAllByRole("textbox")).toHaveLength(0);
    expect(within(section).queryAllByRole("button").every((b) => /^Copy /.test(b.getAttribute("aria-label") ?? ""))).toBe(true);
  });

  it("groups several commits across two repos, in the order given, as short shas with the full one on hover", () => {
    render(<DeliverySection delivery={full} />);
    expect(screen.getByText(/3 in 2 repos/)).toBeTruthy();

    const groups = within(screen.getByRole("list", { name: "Landed commits" })).getAllByRole("list");
    expect(groups.map((g) => g.getAttribute("aria-label"))).toEqual([
      "Commits in bilal-fazlani/taskboard",
      "Commits in bilal-fazlani/homebrew-tap",
    ]);
    const shas = (i: number) => within(groups[i]).getAllByRole("listitem").map((li) => li.textContent);
    expect(shas(0)).toEqual(["48a4afb", "6bafa19"]);
    expect(shas(1)).toEqual(["a198cc5"]);
    expect(screen.getByText("6bafa19").getAttribute("title")).toBe(SHA_API_2);
  });

  it("copies the full sha, the branch and the worktree path", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    render(<DeliverySection delivery={full} />);

    fireEvent.click(screen.getByRole("button", { name: "Copy commit a198cc5" }));
    expect(writeText).toHaveBeenLastCalledWith(SHA_TAP);
    await screen.findByRole("button", { name: "Copied" });

    fireEvent.click(screen.getByRole("button", { name: "Copy branch" }));
    expect(writeText).toHaveBeenLastCalledWith(full.branch);
    fireEvent.click(screen.getByRole("button", { name: "Copy worktree path" }));
    expect(writeText).toHaveBeenLastCalledWith(full.worktree);
  });

  it("shows only the fields that are set", () => {
    render(<DeliverySection delivery={{ branch: "fix-login", landedCommits: [{ sha: "0123abc", repo: "acme/api" }] }} />);
    const section = screen.getByTestId("delivery-section");
    expect(within(section).getByText("fix-login")).toBeTruthy();
    expect(within(section).queryByText("Worktree")).toBeNull();
    expect(within(section).queryByText("PR")).toBeNull();
    expect(within(section).queryByRole("link")).toBeNull();
    expect(within(section).getByText(/1 in 1 repo$/)).toBeTruthy();
  });

  it("shows commits alone, and a url that is not a GitHub pull request as it is", () => {
    const { rerender } = render(<DeliverySection delivery={{ landedCommits: [{ sha: "0123abc", repo: "acme/api" }] }} />);
    expect(screen.queryByText("Branch")).toBeNull();
    expect(screen.getByText("0123abc")).toBeTruthy();

    rerender(<DeliverySection delivery={{ prUrl: "https://gitlab.example.com/acme/api/-/merge_requests/3" }} />);
    expect(screen.getByRole("link", { name: "https://gitlab.example.com/acme/api/-/merge_requests/3" })).toBeTruthy();
    expect(screen.queryByRole("list", { name: "Landed commits" })).toBeNull();
  });

  it("renders nothing at all when no field is set", () => {
    for (const delivery of [undefined, {}, { branch: "", landedCommits: [] }]) {
      const { container, unmount } = render(<DeliverySection delivery={delivery} />);
      expect(container.innerHTML).toBe("");
      unmount();
    }
  });
});

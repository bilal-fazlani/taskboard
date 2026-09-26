// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import type { BuildInfo } from "../api/client";

const mockApi = vi.hoisted(() => ({ version: { get: vi.fn() } }));
vi.mock("../api/client", () => ({ api: mockApi }));

import BuildVersion from "./BuildVersion";

const COMMIT = "a198cc54a09a68ffaa27682c3458b51768c3f2b4";

async function mount(answer: Promise<BuildInfo>) {
  mockApi.version.get.mockReturnValue(answer);
  const { container } = render(<BuildVersion />);
  await act(async () => {
    await answer.catch(() => {});
  });
  return container.firstElementChild as HTMLElement;
}

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

describe("BuildVersion", () => {
  it("shows a release build's version and short commit, with the full commit in the tooltip", async () => {
    const footer = await mount(Promise.resolve({ version: "v0.2.0", commit: COMMIT, dev: false }));

    expect(mockApi.version.get).toHaveBeenCalledTimes(1);
    expect(screen.getByText("v0.2.0")).toBeTruthy();
    expect(screen.getByText("a198cc5")).toBeTruthy();
    expect(footer.textContent).not.toContain("dev build");
    expect(footer.title).toBe(`Version v0.2.0\nCommit ${COMMIT}`);
  });

  it("says when it is a dev build", async () => {
    const footer = await mount(Promise.resolve({ version: "v0.1.0-197-ga198cc5-dirty", commit: COMMIT, dev: true }));

    expect(screen.getByText("v0.1.0-197-ga198cc5-dirty")).toBeTruthy();
    expect(screen.getByText("a198cc5 · dev build")).toBeTruthy();
    expect(footer.title).toBe(`Version v0.1.0-197-ga198cc5-dirty\nCommit ${COMMIT}\nDev build`);
  });

  it("stays empty when the server can't say", async () => {
    const footer = await mount(Promise.reject(new Error("API error 500")));

    expect(footer.textContent).toBe("");
  });
});

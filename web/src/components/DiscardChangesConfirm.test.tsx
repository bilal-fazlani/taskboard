// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import DiscardChangesConfirm from "./DiscardChangesConfirm";

afterEach(cleanup);

describe("DiscardChangesConfirm", () => {
  it("names what would be lost, starts on Keep editing, and Escape keeps editing", () => {
    const onCancel = vi.fn();
    const onDiscard = vi.fn();
    render(<DiscardChangesConfirm what="this document" onCancel={onCancel} onDiscard={onDiscard} />);
    const dialog = screen.getByRole("alertdialog", { name: "Discard your changes?" });
    expect(dialog.textContent).toContain("Your edits to this document have not been saved.");
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Keep editing" }));

    fireEvent.keyDown(document.body, { key: "Escape" });
    expect(onCancel).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(onDiscard).toHaveBeenCalledTimes(1);
  });
});

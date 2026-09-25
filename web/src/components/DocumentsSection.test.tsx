// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { DocumentMeta } from "../api/client";

const mockApi = vi.hoisted(() => ({
  documents: {
    update: vi.fn(),
    delete: vi.fn(),
    downloadUrl: (id: string) => `/api/documents/${id}/download`,
  },
}));
vi.mock("../api/client", () => ({ api: mockApi }));

import DocumentsSection from "./DocumentsSection";

const spec: DocumentMeta = {
  id: "d1", name: "Design spec", format: "markdown", size: 14 * 1024, revision: 1,
  createdAt: "2026-09-25T09:00:00Z", updatedAt: "2026-09-25T09:00:00Z",
};
const notes: DocumentMeta = { ...spec, id: "d2", name: "Notes", size: 10 };

function setup(documents: DocumentMeta[] | null = [spec, notes], extra: Partial<Parameters<typeof DocumentsSection>[0]> = {}) {
  const onOpen = vi.fn();
  const onChanged = vi.fn();
  const onDismissNotice = vi.fn();
  render(
    <DocumentsSection
      documents={documents}
      failed={false}
      notice={null}
      onDismissNotice={onDismissNotice}
      onOpen={onOpen}
      onChanged={onChanged}
      {...extra}
    />,
  );
  return { onOpen, onChanged, onDismissNotice };
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("DocumentsSection", () => {
  it("lists documents with their extension and size, and opens one", () => {
    const { onOpen } = setup();
    expect(screen.getAllByTestId("document-row")).toHaveLength(2);
    expect(screen.getByText("14 KB", { exact: false })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Design spec.md" }));
    expect(onOpen).toHaveBeenCalledWith(spec);
  });

  it("links each download to the file", () => {
    setup();
    const link = screen.getByRole("link", { name: "Download Design spec.md" });
    expect(link.getAttribute("href")).toBe("/api/documents/d1/download");
  });

  it("invites agents when there are none", () => {
    setup([]);
    expect(screen.getByText("No documents yet. Agents can attach them.")).toBeTruthy();
  });

  it("says when the documents could not be loaded", () => {
    setup(null, { failed: true });
    expect(screen.getByText("The documents could not be loaded.")).toBeTruthy();
  });

  it("shows a notice that can be dismissed", () => {
    const { onDismissNotice } = setup([spec], { notice: "Design spec.md was deleted." });
    expect(screen.getByRole("status").textContent).toContain("Design spec.md was deleted.");
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(onDismissNotice).toHaveBeenCalled();
  });

  it("checks a new name before asking the server", () => {
    setup();
    fireEvent.click(screen.getByRole("button", { name: "Rename Design spec.md" }));
    const input = screen.getByRole("textbox", { name: "Document name" });
    fireEvent.change(input, { target: { value: "plan.md" } });
    fireEvent.submit(input);
    expect(screen.getByRole("alert").textContent).toBe("Use letters, digits, spaces, _ and - only.");
    fireEvent.change(input, { target: { value: "notes" } });
    fireEvent.submit(input);
    expect(screen.getByRole("alert").textContent).toBe('This ticket already has a document called "Notes.md".');
    expect(mockApi.documents.update).not.toHaveBeenCalled();
  });

  it("renames through the API and reports the change", async () => {
    mockApi.documents.update.mockResolvedValue({ ...spec, name: "Plan", content: "" });
    const { onChanged } = setup();
    fireEvent.click(screen.getByRole("button", { name: "Rename Design spec.md" }));
    const input = screen.getByRole("textbox", { name: "Document name" });
    fireEvent.change(input, { target: { value: " Plan " } });
    fireEvent.submit(input);
    await waitFor(() => expect(onChanged).toHaveBeenCalled());
    expect(mockApi.documents.update).toHaveBeenCalledWith("d1", { name: "Plan" });
  });

  it("shows the server's reason when a rename is refused", async () => {
    mockApi.documents.update.mockRejectedValue(new Error('API error 400: {"error":"Enter a name"}'));
    setup();
    fireEvent.click(screen.getByRole("button", { name: "Rename Design spec.md" }));
    fireEvent.change(screen.getByRole("textbox", { name: "Document name" }), { target: { value: "Plan" } });
    fireEvent.submit(screen.getByRole("textbox", { name: "Document name" }));
    await waitFor(() => expect(screen.getByRole("alert").textContent).toBe("Enter a name"));
  });

  it("asks before deleting, and Escape cancels only the question", async () => {
    mockApi.documents.delete.mockResolvedValue(undefined);
    const { onChanged } = setup();
    fireEvent.click(screen.getByRole("button", { name: "Delete Design spec.md" }));
    const dialog = screen.getByRole("alertdialog");
    expect(dialog.textContent).toContain("Delete Design spec.md?");
    expect(dialog.textContent).toContain("This can't be undone.");

    fireEvent.keyDown(document.body, { key: "Escape" });
    expect(screen.queryByRole("alertdialog")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Delete Design spec.md" }));
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(onChanged).toHaveBeenCalled());
    expect(mockApi.documents.delete).toHaveBeenCalledWith("d1");
  });
});

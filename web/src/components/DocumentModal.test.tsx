// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { DocumentMeta } from "../api/client";

const mockApi = vi.hoisted(() => ({
  documents: {
    get: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
    downloadUrl: (id: string) => `/api/documents/${id}/download`,
  },
}));
vi.mock("../api/client", () => ({ api: mockApi }));

import DocumentModal from "./DocumentModal";

const spec: DocumentMeta = {
  id: "d1", name: "Design spec", format: "markdown", size: 20, revision: 1,
  createdAt: "2026-09-25T09:00:00Z", updatedAt: "2026-09-25T09:00:00Z",
};

function setup(doc = spec) {
  const props = { onClose: vi.fn(), onRenamed: vi.fn(), onDeleted: vi.fn() };
  const utils = render(<DocumentModal doc={doc} documents={[doc]} ownerLabel="ACP-84" {...props} />);
  return { ...props, ...utils };
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("DocumentModal", () => {
  it("renders the document's markdown under its name and ticket", async () => {
    mockApi.documents.get.mockResolvedValue({ ...spec, content: "# Storage\n\nOne owner." });
    setup();
    expect(screen.getByRole("dialog", { name: "Design spec.md" })).toBeTruthy();
    expect(screen.getByText("ACP-84")).toBeTruthy();
    await waitFor(() => expect(screen.getByRole("heading", { name: "Storage" })).toBeTruthy());
  });

  it("refetches when an agent saves a new revision", async () => {
    mockApi.documents.get.mockResolvedValueOnce({ ...spec, content: "old" });
    const { rerender, onClose, onRenamed, onDeleted } = setup();
    await waitFor(() => expect(screen.getByText("old")).toBeTruthy());
    mockApi.documents.get.mockResolvedValueOnce({ ...spec, revision: 2, content: "new" });
    const next = { ...spec, revision: 2 };
    rerender(<DocumentModal doc={next} documents={[next]} ownerLabel="ACP-84" onClose={onClose} onRenamed={onRenamed} onDeleted={onDeleted} />);
    await waitFor(() => expect(screen.getByText("new")).toBeTruthy());
    expect(mockApi.documents.get).toHaveBeenCalledTimes(2);
  });

  it("says when the content could not be loaded", async () => {
    mockApi.documents.get.mockRejectedValue(new Error("API error 500: boom"));
    setup();
    await waitFor(() => expect(screen.getByText("This document could not be loaded.")).toBeTruthy());
  });

  it("closes on Escape and on the close button", async () => {
    mockApi.documents.get.mockResolvedValue({ ...spec, content: "x" });
    const { onClose } = setup();
    fireEvent.keyDown(document.body, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: "Close document" }));
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it("lets Escape cancel the delete question without closing the document", async () => {
    mockApi.documents.get.mockResolvedValue({ ...spec, content: "x" });
    const { onClose } = setup();
    fireEvent.click(screen.getByRole("button", { name: "Delete Design spec.md" }));
    expect(screen.getByRole("alertdialog")).toBeTruthy();
    fireEvent.keyDown(document.body, { key: "Escape" });
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(onClose).not.toHaveBeenCalled();
  });

  it("renames from the header", async () => {
    mockApi.documents.get.mockResolvedValue({ ...spec, content: "x" });
    mockApi.documents.update.mockResolvedValue({ ...spec, name: "Plan", content: "x" });
    const { onRenamed } = setup();
    fireEvent.click(screen.getByRole("button", { name: "Rename Design spec.md" }));
    const input = screen.getByRole("textbox", { name: "Document name" });
    fireEvent.change(input, { target: { value: "Plan" } });
    fireEvent.submit(input);
    await waitFor(() => expect(onRenamed).toHaveBeenCalledWith(expect.objectContaining({ name: "Plan" })));
  });
});

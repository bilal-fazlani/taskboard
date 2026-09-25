// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { DocumentMeta } from "../api/client";

const mockApi = vi.hoisted(() => ({
  documents: {
    create: vi.fn(),
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
  const onCreated = vi.fn();
  render(
    <DocumentsSection
      documents={documents}
      failed={false}
      notice={null}
      onDismissNotice={onDismissNotice}
      onOpen={onOpen}
      onChanged={onChanged}
      owner={{ ticketId: "t1" }}
      onCreated={onCreated}
      {...extra}
    />,
  );
  return { onOpen, onChanged, onDismissNotice, onCreated };
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

  it("names an image with its extension and downloads it under that name", () => {
    const photo: DocumentMeta = { ...spec, id: "d3", name: "Holiday photo", format: "jpeg", size: 45 * 1024, width: 800, height: 1200 };
    const { onOpen } = setup([spec, photo]);
    fireEvent.click(screen.getByRole("button", { name: "Holiday photo.jpg" }));
    expect(onOpen).toHaveBeenCalledWith(photo);
    const link = screen.getByRole("link", { name: "Download Holiday photo.jpg" });
    expect(link.getAttribute("href")).toBe("/api/documents/d3/download");
    expect(link.getAttribute("download")).toBe("Holiday photo.jpg");
  });

  it("says when there are none", () => {
    setup([]);
    expect(screen.getByText("No documents yet.")).toBeTruthy();
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

  it("creates a new document by name and asks to open it for editing", async () => {
    const created = { ...spec, id: "d9", name: "Rollout plan", content: "" };
    mockApi.documents.create.mockResolvedValue(created);
    const { onCreated } = setup();
    fireEvent.click(screen.getByRole("button", { name: "New document" }));
    const dialog = screen.getByRole("dialog", { name: "New document" });
    expect(dialog.textContent).toContain(".md");
    const input = screen.getByRole("textbox", { name: "Name" });
    fireEvent.submit(input);
    expect(screen.getByRole("alert").textContent).toBe("Enter a name");
    fireEvent.change(input, { target: { value: "Design spec" } });
    fireEvent.submit(input);
    expect(screen.getByRole("alert").textContent).toBe('This ticket already has a document called "Design spec.md".');
    fireEvent.change(input, { target: { value: "plan.md" } });
    fireEvent.submit(input);
    expect(screen.getByRole("alert").textContent).toBe("Use letters, digits, spaces, _ and - only.");
    expect(mockApi.documents.create).not.toHaveBeenCalled();

    fireEvent.change(input, { target: { value: " Rollout plan " } });
    fireEvent.submit(input);
    await waitFor(() => expect(onCreated).toHaveBeenCalledWith(created, true));
    expect(mockApi.documents.create).toHaveBeenCalledWith({ ticketId: "t1", name: "Rollout plan", format: "markdown", content: "" });
    expect(screen.queryByRole("dialog", { name: "New document" })).toBeNull();
  });

  it("shows the server's reason when a new name was taken meanwhile", async () => {
    mockApi.documents.create.mockRejectedValue(
      new Error('API error 400: {"error":"This ticket already has a document called \\"Rollout plan.md\\"."}'),
    );
    const { onCreated } = setup();
    fireEvent.click(screen.getByRole("button", { name: "New document" }));
    const input = screen.getByRole("textbox", { name: "Name" });
    fireEvent.change(input, { target: { value: "Rollout plan" } });
    fireEvent.submit(input);
    await waitFor(() =>
      expect(screen.getByRole("alert").textContent).toBe('This ticket already has a document called "Rollout plan.md".'),
    );
    expect(onCreated).not.toHaveBeenCalled();
  });

  it("closes the new-document dialog on Cancel or Escape without creating anything", () => {
    setup();
    fireEvent.click(screen.getByRole("button", { name: "New document" }));
    fireEvent.keyDown(document.body, { key: "Escape" });
    expect(screen.queryByRole("dialog", { name: "New document" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "New document" }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("dialog", { name: "New document" })).toBeNull();
    expect(mockApi.documents.create).not.toHaveBeenCalled();
  });

  it("uploads a .md file under a name made from its filename", async () => {
    const created = { ...spec, id: "d9", name: "api-design_v1 2", content: "# x" };
    mockApi.documents.create.mockResolvedValue(created);
    const { onCreated } = setup();
    const file = new File(["# x"], "api-design_v1.2.md", { type: "text/markdown" });
    fireEvent.change(screen.getByLabelText("Upload a document"), { target: { files: [file] } });
    await waitFor(() => expect(onCreated).toHaveBeenCalledWith(created, false));
    expect(mockApi.documents.create).toHaveBeenCalledWith({ ticketId: "t1", name: "api-design_v1 2", format: "markdown", content: "# x" });
  });

  it("marks HTML documents with their own icon", () => {
    setup([spec, { ...notes, name: "Report", format: "html" }]);
    expect(screen.getByRole("button", { name: "Report.html" })).toBeTruthy();
    expect(screen.getAllByTestId("document-icon-html")).toHaveLength(1);
    expect(screen.getAllByTestId("document-icon-markdown")).toHaveLength(1);
    expect(screen.getByRole("link", { name: "Download Report.html" }).getAttribute("href")).toBe("/api/documents/d2/download");
  });

  it("accepts .md, .html and .htm uploads", () => {
    setup();
    expect(screen.getByLabelText("Upload a document").getAttribute("accept")).toBe(".md,.html,.htm");
  });

  it("uploads an .HTM file as an HTML document named from its filename", async () => {
    const created = { ...spec, id: "d9", name: "Load test", format: "html", content: "<h1>x</h1>" };
    mockApi.documents.create.mockResolvedValue(created);
    const { onCreated } = setup();
    const file = new File(["<h1>x</h1>"], "Load test.HTM", { type: "text/html" });
    fireEvent.change(screen.getByLabelText("Upload a document"), { target: { files: [file] } });
    await waitFor(() => expect(onCreated).toHaveBeenCalledWith(created, false));
    expect(mockApi.documents.create).toHaveBeenCalledWith({ ticketId: "t1", name: "Load test", format: "html", content: "<h1>x</h1>" });
  });

  it("refuses an upload of another type, or over 8 MB, without reading it or asking the server", async () => {
    setup();
    const txt = new File(["x"], "notes.txt");
    const readTxt = vi.spyOn(txt, "text");
    fireEvent.change(screen.getByLabelText("Upload a document"), { target: { files: [txt] } });
    expect(await screen.findByText("Only .md, .html and .htm files can be attached.")).toBeTruthy();

    const big = new File(["x"], "big.md");
    Object.defineProperty(big, "size", { value: 8 * 1024 * 1024 + 1 });
    const readBig = vi.spyOn(big, "text");
    fireEvent.change(screen.getByLabelText("Upload a document"), { target: { files: [big] } });
    expect(await screen.findByText("This document is 8.1 MB. The limit is 8 MB.")).toBeTruthy();
    expect(readTxt).not.toHaveBeenCalled();
    expect(readBig).not.toHaveBeenCalled();
    expect(mockApi.documents.create).not.toHaveBeenCalled();
  });

  it("shows the server's reason when an upload is refused", async () => {
    mockApi.documents.create.mockRejectedValue(new Error('API error 400: {"error":"This ticket already has a document called \\"Notes.md\\"."}'));
    setup();
    fireEvent.change(screen.getByLabelText("Upload a document"), { target: { files: [new File(["x"], "notes.md")] } });
    expect(await screen.findByText('This ticket already has a document called "Notes.md".')).toBeTruthy();
  });
});

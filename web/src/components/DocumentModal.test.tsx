// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, createEvent, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { DocumentMeta } from "../api/client";

const mockApi = vi.hoisted(() => ({
  documents: {
    create: vi.fn(),
    get: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
    createImage: vi.fn(),
    downloadUrl: (id: string) => `/api/documents/${id}/download`,
    rawUrl: (id: string, revision: number) => `/api/documents/${id}/raw?rev=${revision}`,
    imageUrl: (id: string, revision: number) => `/api/documents/${id}/image?rev=${revision}`,
  },
}));
vi.mock("../api/client", () => ({ api: mockApi }));

import DocumentModal from "./DocumentModal";

const spec: DocumentMeta = {
  id: "d1", name: "Design spec", format: "markdown", size: 20, revision: 1,
  createdAt: "2026-09-25T09:00:00Z", updatedAt: "2026-09-25T09:00:00Z",
};

function setup(doc = spec, extra: Partial<Parameters<typeof DocumentModal>[0]> = {}) {
  const props = {
    onClose: vi.fn(),
    onRenamed: vi.fn(),
    onDeleted: vi.fn(),
    onRecreated: vi.fn(),
    onDirtyChange: vi.fn(),
    onCloseCancelled: vi.fn(),
  };
  const utils = render(<DocumentModal doc={doc} documents={[doc]} owner={{ ticketId: "t1" }} ownerLabel="ACP-84" {...props} {...extra} />);
  const rerender = (next: Partial<Parameters<typeof DocumentModal>[0]>) =>
    utils.rerender(
      <DocumentModal doc={doc} documents={[doc]} owner={{ ticketId: "t1" }} ownerLabel="ACP-84" {...props} {...extra} {...next} />,
    );
  return { ...props, ...utils, rerender };
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
    const { rerender } = setup();
    await waitFor(() => expect(screen.getByText("old")).toBeTruthy());
    mockApi.documents.get.mockResolvedValueOnce({ ...spec, revision: 2, content: "new" });
    const next = { ...spec, revision: 2 };
    rerender({ doc: next, documents: [next] });
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

describe("images in a markdown document", () => {
  const shot: DocumentMeta = {
    id: "img1", name: "Login screen", format: "png", size: 10, revision: 4,
    createdAt: "2026-09-25T09:00:00Z", updatedAt: "2026-09-25T09:00:00Z",
  };
  const shotSrc = "/api/documents/img1/image?rev=4";
  const srcs = (testId: string) =>
    Array.from(screen.getByTestId(testId).querySelectorAll("img")).map((i) => i.getAttribute("src"));

  it("shows the owner's images by name, and marks others missing", async () => {
    mockApi.documents.get.mockResolvedValue({
      ...spec,
      content: "![a](Login screen.png) ![b](<Login screen.png>) ![c](LOGIN%20SCREEN.png) ![d](Other.png)",
    });
    setup(spec, { documents: [spec, shot], owner: { epicId: "e1" }, ownerNoun: "epic" });
    await waitFor(() => expect(srcs("document-content")).toEqual([shotSrc, shotSrc, shotSrc]));
    const missing = screen.getByTestId("missing-image");
    expect(missing.textContent).toBe("Missing image: Other.png");
    expect(missing.getAttribute("title")).toBe('This epic has no image called "Other.png".');
  });

  it("shows them in the edit Preview", async () => {
    mockApi.documents.get.mockResolvedValue({ ...spec, content: "x" });
    setup(spec, { documents: [spec, shot] });
    fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
    fireEvent.change(await screen.findByRole("textbox", { name: "Document content" }), {
      target: { value: "![shot](Login screen.png)" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    expect(srcs("document-preview")).toEqual([shotSrc]);
  });
});

describe("images pasted or dropped while writing", () => {
  const pasted: DocumentMeta = {
    id: "img9", name: "Pasted image", format: "png", size: 10, revision: 1, width: 4, height: 3,
    createdAt: "2026-09-25T09:00:00Z", updatedAt: "2026-09-25T09:00:00Z",
  };
  const png = (name: string) => new File(["x"], name, { type: "image/png" });
  const box = () => screen.getByRole("textbox", { name: "Document content" }) as HTMLTextAreaElement;
  const uploadLines = () => screen.queryAllByTestId("image-upload").map((e) => e.textContent);
  const srcs = (testId: string) =>
    Array.from(screen.getByTestId(testId).querySelectorAll("img")).map((i) => i.getAttribute("src"));

  it("adds a pasted image to the epic, refers to it at the cursor, and shows it in Preview", async () => {
    let finish!: (doc: DocumentMeta) => void;
    mockApi.documents.get.mockResolvedValue({ ...spec, content: "Intro" });
    mockApi.documents.createImage.mockReturnValue(new Promise((resolve) => (finish = resolve)));
    const onImageAdded = vi.fn();
    const { rerender } = setup(spec, { owner: { epicId: "e1" }, ownerNoun: "epic", onImageAdded });
    fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
    await screen.findByRole("textbox", { name: "Document content" });
    box().setSelectionRange(0, 0);
    fireEvent.paste(box(), { clipboardData: { types: ["Files"], files: [png("image.png")] } });
    expect(box().value).toBe("![](Pasted image.png)Intro");
    expect(screen.getByText("unsaved")).toBeTruthy();
    expect(uploadLines()).toEqual(["Uploading Pasted image.png… it appears in Documents when done"]);
    expect(mockApi.documents.createImage.mock.calls[0][0]).toEqual({ epicId: "e1" });
    expect((mockApi.documents.createImage.mock.calls[0][1] as File).name).toBe("Pasted image.png");

    await act(async () => finish(pasted));
    expect(onImageAdded).toHaveBeenCalledWith(pasted);
    expect(uploadLines()).toEqual([]);
    rerender({ documents: [spec, pasted], owner: { epicId: "e1" }, ownerNoun: "epic", onImageAdded });
    fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    expect(srcs("document-preview")).toEqual(["/api/documents/img9/image?rev=1"]);
  });

  it("names a second paste Pasted image 2, keeps dropped names, and rolls back a refused one", async () => {
    mockApi.documents.get.mockResolvedValue({ ...spec, content: "" });
    mockApi.documents.createImage
      .mockReturnValueOnce(new Promise(() => {}))
      .mockRejectedValueOnce(new Error('API error 400: {"error":"This isn\'t a PNG image: its content is GIF."}'));
    setup(spec, { documents: [spec, pasted] });
    fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
    await screen.findByRole("textbox", { name: "Document content" });
    fireEvent.paste(box(), { clipboardData: { types: ["Files"], files: [png("image.png")] } });
    expect(box().value).toBe("![](Pasted image 2.png)");
    fireEvent.drop(box(), { dataTransfer: { types: ["Files"], files: [png("flow chart.png")] } });
    await waitFor(() => expect(box().value).toBe("![](Pasted image 2.png)"));
    expect(screen.getByRole("alert").textContent).toContain(
      "flow chart.png wasn't uploaded: This isn't a PNG image: its content is GIF.",
    );
  });

  it("still shows the upload, and then why it failed, after Save ends editing, without changing the saved text", async () => {
    let refuse!: (err: Error) => void;
    mockApi.documents.get.mockResolvedValue({ ...spec, content: "Intro" });
    mockApi.documents.createImage.mockReturnValue(new Promise((_, reject) => (refuse = reject)));
    mockApi.documents.update.mockResolvedValue({ ...spec, revision: 2, content: "Intro![](Pasted image.png)" });
    setup();
    fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
    await screen.findByRole("textbox", { name: "Document content" });
    box().setSelectionRange(5, 5);
    fireEvent.paste(box(), { clipboardData: { types: ["Files"], files: [png("image.png")] } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(screen.queryByRole("textbox", { name: "Document content" })).toBeNull());
    expect(mockApi.documents.update).toHaveBeenCalledWith("d1", { content: "Intro![](Pasted image.png)", expectedRevision: 1 });
    expect(uploadLines()).toEqual(["Uploading Pasted image.png… it appears in Documents when done"]);

    await act(async () => refuse(new Error('API error 400: {"error":"This isn\'t a PNG image: its content is GIF."}')));
    expect(uploadLines()).toEqual([]);
    expect(screen.getByRole("alert").textContent).toContain(
      "Pasted image.png wasn't uploaded: This isn't a PNG image: its content is GIF. The saved text still refers to it.",
    );
    expect(mockApi.documents.update).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("document-content")).toBeTruthy();
  });

  it("leaves a text paste to the browser", async () => {
    mockApi.documents.get.mockResolvedValue({ ...spec, content: "x" });
    setup();
    fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
    await screen.findByRole("textbox", { name: "Document content" });
    const event = createEvent.paste(box(), { clipboardData: { types: ["text/plain"], files: [] } });
    fireEvent(box(), event);
    expect(event.defaultPrevented).toBe(false);
    expect(mockApi.documents.createImage).not.toHaveBeenCalled();
  });
});

describe("editing", () => {
  const load = (content: string, revision = 1) =>
    mockApi.documents.get.mockResolvedValue({ ...spec, revision, content });
  const conflict409 = (content: string, revision: number) =>
    new Error(`API error 409: ${JSON.stringify({ error: "changed", current: { ...spec, revision, content } })}`);
  const box = () => screen.getByRole("textbox", { name: "Document content" }) as HTMLTextAreaElement;

  async function startEditing() {
    fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
    return screen.findByRole("textbox", { name: "Document content" });
  }

  it("edits, previews and saves from the revision it started at", async () => {
    load("# Old");
    mockApi.documents.update.mockResolvedValue({ ...spec, revision: 2, content: "# New" });
    const { onDirtyChange } = setup();
    const textarea = await startEditing();
    expect(screen.queryByText("unsaved")).toBeNull();
    fireEvent.change(textarea, { target: { value: "# New" } });
    expect(screen.getByText("unsaved")).toBeTruthy();
    expect(onDirtyChange).toHaveBeenLastCalledWith(true);

    fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    expect(screen.getByRole("heading", { name: "New" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Write" }));
    expect(box().value).toBe("# New");

    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(screen.queryByRole("textbox", { name: "Document content" })).toBeNull());
    expect(mockApi.documents.update).toHaveBeenCalledWith("d1", { content: "# New", expectedRevision: 1 });
    expect(screen.getByRole("heading", { name: "New" })).toBeTruthy();
    expect(onDirtyChange).toHaveBeenLastCalledWith(false);
  });

  it("leaves edit mode at once on Cancel with nothing changed", async () => {
    load("old");
    setup();
    await startEditing();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(screen.queryByRole("textbox", { name: "Document content" })).toBeNull();
  });

  it("asks before Cancel, Escape or × throws away unsaved text", async () => {
    load("old");
    const { onClose } = setup();
    fireEvent.change(await startEditing(), { target: { value: "mine" } });

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.getByRole("alertdialog", { name: "Discard your changes?" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(box().value).toBe("mine");

    fireEvent.click(screen.getByRole("button", { name: "Close document" }));
    expect(screen.getByRole("alertdialog", { name: "Discard your changes?" })).toBeTruthy();
    // Escape closes only the question, the topmost layer.
    fireEvent.keyDown(document.body, { key: "Escape" });
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(onClose).not.toHaveBeenCalled();
    expect(box().value).toBe("mine");

    fireEvent.keyDown(document.body, { key: "Escape" });
    expect(screen.getByRole("alertdialog", { name: "Discard your changes?" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("discards to view mode from Cancel, keeping the modal open", async () => {
    load("old");
    const { onClose } = setup();
    fireEvent.change(await startEditing(), { target: { value: "mine" } });
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(screen.queryByRole("textbox", { name: "Document content" })).toBeNull();
    expect(screen.getByText("old")).toBeTruthy();
    expect(onClose).not.toHaveBeenCalled();
  });

  it("asks when Back dropped the document, and puts it back on Keep editing", async () => {
    load("old");
    const { rerender, onClose, onCloseCancelled } = setup();
    fireEvent.change(await startEditing(), { target: { value: "mine" } });
    rerender({ closeRequested: true });
    expect(screen.getByRole("alertdialog", { name: "Discard your changes?" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(onCloseCancelled).toHaveBeenCalledTimes(1);
    expect(onClose).not.toHaveBeenCalled();

    rerender({ closeRequested: true });
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("puts the document back in one click when Back comes while × is already asking", async () => {
    load("old");
    const { rerender, onClose, onCloseCancelled } = setup();
    fireEvent.change(await startEditing(), { target: { value: "mine" } });
    fireEvent.click(screen.getByRole("button", { name: "Close document" }));
    expect(screen.getByRole("alertdialog", { name: "Discard your changes?" })).toBeTruthy();
    rerender({ closeRequested: true });

    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(onCloseCancelled).toHaveBeenCalledTimes(1);
    rerender({ closeRequested: false });
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(box().value).toBe("mine");
    expect(onClose).not.toHaveBeenCalled();
  });

  it("shows the notice when someone else saves mid-edit, and can load theirs", async () => {
    load("old");
    const { rerender } = setup();
    fireEvent.change(await startEditing(), { target: { value: "mine" } });

    mockApi.documents.get.mockResolvedValue({ ...spec, revision: 2, content: "theirs" });
    const next = { ...spec, revision: 2 };
    rerender({ doc: next, documents: [next] });
    const notice = await screen.findByRole("status");
    expect(notice.textContent).toContain("This document changed while you were editing. Your text isn't saved yet.");
    expect(box().value).toBe("mine");

    fireEvent.click(screen.getByRole("button", { name: "Discard mine, load theirs" }));
    expect(await screen.findByText("theirs")).toBeTruthy();
    expect(screen.queryByRole("textbox", { name: "Document content" })).toBeNull();
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("shows the notice when an image rename elsewhere rewrote the document mid-edit", async () => {
    const shot: DocumentMeta = { ...spec, id: "i1", name: "Login screen", format: "png", width: 8, height: 8 };
    load("![a](<Login screen.png>)");
    const { rerender } = setup(spec, { documents: [spec, shot] });
    fireEvent.change(await startEditing(), { target: { value: "![a](<Login screen.png>) and mine" } });

    // The rename rewrote this document (a new revision) and renamed the image.
    mockApi.documents.get.mockResolvedValue({ ...spec, revision: 2, content: "![a](<Home page.png>)" });
    const next = { ...spec, revision: 2 };
    rerender({ doc: next, documents: [next, { ...shot, name: "Home page" }] });
    const notice = await screen.findByRole("status");
    expect(notice.textContent).toContain("This document changed while you were editing. Your text isn't saved yet.");
    expect(box().value).toBe("![a](<Login screen.png>) and mine");
    // The unsaved text still names the image by its old name.
    fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    expect(screen.getByTestId("missing-image").textContent).toBe("Missing image: Login screen.png");

    fireEvent.click(screen.getByRole("button", { name: "Discard mine, load theirs" }));
    await waitFor(() => expect(screen.getByRole("img", { name: "a" }).getAttribute("src")).toBe("/api/documents/i1/image?rev=1"));
    expect(screen.queryByTestId("missing-image")).toBeNull();
  });

  it("quietly takes a newer revision while there is nothing unsaved", async () => {
    load("old");
    const { rerender } = setup();
    await startEditing();
    mockApi.documents.get.mockResolvedValue({ ...spec, revision: 2, content: "theirs" });
    const next = { ...spec, revision: 2 };
    rerender({ doc: next, documents: [next] });
    await waitFor(() => expect(box().value).toBe("theirs"));
    expect(screen.queryByRole("status")).toBeNull();

    mockApi.documents.update.mockResolvedValue({ ...spec, revision: 3, content: "mine" });
    fireEvent.change(box(), { target: { value: "mine" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(mockApi.documents.update).toHaveBeenCalledWith("d1", { content: "mine", expectedRevision: 2 }));
  });

  it("keeps editing after the live notice and overwrites on save with the newer revision", async () => {
    load("old");
    const { rerender } = setup();
    fireEvent.change(await startEditing(), { target: { value: "mine" } });
    mockApi.documents.get.mockResolvedValue({ ...spec, revision: 2, content: "theirs" });
    const next = { ...spec, revision: 2 };
    rerender({ doc: next, documents: [next] });
    await screen.findByRole("status");

    fireEvent.click(screen.getByRole("button", { name: "Keep editing, overwrite on save" }));
    expect(screen.queryByRole("status")).toBeNull();
    expect(box().value).toBe("mine");
    expect(screen.getByText("unsaved")).toBeTruthy();

    mockApi.documents.update.mockResolvedValue({ ...spec, revision: 3, content: "mine" });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(mockApi.documents.update).toHaveBeenCalledWith("d1", { content: "mine", expectedRevision: 2 }));
  });

  it("shows the notice at Save when the change arrived unseen, and overwrites after 'Keep editing'", async () => {
    load("old");
    mockApi.documents.update
      .mockRejectedValueOnce(conflict409("theirs", 2))
      .mockResolvedValueOnce({ ...spec, revision: 3, content: "mine" });
    setup();
    fireEvent.change(await startEditing(), { target: { value: "mine" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect((await screen.findByRole("status")).textContent).toContain("This document changed while you were editing.");
    expect(box().value).toBe("mine");

    fireEvent.click(screen.getByRole("button", { name: "Keep editing, overwrite on save" }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(mockApi.documents.update).toHaveBeenLastCalledWith("d1", { content: "mine", expectedRevision: 2 }));
    await waitFor(() => expect(screen.queryByRole("textbox", { name: "Document content" })).toBeNull());
  });

  it("loads theirs from a conflict found at Save", async () => {
    load("old");
    mockApi.documents.update.mockRejectedValueOnce(conflict409("theirs", 2));
    setup();
    fireEvent.change(await startEditing(), { target: { value: "mine" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await screen.findByRole("status");
    fireEvent.click(screen.getByRole("button", { name: "Discard mine, load theirs" }));
    expect(await screen.findByText("theirs")).toBeTruthy();
  });

  it("offers to save a deleted document as a new one", async () => {
    load("old");
    const recreated = { ...spec, id: "d3", content: "mine" };
    mockApi.documents.create.mockResolvedValue(recreated);
    const { rerender, onRecreated } = setup();
    fireEvent.change(await startEditing(), { target: { value: "mine" } });
    rerender({ documents: [], deleted: true });
    expect((await screen.findByRole("status")).textContent).toContain(
      "This document was deleted while you were editing. Your text isn't saved yet.",
    );
    expect(screen.getByRole("button", { name: "Save" }).hasAttribute("disabled")).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Save as a new document" }));
    await waitFor(() => expect(onRecreated).toHaveBeenCalledWith(recreated));
    expect(mockApi.documents.create).toHaveBeenCalledWith({ ticketId: "t1", name: "Design spec", format: "markdown", content: "mine" });
  });

  it("closes a deleted document on Discard", async () => {
    load("old");
    const { rerender, onClose } = setup();
    fireEvent.change(await startEditing(), { target: { value: "mine" } });
    rerender({ documents: [], deleted: true });
    await screen.findByRole("status");
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("alertdialog")).toBeNull();
  });

  it("offers the same choices when a save finds the document gone", async () => {
    load("old");
    mockApi.documents.update.mockRejectedValueOnce(new Error('API error 404: {"error":"document not found"}'));
    mockApi.documents.create.mockRejectedValueOnce(
      new Error('API error 400: {"error":"This ticket already has a document called \\"Design spec.md\\"."}'),
    );
    setup();
    fireEvent.change(await startEditing(), { target: { value: "mine" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect((await screen.findByRole("status")).textContent).toContain("This document was deleted while you were editing.");
    fireEvent.click(screen.getByRole("button", { name: "Save as a new document" }));
    expect((await screen.findByRole("alert")).textContent).toBe('This ticket already has a document called "Design spec.md".');
    expect(box().value).toBe("mine");
  });

  it("opens straight into edit mode for a new document", async () => {
    mockApi.documents.get.mockResolvedValue({ ...spec, content: "" });
    setup(spec, { startEditing: true });
    expect(await screen.findByRole("textbox", { name: "Document content" })).toBeTruthy();
  });

  it("refuses to save more than 8 MB", async () => {
    load("old");
    setup();
    fireEvent.change(await startEditing(), { target: { value: "x".repeat(8 * 1024 * 1024 + 1) } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect((await screen.findByRole("alert")).textContent).toBe("This document is 8.1 MB. The limit is 8 MB.");
    expect(mockApi.documents.update).not.toHaveBeenCalled();
  });

  it("shows the server's reason when a save fails otherwise, keeping the text", async () => {
    load("old");
    mockApi.documents.update.mockRejectedValueOnce(new Error("API error 500: disk full"));
    setup();
    fireEvent.change(await startEditing(), { target: { value: "mine" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect((await screen.findByRole("alert")).textContent).toBe("disk full");
    expect(box().value).toBe("mine");
  });
});

describe("HTML documents", () => {
  const page: DocumentMeta = { ...spec, id: "h1", name: "Report", format: "html", revision: 3 };

  it("shows the page full size in a frame sandboxed to scripts only, without an Edit button", async () => {
    setup(page);
    const frame = screen.getByTitle("Report.html");
    expect(frame.tagName).toBe("IFRAME");
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts");
    for (const token of ["allow-same-origin", "allow-top-navigation", "allow-popups", "allow-forms", "allow-modals", "allow-downloads"]) {
      expect(frame.getAttribute("sandbox")).not.toContain(token);
    }
    expect(frame.getAttribute("src")).toBe("/api/documents/h1/raw?rev=3");
    expect(frame.getAttribute("referrerpolicy")).toBe("no-referrer");
    expect(frame.className).toContain("h-full");
    expect(frame.className).toContain("w-full");
    expect(screen.queryByRole("button", { name: "Edit" })).toBeNull();
    // The page is the server's to render: the modal never fetches it as JSON.
    await Promise.resolve();
    expect(mockApi.documents.get).not.toHaveBeenCalled();
  });

  it("keeps Download, Rename and Delete in the header", () => {
    setup(page);
    const download = screen.getByRole("link", { name: "Download Report.html" });
    expect(download.getAttribute("href")).toBe("/api/documents/h1/download");
    expect(download.getAttribute("download")).toBe("Report.html");
    expect(screen.getByRole("button", { name: "Rename Report.html" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Delete Report.html" })).toBeTruthy();
  });

  it("reloads the frame when an agent saves a new revision, in a new frame element", () => {
    const { rerender } = setup(page);
    const before = screen.getByTitle("Report.html");
    // The same revision keeps the frame.
    rerender({ doc: { ...page }, documents: [page] });
    expect(screen.getByTitle("Report.html")).toBe(before);

    const next = { ...page, revision: 4 };
    rerender({ doc: next, documents: [next] });
    const after = screen.getByTitle("Report.html");
    expect(after.getAttribute("src")).toBe("/api/documents/h1/raw?rev=4");
    // A new element, not a new src on the old one: changing a frame's src
    // adds a browser history entry, so × (a Back) would only go back inside
    // the frame and leave the modal open.
    expect(after).not.toBe(before);
    expect(before.isConnected).toBe(false);
  });

  it("puts the page in the Tab order, after the header, and wraps back from it", () => {
    setup(page);
    const dialog = screen.getByRole("dialog");
    const frame = screen.getByTitle("Report.html");
    // Shift+Tab from the dialog wraps to its last stop: the page.
    fireEvent.keyDown(dialog, { key: "Tab", shiftKey: true });
    expect(document.activeElement).toBe(frame);
    // Tab from Close is not wrapped: it goes on into the page.
    const close = screen.getByRole("button", { name: "Close document" });
    close.focus();
    expect(fireEvent.keyDown(close, { key: "Tab" })).toBe(true);
    expect(document.activeElement).toBe(close);
    // Tabbing out of the page's own controls lands after the frame and
    // wraps to the first control in the header.
    const wrap = document.querySelector<HTMLElement>("[data-focus-wrap]");
    expect(wrap).not.toBeNull();
    wrap!.focus();
    expect(document.activeElement).toBe(screen.getByRole("link", { name: "Download Report.html" }));
  });

  it("has no wrap stop for a markdown document", async () => {
    mockApi.documents.get.mockResolvedValue({ ...spec, content: "x" });
    setup();
    await waitFor(() => expect(mockApi.documents.get).toHaveBeenCalled());
    expect(document.querySelector("[data-focus-wrap]")).toBeNull();
  });

  it("never opens an HTML document in edit mode", () => {
    setup(page, { startEditing: true });
    expect(screen.getByTitle("Report.html").tagName).toBe("IFRAME");
    expect(screen.queryByRole("textbox", { name: "Document content" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });
});

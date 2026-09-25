// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { DocumentMeta } from "../api/client";

const mockApi = vi.hoisted(() => ({
  documents: {
    create: vi.fn(),
    createImage: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
    usage: vi.fn(),
    downloadUrl: (id: string) => `/api/documents/${id}/download`,
    thumbnailUrl: (id: string, revision: number) => `/api/documents/${id}/thumbnail?rev=${revision}`,
  },
}));
vi.mock("../api/client", () => ({ api: mockApi }));

import DocumentsSection from "./DocumentsSection";
import { USAGE_TIMEOUT_MS } from "./DeleteDocumentConfirm";

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
    // Only an image is looked for in the owner's text.
    expect(mockApi.documents.usage).not.toHaveBeenCalled();
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

  it("accepts .md, .html and .htm uploads, and PNG, JPEG, GIF and WebP images", () => {
    setup();
    expect(screen.getByLabelText("Upload a document").getAttribute("accept")).toBe(".md,.html,.htm,.png,.jpg,.jpeg,.gif,.webp");
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
    expect(await screen.findByText("Only .md, .html, .htm, .png, .jpg, .jpeg, .gif and .webp files can be attached.")).toBeTruthy();

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
  describe("images", () => {
    const shot: DocumentMeta = {
      ...spec, id: "d5", name: "Login screen", format: "png", size: 240 * 1024, revision: 3, width: 1280, height: 800,
    };

    it("shows an image row with its thumbnail and its size in pixels, and opens it", () => {
      const { onOpen } = setup([spec, shot]);
      const [textRow, imageRow] = screen.getAllByTestId("document-row");
      const thumb = within(imageRow).getByTestId("document-thumbnail");
      expect(thumb.getAttribute("src")).toBe("/api/documents/d5/thumbnail?rev=3");
      expect(thumb.getAttribute("alt")).toBe("");
      expect(within(imageRow).queryByTestId("document-icon-markdown")).toBeNull();
      expect(within(imageRow).getByText("240 KB · 1280×800")).toBeTruthy();
      // A text row keeps its icon and when it changed.
      expect(within(textRow).queryByTestId("document-thumbnail")).toBeNull();
      expect(within(textRow).getByTestId("document-icon-markdown")).toBeTruthy();
      fireEvent.click(screen.getByRole("button", { name: "Login screen.png" }));
      expect(onOpen).toHaveBeenCalledWith(shot);
    });

    it("downloads, renames and deletes an image as any other document", async () => {
      mockApi.documents.update.mockResolvedValue({ ...shot, name: "Sign in" });
      mockApi.documents.delete.mockResolvedValue(undefined);
      const { onChanged } = setup([shot]);
      expect(screen.getByRole("link", { name: "Download Login screen.png" }).getAttribute("download")).toBe("Login screen.png");

      fireEvent.click(screen.getByRole("button", { name: "Rename Login screen.png" }));
      expect(screen.getByText(".png")).toBeTruthy();
      const input = screen.getByRole("textbox", { name: "Document name" });
      fireEvent.change(input, { target: { value: "Sign in" } });
      fireEvent.submit(input);
      await waitFor(() => expect(mockApi.documents.update).toHaveBeenCalledWith("d5", { name: "Sign in" }));
      await waitFor(() => expect(onChanged).toHaveBeenCalledTimes(1));

      mockApi.documents.usage.mockResolvedValue({ places: [] });
      fireEvent.click(screen.getByRole("button", { name: "Delete Login screen.png" }));
      const del = within(screen.getByRole("alertdialog")).getByRole("button", { name: "Delete" }) as HTMLButtonElement;
      await waitFor(() => expect(del.disabled).toBe(false));
      fireEvent.click(del);
      await waitFor(() => expect(mockApi.documents.delete).toHaveBeenCalledWith("d5"));
      await waitFor(() => expect(onChanged).toHaveBeenCalledTimes(2));
    });

    it("warns, before deleting an image, where its owner's text uses it", async () => {
      let answer: (value: unknown) => void = () => {};
      mockApi.documents.usage.mockReturnValue(new Promise((resolve) => (answer = resolve)));
      mockApi.documents.delete.mockResolvedValue(undefined);
      const { onChanged } = setup([spec, shot]);
      fireEvent.click(screen.getByRole("button", { name: "Delete Login screen.png" }));
      const dialog = screen.getByRole("alertdialog");
      const del = within(dialog).getByRole("button", { name: "Delete" }) as HTMLButtonElement;
      // Delete waits for the answer.
      expect(del.disabled).toBe(true);
      await waitFor(() => expect(mockApi.documents.usage).toHaveBeenCalledWith("d5"));
      expect(del.disabled).toBe(true);
      answer({ places: [{ kind: "description" }, { kind: "document", documentId: "d9", name: "Plan.md" }] });
      await waitFor(() => expect(del.disabled).toBe(false));
      expect(within(dialog).getByRole("heading").textContent).toBe("Delete Login screen.png?");
      const desc = document.getElementById(dialog.getAttribute("aria-describedby") ?? "");
      expect(desc?.textContent).toBe(
        "It's used in 2 places: the description and Plan.md. They'll show a missing image. This can't be undone.",
      );
      fireEvent.click(del);
      await waitFor(() => expect(mockApi.documents.delete).toHaveBeenCalledWith("d5"));
      await waitFor(() => expect(onChanged).toHaveBeenCalledTimes(1));
    });

    it("stops waiting for where an image is used after a while", async () => {
      vi.useFakeTimers();
      try {
        mockApi.documents.usage.mockReturnValue(new Promise(() => {}));
        setup([shot]);
        fireEvent.click(screen.getByRole("button", { name: "Delete Login screen.png" }));
        const del = within(screen.getByRole("alertdialog")).getByRole("button", { name: "Delete" }) as HTMLButtonElement;
        await act(async () => vi.advanceTimersByTime(USAGE_TIMEOUT_MS - 1));
        expect(del.disabled).toBe(true);
        await act(async () => vi.advanceTimersByTime(1));
        expect(del.disabled).toBe(false);
        expect(screen.getByRole("alertdialog").textContent).toContain("This can't be undone.");
      } finally {
        vi.useRealTimers();
      }
    });

    it("still deletes an image when where it's used can't be told", async () => {
      mockApi.documents.usage.mockRejectedValue(new Error("offline"));
      mockApi.documents.delete.mockResolvedValue(undefined);
      setup([shot]);
      fireEvent.click(screen.getByRole("button", { name: "Delete Login screen.png" }));
      const dialog = screen.getByRole("alertdialog");
      const del = within(dialog).getByRole("button", { name: "Delete" }) as HTMLButtonElement;
      await waitFor(() => expect(del.disabled).toBe(false));
      expect(dialog.textContent).toContain("This can't be undone.");
      expect(within(dialog).queryByTestId("image-usage")).toBeNull();
      fireEvent.click(del);
      await waitFor(() => expect(mockApi.documents.delete).toHaveBeenCalledWith("d5"));
    });

    it.each([
      ["Login screen.png", "image/png"],
      ["Holiday photo.JPG", "image/jpeg"],
      ["Holiday photo.jpeg", "image/jpeg"],
      ["Spinner.gif", "image/gif"],
      ["Mock.webp", "image/webp"],
    ])("uploads %s as an image file, without reading it here", async (filename, type) => {
      const created = { ...shot, id: "d9" };
      mockApi.documents.createImage.mockResolvedValue(created);
      const { onCreated } = setup();
      const file = new File([new Uint8Array([1, 2, 3])], filename, { type });
      const read = vi.spyOn(file, "text");
      fireEvent.change(screen.getByLabelText("Upload a document"), { target: { files: [file] } });
      await waitFor(() => expect(onCreated).toHaveBeenCalledWith(created, false));
      expect(mockApi.documents.createImage).toHaveBeenCalledWith({ ticketId: "t1" }, file);
      expect(mockApi.documents.create).not.toHaveBeenCalled();
      expect(read).not.toHaveBeenCalled();
    });

    it("uploads an image to an epic", async () => {
      mockApi.documents.createImage.mockResolvedValue({ ...shot, id: "d9", ticketId: undefined, epicId: "e1" });
      const { onCreated } = setup([], { owner: { epicId: "e1" }, ownerNoun: "epic" });
      const file = new File(["x"], "Plan.png", { type: "image/png" });
      fireEvent.change(screen.getByLabelText("Upload a document"), { target: { files: [file] } });
      await waitFor(() => expect(onCreated).toHaveBeenCalled());
      expect(mockApi.documents.createImage).toHaveBeenCalledWith({ epicId: "e1" }, file);
    });

    it("refuses an SVG, and an image over 8 MB, before reading it or asking the server", async () => {
      setup();
      const svg = new File(["<svg/>"], "logo.svg", { type: "image/svg+xml" });
      fireEvent.change(screen.getByLabelText("Upload a document"), { target: { files: [svg] } });
      expect(await screen.findByText("SVG images can't be attached. Use PNG, JPEG, GIF or WebP.")).toBeTruthy();

      const big = new File(["x"], "big.png", { type: "image/png" });
      Object.defineProperty(big, "size", { value: 9 * 1024 * 1024 });
      fireEvent.change(screen.getByLabelText("Upload a document"), { target: { files: [big] } });
      expect(await screen.findByText("This image is 9.0 MB. The limit is 8 MB.")).toBeTruthy();
      expect(mockApi.documents.createImage).not.toHaveBeenCalled();
    });

    it("shows the server's reason when an image is refused", async () => {
      mockApi.documents.createImage.mockRejectedValue(new Error('API error 400: {"error":"This file isn\'t a PNG image."}'));
      setup();
      fireEvent.change(screen.getByLabelText("Upload a document"), { target: { files: [new File(["x"], "fake.png")] } });
      expect(await screen.findByText("This file isn't a PNG image.")).toBeTruthy();
    });
  });
});

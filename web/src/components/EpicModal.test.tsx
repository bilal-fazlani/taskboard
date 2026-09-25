// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { BrowserRouter } from "react-router-dom";
import type { DocumentMeta, Epic } from "../api/client";

const mockApi = vi.hoisted(() => ({
  epics: { update: vi.fn() },
  documents: {
    list: vi.fn(),
    get: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
    create: vi.fn(),
    downloadUrl: (id: string) => `/api/documents/${id}/download`,
    rawUrl: (id: string, rev: number) => `/api/documents/${id}/raw?rev=${rev}`,
    imageUrl: (id: string, rev: number) => `/api/documents/${id}/image?rev=${rev}`,
    thumbnailUrl: (id: string, rev: number) => `/api/documents/${id}/thumbnail?rev=${rev}`,
  },
}));
vi.mock("../api/client", () => ({ api: mockApi }));
vi.mock("../hooks/useLiveRefresh", () => ({ useLiveRefresh: () => {} }));

import EpicModal from "./EpicModal";

const launch = {
  id: "e1", projectId: "p1", name: "Launch", description: "Go live", createdAt: "", updatedAt: "",
  counts: {}, total: 0, complete: false, lastActivityAt: null, documentCount: 1,
} as Epic;
const plan: DocumentMeta = {
  id: "d1", epicId: "e1", name: "Rollout plan", format: "markdown", size: 6, revision: 1, createdAt: "", updatedAt: "",
};
const page: DocumentMeta = { ...plan, id: "d2", name: "Load test", format: "html" };

async function settle() {
  for (let i = 0; i < 10; i++) await act(async () => new Promise((resolve) => setTimeout(resolve, 5)));
}

beforeEach(() => window.history.replaceState(null, "", "/epics?project=ACP&epic=Launch"));
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  window.history.replaceState(null, "", "/");
});

function setup(extra: Partial<Parameters<typeof EpicModal>[0]> = {}) {
  const props = { onClose: vi.fn(), onSaved: vi.fn(), onDirtyChange: vi.fn(), onCloseCancelled: vi.fn() };
  const ui = (more: Partial<Parameters<typeof EpicModal>[0]> = {}) => (
    <BrowserRouter>
      <EpicModal epic={launch} epics={[launch]} projectPrefix="ACP" {...props} {...extra} {...more} />
    </BrowserRouter>
  );
  const utils = render(ui());
  return { ...props, rerender: (more: Partial<Parameters<typeof EpicModal>[0]>) => utils.rerender(ui(more)) };
}
const modal = () => screen.getByRole("dialog", { name: "Edit epic" });

describe("EpicModal", () => {
  it("edits the name and description and lists the epic's documents", async () => {
    mockApi.documents.list.mockResolvedValue([plan]);
    mockApi.epics.update.mockResolvedValue({ ...launch, name: "Go live" });
    const { onSaved } = setup();
    expect(modal()).toBeTruthy();
    expect(document.activeElement).toBe(within(modal()).getByLabelText("Name"));
    expect(await screen.findByRole("button", { name: "Rollout plan.md" })).toBeTruthy();
    expect(mockApi.documents.list).toHaveBeenCalledWith({ epicId: "e1" });
    expect((within(modal()).getByLabelText(/Description/) as HTMLInputElement).value).toBe("Go live");

    fireEvent.change(within(modal()).getByLabelText("Name"), { target: { value: "Go live" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(onSaved).toHaveBeenCalledWith(expect.objectContaining({ name: "Go live" })));
    expect(mockApi.epics.update).toHaveBeenCalledWith("e1", { name: "Go live", description: "Go live" });
  });

  it("closes on Escape, on × and on Cancel", async () => {
    mockApi.documents.list.mockResolvedValue([]);
    const { onClose } = setup();
    await screen.findByText("No documents yet.");
    fireEvent.keyDown(document.body, { key: "Escape" });
    fireEvent.click(within(modal()).getByRole("button", { name: "Close" }));
    fireEvent.click(within(modal()).getByRole("button", { name: "Cancel" }));
    expect(onClose).toHaveBeenCalledTimes(3);
  });

  it("opens an epic document over the modal, and Escape closes only the document", async () => {
    mockApi.documents.list.mockResolvedValue([plan]);
    mockApi.documents.get.mockResolvedValue({ ...plan, content: "# Steps" });
    const { onClose } = setup();
    fireEvent.click(await screen.findByRole("button", { name: "Rollout plan.md" }));
    expect(await screen.findByRole("heading", { name: "Steps" })).toBeTruthy();
    expect(new URLSearchParams(window.location.search).get("doc")).toBe("Rollout plan.md");
    expect(screen.getByText("Launch", { selector: "span" })).toBeTruthy();
    expect(modal().hasAttribute("inert")).toBe(true);

    fireEvent.keyDown(document.body, { key: "Escape" });
    await settle();
    expect(screen.queryByRole("heading", { name: "Steps" })).toBeNull();
    expect(onClose).not.toHaveBeenCalled();
    expect(new URLSearchParams(window.location.search).has("doc")).toBe(false);
  });

  it("names the epic in document messages", async () => {
    mockApi.documents.list.mockResolvedValue([plan]);
    setup();
    fireEvent.click(await screen.findByRole("button", { name: "New document" }));
    const dialog = screen.getByRole("dialog", { name: "New document" });
    fireEvent.change(within(dialog).getByRole("textbox", { name: "Name" }), { target: { value: "rollout PLAN" } });
    fireEvent.submit(within(dialog).getByRole("textbox", { name: "Name" }));
    expect(within(dialog).getByRole("alert").textContent).toBe('This epic already has a document called "Rollout plan.md".');
    // Escape closes the New document dialog only.
    fireEvent.keyDown(document.body, { key: "Escape" });
    expect(screen.queryByRole("dialog", { name: "New document" })).toBeNull();
    expect(modal()).toBeTruthy();
  });

  it("creates a document on the epic with New, and opens it for editing", async () => {
    const created = { ...plan, id: "d9", name: "Notes", size: 0 };
    mockApi.documents.list.mockResolvedValueOnce([plan]).mockResolvedValue([plan, created]);
    mockApi.documents.create.mockResolvedValue({ ...created, content: "" });
    mockApi.documents.get.mockResolvedValue({ ...created, content: "" });
    setup();
    fireEvent.click(await screen.findByRole("button", { name: "New document" }));
    const dialog = screen.getByRole("dialog", { name: "New document" });
    fireEvent.change(within(dialog).getByRole("textbox", { name: "Name" }), { target: { value: "Notes" } });
    fireEvent.submit(within(dialog).getByRole("textbox", { name: "Name" }));
    await waitFor(() =>
      expect(mockApi.documents.create).toHaveBeenCalledWith({ epicId: "e1", name: "Notes", format: "markdown", content: "" }),
    );
    expect(await screen.findByRole("textbox", { name: "Document content" })).toBeTruthy();
    expect(new URLSearchParams(window.location.search).get("doc")).toBe("Notes.md");
  });

  it("uploads an HTML file to the epic and shows it in a frame sandboxed to scripts only", async () => {
    mockApi.documents.list.mockResolvedValueOnce([]).mockResolvedValue([page]);
    mockApi.documents.create.mockResolvedValue({ ...page, content: "<h1>x</h1>" });
    setup();
    await screen.findByText("No documents yet.");
    const file = new File(["<h1>x</h1>"], "Load test.html", { type: "text/html" });
    fireEvent.change(screen.getByLabelText("Upload a document"), { target: { files: [file] } });
    await waitFor(() =>
      expect(mockApi.documents.create).toHaveBeenCalledWith({ epicId: "e1", name: "Load test", format: "html", content: "<h1>x</h1>" }),
    );
    fireEvent.click(await screen.findByRole("button", { name: "Load test.html" }));
    const frame = await screen.findByTitle("Load test.html");
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts");
    expect(frame.getAttribute("src")).toBe("/api/documents/d2/raw?rev=1");
  });

  it("counts a document's unsaved text as its own, and asks when Back drops the epic", async () => {
    mockApi.documents.list.mockResolvedValue([plan]);
    mockApi.documents.get.mockResolvedValue({ ...plan, content: "# Steps" });
    const { onDirtyChange, onCloseCancelled, rerender } = setup();
    fireEvent.click(await screen.findByRole("button", { name: "Rollout plan.md" }));
    fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
    fireEvent.change(screen.getByRole("textbox", { name: "Document content" }), { target: { value: "# Mine" } });
    await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(true));

    rerender({ closeRequested: true });
    const ask = await screen.findByRole("alertdialog", { name: "Discard your changes?" });
    fireEvent.click(within(ask).getByRole("button", { name: "Keep editing" }));
    expect(onCloseCancelled).toHaveBeenCalledTimes(1);
    expect((screen.getByRole("textbox", { name: "Document content" }) as HTMLTextAreaElement).value).toBe("# Mine");
  });

  it("shows the epic's images with thumbnails, steps through them in place, and × closes in one press", async () => {
    const shot: DocumentMeta = { ...plan, id: "i1", name: "Shot", format: "png", size: 2048, width: 320, height: 200 };
    const photo: DocumentMeta = { ...plan, id: "i2", name: "Photo", format: "webp", size: 4096, width: 100, height: 50 };
    mockApi.documents.list.mockResolvedValue([plan, shot, photo]);
    const { onClose } = setup();
    fireEvent.click(await screen.findByRole("button", { name: "Shot.png" }));
    expect(screen.getAllByTestId("document-thumbnail").map((t) => t.getAttribute("src"))).toEqual([
      "/api/documents/i1/thumbnail?rev=1",
      "/api/documents/i2/thumbnail?rev=1",
    ]);
    expect(screen.getByText("2 KB · 320×200")).toBeTruthy();
    const viewer = await screen.findByRole("dialog", { name: "Shot.png" });
    expect(within(viewer).getByTestId("image-facts").textContent).toBe("1 of 2 images · 320×200");
    const length = window.history.length;

    fireEvent.keyDown(document.body, { key: "ArrowRight" });
    expect(await screen.findByRole("dialog", { name: "Photo.webp" })).toBe(viewer);
    expect(new URLSearchParams(window.location.search).get("doc")).toBe("Photo.webp");
    expect(window.history.length).toBe(length);

    fireEvent.click(within(viewer).getByRole("button", { name: "Close document" }));
    await settle();
    expect(screen.queryByRole("dialog", { name: "Photo.webp" })).toBeNull();
    expect(new URLSearchParams(window.location.search).get("epic")).toBe("Launch");
    expect(new URLSearchParams(window.location.search).has("doc")).toBe(false);
    expect(onClose).not.toHaveBeenCalled();
  });
});


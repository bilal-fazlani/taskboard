// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { DocumentMeta } from "../api/client";

const mockApi = vi.hoisted(() => ({
  documents: {
    create: vi.fn(),
    get: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
    downloadUrl: (id: string) => `/api/documents/${id}/download`,
    rawUrl: (id: string, revision: number) => `/api/documents/${id}/raw?rev=${revision}`,
    imageUrl: (id: string, revision: number) => `/api/documents/${id}/image?rev=${revision}`,
  },
}));
vi.mock("../api/client", () => ({ api: mockApi }));

import DocumentModal from "./DocumentModal";

const base = { size: 240 * 1024, revision: 1, createdAt: "2026-09-25T09:00:00Z", updatedAt: "2026-09-25T09:00:00Z" };
const spec: DocumentMeta = { ...base, id: "d1", name: "Design spec", format: "markdown" };
const login: DocumentMeta = { ...base, id: "i1", name: "Login screen", format: "png", width: 1280, height: 800 };
const photo: DocumentMeta = { ...base, id: "i2", name: "Holiday photo", format: "jpeg", width: 4000, height: 3000 };
const spinner: DocumentMeta = { ...base, id: "i3", name: "Spinner", format: "gif", width: 64, height: 64 };
const all = [login, spec, photo, spinner];

type Props = Parameters<typeof DocumentModal>[0];

function setup(doc: DocumentMeta = photo, extra: Partial<Props> = {}) {
  const props = {
    onClose: vi.fn(),
    onRenamed: vi.fn(),
    onDeleted: vi.fn(),
    onRecreated: vi.fn(),
    onDirtyChange: vi.fn(),
    onCloseCancelled: vi.fn(),
    onStep: vi.fn(),
  };
  const view = (next: Partial<Props>) => (
    <DocumentModal doc={doc} documents={all} owner={{ ticketId: "t1" }} ownerLabel="ACP-84" {...props} {...extra} {...next} />
  );
  const utils = render(view({}));
  return { ...props, ...utils, rerender: (next: Partial<Props>) => utils.rerender(view(next)) };
}

const picture = () => screen.getByRole("img", { name: /\.(png|jpg|gif|webp)$/ }) as HTMLImageElement;

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("image viewer", () => {
  it("shows the image fitted to the window, under its name, where and how large it is", () => {
    setup();
    const dialog = screen.getByRole("dialog", { name: "Holiday photo.jpg" });
    expect(within(dialog).getByText("ACP-84")).toBeTruthy();
    expect(picture().getAttribute("src")).toBe("/api/documents/i2/image?rev=1");
    expect(picture().getAttribute("alt")).toBe("Holiday photo.jpg");
    expect(picture().dataset.zoom).toBe("fit");
    expect(picture().className).toMatch(/\bmax-w-full\b/);
    expect(picture().className).toMatch(/\bmax-h-full\b/);
    // The second of three images: the markdown document is not counted.
    expect(screen.getByTestId("image-facts").textContent).toBe("2 of 3 images · 4000×3000");
    expect(screen.getByRole("button", { name: "Fit" }).getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByRole("button", { name: "100%" }).getAttribute("aria-pressed")).toBe("false");
    // Nothing is fetched as text, and nothing is framed.
    expect(mockApi.documents.get).not.toHaveBeenCalled();
    expect(dialog.querySelector("iframe")).toBeNull();
  });

  it("offers Download, Rename, Delete and Close, and no Edit", () => {
    setup();
    const link = screen.getByRole("link", { name: "Download Holiday photo.jpg" });
    expect(link.getAttribute("href")).toBe("/api/documents/i2/download");
    expect(link.getAttribute("download")).toBe("Holiday photo.jpg");
    expect(screen.getByRole("button", { name: "Rename Holiday photo.jpg" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Delete Holiday photo.jpg" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Close document" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Edit" })).toBeNull();
  });

  it("shows actual pixels at 100%, scrolling within the window, and fits again", () => {
    setup();
    fireEvent.click(screen.getByRole("button", { name: "100%" }));
    expect(screen.getByRole("button", { name: "100%" }).getAttribute("aria-pressed")).toBe("true");
    expect(picture().dataset.zoom).toBe("actual");
    expect(picture().className).toMatch(/\bmax-w-none\b/);
    expect(picture().className).toMatch(/\bmax-h-none\b/);
    const scroller = screen.getByTestId("image-scroll");
    expect(scroller.className).toMatch(/\boverflow-auto\b/);
    expect(scroller.contains(picture())).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "Fit" }));
    expect(picture().dataset.zoom).toBe("fit");
    expect(screen.queryByTestId("image-scroll")).toBeNull();
  });

  it("says one of one image", () => {
    render(
      <DocumentModal doc={login} documents={[spec, login]} owner={{ epicId: "e1" }} ownerLabel="Images" ownerNoun="epic"
        onClose={vi.fn()} onRenamed={vi.fn()} onDeleted={vi.fn()} onRecreated={vi.fn()} />,
    );
    expect(screen.getByTestId("image-facts").textContent).toBe("1 of 1 image · 1280×800");
    expect(screen.getByText("Images")).toBeTruthy();
  });

  it("says when the image could not be loaded", () => {
    setup();
    fireEvent.error(picture());
    expect(screen.getByText("This image could not be loaded.")).toBeTruthy();
  });

  it("renames from the header", async () => {
    mockApi.documents.update.mockResolvedValue({ ...photo, name: "Beach" });
    const { onRenamed } = setup();
    fireEvent.click(screen.getByRole("button", { name: "Rename Holiday photo.jpg" }));
    const input = screen.getByRole("textbox", { name: "Document name" });
    expect(screen.getByText(".jpg")).toBeTruthy();
    fireEvent.change(input, { target: { value: "Beach" } });
    fireEvent.submit(input);
    await waitFor(() => expect(onRenamed).toHaveBeenCalledWith(expect.objectContaining({ name: "Beach" })));
    expect(mockApi.documents.update).toHaveBeenCalledWith("i2", { name: "Beach" });
  });

  it("deletes from the header after asking", async () => {
    mockApi.documents.delete.mockResolvedValue(undefined);
    const { onDeleted, onClose } = setup();
    fireEvent.click(screen.getByRole("button", { name: "Delete Holiday photo.jpg" }));
    const confirm = screen.getByRole("alertdialog");
    expect(within(confirm).getByText("Delete Holiday photo.jpg?")).toBeTruthy();
    fireEvent.click(within(confirm).getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(onDeleted).toHaveBeenCalledTimes(1));
    expect(mockApi.documents.delete).toHaveBeenCalledWith("i2");
    expect(onClose).not.toHaveBeenCalled();
  });

  it("closes on ×, Escape and the scrim", () => {
    const { onClose, container } = setup();
    fireEvent.click(screen.getByRole("button", { name: "Close document" }));
    fireEvent.keyDown(document.body, { key: "Escape" });
    fireEvent.click(container.querySelector('[aria-hidden="true"].absolute') as HTMLElement);
    expect(onClose).toHaveBeenCalledTimes(3);
  });
});

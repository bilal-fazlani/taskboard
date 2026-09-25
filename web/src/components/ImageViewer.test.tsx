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
    usage: vi.fn(),
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
    mockApi.documents.usage.mockResolvedValue({ places: [] });
    const { onDeleted, onClose } = setup();
    fireEvent.click(screen.getByRole("button", { name: "Delete Holiday photo.jpg" }));
    const confirm = screen.getByRole("alertdialog");
    expect(within(confirm).getByText("Delete Holiday photo.jpg?")).toBeTruthy();
    const del = within(confirm).getByRole("button", { name: "Delete" }) as HTMLButtonElement;
    await waitFor(() => expect(del.disabled).toBe(false));
    expect(confirm.textContent).not.toContain("used in");
    fireEvent.click(del);
    await waitFor(() => expect(onDeleted).toHaveBeenCalledTimes(1));
    expect(mockApi.documents.delete).toHaveBeenCalledWith("i2");
    expect(onClose).not.toHaveBeenCalled();
  });

  it("names the places that use the image before deleting it", async () => {
    mockApi.documents.delete.mockResolvedValue(undefined);
    mockApi.documents.usage.mockResolvedValue({
      places: [{ kind: "description" }, { kind: "document", documentId: "d1", name: "Design spec.md" }],
    });
    const { onDeleted } = setup(login);
    fireEvent.click(screen.getByRole("button", { name: "Delete Login screen.png" }));
    const confirm = screen.getByRole("alertdialog");
    await waitFor(() =>
      expect(confirm.textContent).toBe(
        "Delete Login screen.png?It's used in 2 places: the description and Design spec.md. They'll show a missing image. This can't be undone.CancelDelete",
      ),
    );
    expect(mockApi.documents.usage).toHaveBeenCalledWith("i1");
    fireEvent.click(within(confirm).getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(onDeleted).toHaveBeenCalledTimes(1));
    expect(mockApi.documents.delete).toHaveBeenCalledWith("i1");
  });

  it("closes on ×, Escape and the scrim", () => {
    const { onClose, container } = setup();
    fireEvent.click(screen.getByRole("button", { name: "Close document" }));
    fireEvent.keyDown(document.body, { key: "Escape" });
    fireEvent.click(container.querySelector('[aria-hidden="true"].absolute') as HTMLElement);
    expect(onClose).toHaveBeenCalledTimes(3);
  });
});

describe("stepping with ← and →", () => {
  it("steps to the next and the previous of the owner's images, skipping other documents", () => {
    const { onStep } = setup(photo);
    fireEvent.keyDown(document.body, { key: "ArrowRight" });
    expect(onStep).toHaveBeenLastCalledWith(spinner);
    fireEvent.keyDown(document.body, { key: "ArrowLeft" });
    expect(onStep).toHaveBeenLastCalledWith(login);
    expect(onStep).toHaveBeenCalledTimes(2);
  });

  it("stops at either end", () => {
    const first = setup(login);
    fireEvent.keyDown(document.body, { key: "ArrowLeft" });
    expect(first.onStep).not.toHaveBeenCalled();
    cleanup();
    const last = setup(spinner);
    fireEvent.keyDown(document.body, { key: "ArrowRight" });
    expect(last.onStep).not.toHaveBeenCalled();
  });

  it("shows the image it is handed next, fitted", () => {
    const { rerender } = setup(photo);
    fireEvent.click(screen.getByRole("button", { name: "100%" }));
    rerender({ doc: spinner });
    expect(screen.getByRole("dialog", { name: "Spinner.gif" })).toBeTruthy();
    expect(picture().getAttribute("src")).toBe("/api/documents/i3/image?rev=1");
    expect(picture().dataset.zoom).toBe("fit");
    expect(screen.getByTestId("image-facts").textContent).toBe("3 of 3 images · 64×64");
  });

  it("does nothing while the rename field is open", () => {
    const { onStep } = setup(photo);
    fireEvent.click(screen.getByRole("button", { name: "Rename Holiday photo.jpg" }));
    const input = screen.getByRole("textbox", { name: "Document name" });
    fireEvent.keyDown(input, { key: "ArrowRight" });
    // Even with focus moved off the field.
    fireEvent.keyDown(document.body, { key: "ArrowLeft" });
    expect(onStep).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    fireEvent.keyDown(document.body, { key: "ArrowRight" });
    expect(onStep).toHaveBeenCalledExactlyOnceWith(spinner);
  });

  it("does nothing while the delete question is up", () => {
    const { onStep } = setup(photo);
    fireEvent.click(screen.getByRole("button", { name: "Delete Holiday photo.jpg" }));
    const cancel = within(screen.getByRole("alertdialog")).getByRole("button", { name: "Cancel" });
    fireEvent.keyDown(cancel, { key: "ArrowRight" });
    fireEvent.keyDown(document.body, { key: "ArrowLeft" });
    expect(onStep).not.toHaveBeenCalled();
    fireEvent.click(cancel);
    fireEvent.keyDown(document.body, { key: "ArrowLeft" });
    expect(onStep).toHaveBeenCalledExactlyOnceWith(login);
  });

  it("leaves arrows alone in a field, with a modifier, or once handled", () => {
    const { onStep } = setup(photo);
    const field = document.createElement("input");
    document.body.appendChild(field);
    fireEvent.keyDown(field, { key: "ArrowRight" });
    field.remove();
    for (const mod of ["altKey", "ctrlKey", "metaKey", "shiftKey"]) fireEvent.keyDown(document.body, { key: "ArrowRight", [mod]: true });
    const handled = new KeyboardEvent("keydown", { key: "ArrowRight", bubbles: true, cancelable: true });
    handled.preventDefault();
    document.body.dispatchEvent(handled);
    expect(onStep).not.toHaveBeenCalled();
  });

  it("does not step from a text document", () => {
    mockApi.documents.get.mockResolvedValue({ ...spec, content: "x" });
    const { onStep } = setup(spec);
    fireEvent.keyDown(document.body, { key: "ArrowRight" });
    expect(onStep).not.toHaveBeenCalled();
  });
});

describe("previous and next buttons, and the hint", () => {
  const button = (name: string) => screen.getByRole("button", { name }) as HTMLButtonElement;

  it("step to the previous and next image, in the Tab order", () => {
    const { onStep } = setup(photo);
    for (const name of ["Previous image", "Next image"]) {
      expect(button(name).disabled).toBe(false);
      expect(button(name).tabIndex).toBe(0);
      expect(button(name).getAttribute("aria-disabled")).toBeNull();
    }
    fireEvent.click(button("Next image"));
    expect(onStep).toHaveBeenLastCalledWith(spinner);
    fireEvent.click(button("Previous image"));
    expect(onStep).toHaveBeenLastCalledWith(login);
    expect(onStep).toHaveBeenCalledTimes(2);
  });

  it("are disabled, but keep their place and focus, at the first and last image", () => {
    const first = setup(login);
    expect(button("Previous image").getAttribute("aria-disabled")).toBe("true");
    expect(button("Next image").getAttribute("aria-disabled")).toBeNull();
    button("Previous image").focus();
    fireEvent.click(button("Previous image"));
    expect(first.onStep).not.toHaveBeenCalled();
    expect(document.activeElement).toBe(button("Previous image"));
    cleanup();

    const last = setup(spinner);
    expect(button("Next image").getAttribute("aria-disabled")).toBe("true");
    fireEvent.click(button("Next image"));
    expect(last.onStep).not.toHaveBeenCalled();
  });

  it("keep focus on the button when a step lands on the last image", () => {
    const { rerender, onStep } = setup(photo);
    button("Next image").focus();
    fireEvent.click(button("Next image"));
    rerender({ doc: onStep.mock.calls[0][0] });
    expect(screen.getByRole("dialog", { name: "Spinner.gif" })).toBeTruthy();
    expect(document.activeElement).toBe(button("Next image"));
    expect(button("Next image").getAttribute("aria-disabled")).toBe("true");
  });

  it("do nothing while the rename field is open", () => {
    const { onStep } = setup(photo);
    fireEvent.click(screen.getByRole("button", { name: "Rename Holiday photo.jpg" }));
    expect(button("Next image").getAttribute("aria-disabled")).toBe("true");
    fireEvent.click(button("Next image"));
    expect(onStep).not.toHaveBeenCalled();
  });

  it("are not there for a lone image, or without a way to step", () => {
    render(
      <DocumentModal doc={login} documents={[spec, login]} owner={{ ticketId: "t1" }} ownerLabel="ACP-84"
        onClose={vi.fn()} onRenamed={vi.fn()} onDeleted={vi.fn()} onRecreated={vi.fn()} onStep={vi.fn()} />,
    );
    expect(screen.queryByRole("button", { name: "Next image" })).toBeNull();
    expect(screen.getByTestId("image-hint").textContent).toBe("Esc closes · Back closes");
    cleanup();
    setup(photo, { onStep: undefined });
    expect(screen.queryByRole("button", { name: "Previous image" })).toBeNull();
  });

  it("says how to move between the ticket's images, or the epic's", () => {
    setup(photo);
    expect(screen.getByTestId("image-hint").textContent).toBe("← → move between this ticket's images · Esc closes · Back closes");
    cleanup();
    setup(photo, { owner: { epicId: "e1" }, ownerNoun: "epic" });
    expect(screen.getByTestId("image-hint").textContent).toBe("← → move between this epic's images · Esc closes · Back closes");
  });

  it("is not shown for a text document", () => {
    mockApi.documents.get.mockResolvedValue({ ...spec, content: "x" });
    setup(spec);
    expect(screen.queryByTestId("image-hint")).toBeNull();
    expect(screen.queryByRole("button", { name: "Next image" })).toBeNull();
  });
});

describe("the 100% view and the keyboard", () => {
  // jsdom lays nothing out, so the region's scroll metrics are set by hand.
  function scrollable(el: HTMLElement, metrics: { scrollLeft: number; clientWidth: number; scrollWidth: number }) {
    for (const [key, value] of Object.entries(metrics)) Object.defineProperty(el, key, { configurable: true, value });
  }

  it("lets ← and → pan a wide image while it can still scroll that way, and steps at the edge", () => {
    const { onStep } = setup(photo);
    fireEvent.click(screen.getByRole("button", { name: "100%" }));
    const region = screen.getByTestId("image-scroll");
    region.focus();

    scrollable(region, { scrollLeft: 0, clientWidth: 500, scrollWidth: 4000 });
    // Not handled: the browser scrolls the region.
    expect(fireEvent.keyDown(region, { key: "ArrowRight" })).toBe(true);
    expect(onStep).not.toHaveBeenCalled();
    // Nothing further left: ← steps.
    expect(fireEvent.keyDown(region, { key: "ArrowLeft" })).toBe(false);
    expect(onStep).toHaveBeenLastCalledWith(login);

    scrollable(region, { scrollLeft: 1200, clientWidth: 500, scrollWidth: 4000 });
    expect(fireEvent.keyDown(region, { key: "ArrowLeft" })).toBe(true);
    expect(fireEvent.keyDown(region, { key: "ArrowRight" })).toBe(true);
    expect(onStep).toHaveBeenCalledTimes(1);

    scrollable(region, { scrollLeft: 3500, clientWidth: 500, scrollWidth: 4000 });
    expect(fireEvent.keyDown(region, { key: "ArrowRight" })).toBe(false);
    expect(onStep).toHaveBeenLastCalledWith(spinner);
  });

  it("steps as usual from the 100% view of an image that fits", () => {
    const { onStep } = setup(photo);
    fireEvent.click(screen.getByRole("button", { name: "100%" }));
    const region = screen.getByTestId("image-scroll");
    scrollable(region, { scrollLeft: 0, clientWidth: 800, scrollWidth: 800 });
    fireEvent.keyDown(region, { key: "ArrowRight" });
    expect(onStep).toHaveBeenCalledExactlyOnceWith(spinner);
  });

  it("keeps focus in the viewer when a step takes the focused 100% view away", () => {
    const { rerender } = setup(photo);
    fireEvent.click(screen.getByRole("button", { name: "100%" }));
    const region = screen.getByTestId("image-scroll");
    region.focus();
    expect(document.activeElement).toBe(region);
    rerender({ doc: spinner });
    const dialog = screen.getByRole("dialog", { name: "Spinner.gif" });
    expect(region.isConnected).toBe(false);
    expect(document.activeElement).toBe(dialog);
  });
});


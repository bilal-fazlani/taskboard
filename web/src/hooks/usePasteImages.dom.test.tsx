// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, createEvent, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useRef, useState } from "react";
import type { DocumentMeta, DocumentOwnerRef } from "../api/client";

const mockApi = vi.hoisted(() => ({ documents: { createImage: vi.fn() } }));
vi.mock("../api/client", () => ({ api: mockApi }));

import { usePasteImages } from "./usePasteImages";
import ImageUploadStatus from "../components/ImageUploadStatus";

function meta(id: string, name: string, format: DocumentMeta["format"] = "png"): DocumentMeta {
  return { id, name, format, size: 10, revision: 1, createdAt: "", updatedAt: "" };
}

function Harness({
  initial = "",
  owner = { ticketId: "t1" },
  documents = [],
  onEdit,
  onUploaded,
}: {
  initial?: string;
  owner?: DocumentOwnerRef | null;
  documents?: DocumentMeta[] | null;
  onEdit?: (next: string) => void;
  onUploaded?: (doc: DocumentMeta) => void;
}) {
  const [value, setValue] = useState(initial);
  const ref = useRef<HTMLTextAreaElement>(null);
  const change = (next: string) => {
    setValue(next);
    onEdit?.(next);
  };
  const paste = usePasteImages({ owner, documents, value, onChange: change, textareaRef: ref, onUploaded });
  return (
    <>
      <textarea ref={ref} aria-label="Text" value={value} onChange={(e) => change(e.target.value)} {...paste.textareaProps} />
      <ImageUploadStatus uploads={paste.uploads} problems={paste.problems} onDismiss={paste.dismissProblems} />
    </>
  );
}

function image(name: string, type = "image/png", size = 10): File {
  const f = new File(["x"], name, { type });
  if (size !== 10) Object.defineProperty(f, "size", { value: size });
  return f;
}

const textarea = () => screen.getByLabelText("Text") as HTMLTextAreaElement;

function paste(files: File[], types = ["Files"]) {
  const el = textarea();
  const event = createEvent.paste(el, { clipboardData: { types, files } });
  fireEvent(el, event);
  return event;
}

function drop(files: File[]) {
  const el = textarea();
  const dataTransfer = { types: ["Files"], files, items: files.map((f) => ({ kind: "file", type: f.type })), dropEffect: "" };
  const over = createEvent.dragOver(el, { dataTransfer });
  fireEvent(el, over);
  const event = createEvent.drop(el, { dataTransfer });
  fireEvent(el, event);
  return { over, event, dataTransfer };
}

function deferred<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function place(caret: number) {
  const el = textarea();
  el.focus();
  el.setSelectionRange(caret, caret);
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("usePasteImages: paste", () => {
  it("puts ![](Pasted image.png) at the cursor at once, through onChange, and uploads under that name", async () => {
    const upload = deferred<DocumentMeta>();
    mockApi.documents.createImage.mockReturnValue(upload.promise);
    const onEdit = vi.fn();
    const onUploaded = vi.fn();
    render(<Harness initial="Before after" onEdit={onEdit} onUploaded={onUploaded} />);
    place(7);
    const event = paste([image("image.png")]);
    expect(event.defaultPrevented).toBe(true);
    expect(textarea().value).toBe("Before ![](Pasted image.png)after");
    expect(onEdit).toHaveBeenCalledWith("Before ![](Pasted image.png)after");
    expect(textarea().selectionStart).toBe(7 + "![](Pasted image.png)".length);
    expect(screen.getByRole("status").textContent).toBe("Uploading Pasted image.png… it appears in Documents when done");
    const [owner, sent] = mockApi.documents.createImage.mock.calls[0];
    expect(owner).toEqual({ ticketId: "t1" });
    expect((sent as File).name).toBe("Pasted image.png");

    const added = meta("d1", "Pasted image");
    await act(async () => upload.resolve(added));
    expect(onUploaded).toHaveBeenCalledWith(added);
    expect(screen.queryByRole("status")).toBeNull();
    expect(textarea().value).toBe("Before ![](Pasted image.png)after");
  });

  it("names pastes Pasted image, Pasted image 2… skipping the owner's names and those under way", () => {
    mockApi.documents.createImage.mockReturnValue(new Promise(() => {}));
    render(<Harness documents={[meta("d1", "pasted image"), meta("d2", "Pasted image 3", "jpeg")]} />);
    place(0);
    paste([image("image.png")]);
    paste([image("image.png")]);
    expect(textarea().value).toBe("![](Pasted image 2.png)![](Pasted image 4.png)");
    expect(screen.getAllByRole("status").map((s) => s.textContent)).toEqual([
      "Uploading Pasted image 2.png… it appears in Documents when done",
      "Uploading Pasted image 4.png… it appears in Documents when done",
    ]);
  });

  it("gives several images pasted at once a reference each, in order", () => {
    mockApi.documents.createImage.mockReturnValue(new Promise(() => {}));
    render(<Harness />);
    place(0);
    paste([image("a.png"), image("b.gif", "image/gif")]);
    expect(textarea().value).toBe("![](Pasted image.png)\n![](Pasted image 2.gif)");
    expect(mockApi.documents.createImage.mock.calls.map((c) => (c[1] as File).name)).toEqual([
      "Pasted image.png",
      "Pasted image 2.gif",
    ]);
  });

  it("leaves a text paste to the browser, even when the clipboard also has an image", () => {
    render(<Harness initial="abc" />);
    place(1);
    const event = paste([image("image.png")], ["text/plain", "text/html", "Files"]);
    expect(event.defaultPrevented).toBe(false);
    expect(textarea().value).toBe("abc");
    expect(mockApi.documents.createImage).not.toHaveBeenCalled();
    expect(paste([], ["text/plain"]).defaultPrevented).toBe(false);
  });

  it("takes the next free name when the server says the one picked was taken meanwhile", async () => {
    mockApi.documents.createImage
      .mockRejectedValueOnce(new Error('API error 400: {"error":"This ticket already has a document called \\"Pasted image.png\\"."}'))
      .mockResolvedValueOnce(meta("d2", "Pasted image 2"));
    render(<Harness initial="x" />);
    place(1);
    paste([image("image.png")]);
    await waitFor(() => expect(textarea().value).toBe("x![](Pasted image 2.png)"));
    expect((mockApi.documents.createImage.mock.calls[1][1] as File).name).toBe("Pasted image 2.png");
    await waitFor(() => expect(screen.queryByRole("status")).toBeNull());
    expect(screen.queryByRole("alert")).toBeNull();
  });
});

describe("usePasteImages: drop", () => {
  it("keeps each dropped file's converted name, in order, and never lets the browser open a file", () => {
    mockApi.documents.createImage.mockReturnValue(new Promise(() => {}));
    render(<Harness initial="ab" documents={[meta("d1", "Pasted image")]} />);
    place(1);
    const { over, event, dataTransfer } = drop([image("login (v2).png"), image("photo.JPEG", "image/jpeg")]);
    expect(over.defaultPrevented).toBe(true);
    expect(dataTransfer.dropEffect).toBe("copy");
    expect(event.defaultPrevented).toBe(true);
    expect(textarea().value).toBe("a![](login  v2.png)\n![](photo.jpg)b");
    expect(mockApi.documents.createImage.mock.calls.map((c) => (c[1] as File).name)).toEqual(["login  v2.png", "photo.jpg"]);
  });

  it("does nothing special with a file that is not an image", () => {
    render(<Harness initial="ab" />);
    place(1);
    const { over, event, dataTransfer } = drop([new File(["# x"], "notes.md", { type: "text/markdown" })]);
    expect(over.defaultPrevented).toBe(true);
    expect(dataTransfer.dropEffect).toBe("none");
    expect(event.defaultPrevented).toBe(true);
    expect(textarea().value).toBe("ab");
    expect(mockApi.documents.createImage).not.toHaveBeenCalled();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("uploads only the images of a drop that mixes them with other files", () => {
    mockApi.documents.createImage.mockReturnValue(new Promise(() => {}));
    render(<Harness />);
    place(0);
    drop([new File(["# x"], "notes.md", { type: "text/markdown" }), image("shot.png")]);
    expect(textarea().value).toBe("![](shot.png)");
    expect(mockApi.documents.createImage).toHaveBeenCalledTimes(1);
  });
});

describe("usePasteImages: checks and failures", () => {
  it("checks type and size before uploading: nothing goes in, and the reason shows", () => {
    render(<Harness initial="ab" />);
    place(1);
    drop([image("logo.svg", "image/svg+xml"), image("huge.png", "image/png", 9 * 1024 * 1024)]);
    expect(textarea().value).toBe("ab");
    expect(mockApi.documents.createImage).not.toHaveBeenCalled();
    expect(screen.getByRole("alert").textContent).toContain(
      "logo.svg wasn't uploaded: SVG images can't be attached. Use PNG, JPEG, GIF or WebP.",
    );
    expect(screen.getByRole("alert").textContent).toContain("huge.png wasn't uploaded: This image is 9.0 MB. The limit is 8 MB.");
    fireEvent.click(screen.getByRole("button", { name: "Dismiss image message" }));
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("refuses a dropped file whose name the owner already has, before uploading", () => {
    render(<Harness documents={[meta("d1", "Shot")]} />);
    place(0);
    drop([image("shot.png")]);
    expect(textarea().value).toBe("");
    expect(screen.getByRole("alert").textContent).toContain('shot.png wasn\'t uploaded: This ticket already has a document called "Shot.png".');
  });

  it("takes out exactly its reference when the server refuses, wherever typing moved it", async () => {
    const first = deferred<DocumentMeta>();
    const second = deferred<DocumentMeta>();
    mockApi.documents.createImage.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    const onEdit = vi.fn();
    render(<Harness initial="ab" onEdit={onEdit} />);
    place(1);
    paste([image("image.png")]);
    paste([image("image.png")]);
    expect(textarea().value).toBe("a![](Pasted image.png)![](Pasted image 2.png)b");
    // The user types before, between and after the references, a key at a time.
    for (const value of [
      "S a![](Pasted image.png)![](Pasted image 2.png)b",
      "Start a![](Pasted image.png)![](Pasted image 2.png)b",
      "Start a![](Pasted image.png) and ![](Pasted image 2.png)b",
      "Start a![](Pasted image.png) and ![](Pasted image 2.png)b end",
    ]) {
      fireEvent.change(textarea(), { target: { value } });
    }
    onEdit.mockClear();
    await act(async () => first.reject(new Error('API error 400: {"error":"This isn\'t a PNG image: its content is JPEG."}')));
    expect(textarea().value).toBe("Start a and ![](Pasted image 2.png)b end");
    expect(onEdit).toHaveBeenCalledWith("Start a and ![](Pasted image 2.png)b end");
    expect(screen.getByRole("alert").textContent).toContain("Pasted image.png wasn't uploaded: This isn't a PNG image: its content is JPEG.");
    expect(screen.getAllByRole("status")).toHaveLength(1);
    await act(async () => second.reject(new Error("Failed to fetch")));
    expect(textarea().value).toBe("Start a and b end");
    expect(screen.getByRole("alert").textContent).toContain("Pasted image 2.png wasn't uploaded: The image was not uploaded.");
  });

  it("leaves a reference the user has edited since, and the rest of the text, as they are", async () => {
    const upload = deferred<DocumentMeta>();
    mockApi.documents.createImage.mockReturnValue(upload.promise);
    render(<Harness initial="" />);
    place(0);
    paste([image("image.png")]);
    fireEvent.change(textarea(), { target: { value: "![Login](Pasted image.png)" } });
    await act(async () => upload.reject(new Error("API error 500: boom")));
    expect(textarea().value).toBe("![Login](Pasted image.png)");
    expect(screen.getByRole("alert")).toBeTruthy();
  });

  it("frees a failed paste's name for the next one", async () => {
    mockApi.documents.createImage.mockRejectedValueOnce(new Error("API error 500: boom")).mockReturnValue(new Promise(() => {}));
    render(<Harness />);
    place(0);
    paste([image("image.png")]);
    await waitFor(() => expect(screen.getByRole("alert")).toBeTruthy());
    paste([image("image.png")]);
    expect(textarea().value).toBe("![](Pasted image.png)");
  });

  it("says images wait for an owner that does not exist yet, and leaves the text alone", () => {
    render(<Harness owner={null} documents={null} initial="New" />);
    place(3);
    expect(paste([image("image.png")]).defaultPrevented).toBe(true);
    expect(textarea().value).toBe("New");
    expect(screen.getByRole("alert").textContent).toContain("Images can be pasted or dropped once the ticket is created.");
    fireEvent.click(screen.getByRole("button", { name: "Dismiss image message" }));
    expect(drop([image("shot.png")]).event.defaultPrevented).toBe(true);
    expect(textarea().value).toBe("New");
    expect(screen.getByRole("alert")).toBeTruthy();
    expect(mockApi.documents.createImage).not.toHaveBeenCalled();
  });
});

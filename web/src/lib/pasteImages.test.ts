import { describe, expect, it } from "vitest";
import type { DocumentMeta } from "../api/client";
import { findOwnerImage } from "./imageRefs";
import {
  draggingFiles,
  followRange,
  imageReference,
  isImageFile,
  pastedImageFiles,
  pastedImageName,
  prepareImage,
} from "./pasteImages";

function doc(name: string, format: DocumentMeta["format"] = "png"): Pick<DocumentMeta, "id" | "name" | "format"> {
  return { id: name, name, format };
}

function file(name: string, type: string, size = 10): File {
  const f = new File(["x"], name, { type });
  Object.defineProperty(f, "size", { value: size });
  return f;
}

describe("pastedImageName", () => {
  it("is Pasted image, then the first free number, ignoring case", () => {
    expect(pastedImageName([])).toBe("Pasted image");
    expect(pastedImageName(["pasted IMAGE"])).toBe("Pasted image 2");
    expect(pastedImageName(["Pasted image", "Pasted image 2", "pasted image 4"])).toBe("Pasted image 3");
  });
});

describe("prepareImage", () => {
  it("names a paste by the name given, with its type's extension", () => {
    const image = prepareImage(file("image.png", "image/png"), [], "ticket", "Pasted image 2");
    expect(image).toMatchObject({ name: "Pasted image 2", format: "png", shown: "Pasted image 2.png" });
    expect("file" in image && image.file.name).toBe("Pasted image 2.png");
    const jpeg = prepareImage(file("image.jpeg", "image/jpeg"), [], "ticket", "Pasted image");
    expect(jpeg).toMatchObject({ format: "jpeg", shown: "Pasted image.jpg" });
  });

  it("keeps a dropped file's name, converted by the name rules", () => {
    expect(prepareImage(file("login (v2).PNG", "image/png"), [], "ticket")).toMatchObject({
      name: "login  v2",
      format: "png",
      shown: "login  v2.png",
    });
    expect(prepareImage(file("photo.jpeg", "image/jpeg"), [], "epic")).toMatchObject({ shown: "photo.jpg", format: "jpeg" });
  });

  it("refuses SVG, other types, files over 8 MB and taken names in the server's words", () => {
    expect(prepareImage(file("logo.svg", "image/svg+xml"), [], "ticket")).toEqual({
      error: "SVG images can't be attached. Use PNG, JPEG, GIF or WebP.",
      shown: "logo.svg",
    });
    expect(prepareImage(file("image.svg", "image/svg+xml"), [], "ticket", "Pasted image")).toMatchObject({
      error: "SVG images can't be attached. Use PNG, JPEG, GIF or WebP.",
    });
    expect(prepareImage(file("scan.tiff", "image/tiff"), [], "ticket")).toMatchObject({
      error: "Only .md, .html, .htm, .png, .jpg, .jpeg, .gif and .webp files can be attached.",
    });
    expect(prepareImage(file("big.png", "image/png", 9 * 1024 * 1024), [], "ticket")).toEqual({
      error: "This image is 9.0 MB. The limit is 8 MB.",
      shown: "big.png",
    });
    expect(prepareImage(file("shot.png", "image/png"), [doc("Shot", "jpeg")], "epic")).toEqual({
      error: 'This epic already has a document called "Shot.jpg".',
      shown: "shot.png",
    });
  });
});

describe("imageReference", () => {
  it("is the bare form the ticket asks for, and finds the image", () => {
    expect(imageReference("Pasted image 2.png")).toBe("![](Pasted image 2.png)");
    expect(imageReference("login_v2.png")).toBe("![](login_v2.png)");
    expect(findOwnerImage([{ ...doc("Pasted image 2"), revision: 1 }], "Pasted image 2.png")).toBeTruthy();
  });

  it("angle-brackets a name whose underscores could read as emphasis", () => {
    expect(imageReference("my _draft_ shot.png")).toBe("![](<my _draft_ shot.png>)");
  });
});

describe("followRange", () => {
  const ref = "![](a.png)";
  const text = `one ${ref} two`;
  it("keeps the place of text edited after, and moves it for text edited before", () => {
    expect(followRange(text, `${text}!`, 4, ref)).toBe(4);
    expect(followRange(text, `one ${ref}X two`, 4, ref)).toBe(4);
    expect(followRange(text, `zero, ${text}`, 4, ref)).toBe(10);
    expect(followRange(text, `on ${ref} two`, 4, ref)).toBe(3);
  });

  it("finds the text when the same characters were typed right beside it", () => {
    expect(followRange(text, `one !${ref} two`, 4, ref)).toBe(5);
    expect(followRange(text, `one ${ref}) two`, 4, ref)).toBe(4);
  });

  it("finds the text after an edit of several places, when it is there once", () => {
    expect(followRange(text, `One, ${ref} and two`, 4, ref)).toBe(5);
    expect(followRange(text, `One, ${ref} and ${ref}`, 4, ref)).toBeNull();
  });

  it("is null once the edit touched the text", () => {
    expect(followRange(text, `one ![Login](a.png) two`, 4, ref)).toBeNull();
    expect(followRange(text, "", 4, ref)).toBeNull();
  });
});

describe("clipboard and drag data", () => {
  const transfer = (types: string[], files: File[] = []) => ({ types, files: files as unknown as FileList });
  it("takes the clipboard's images unless it carries text", () => {
    const png = file("image.png", "image/png");
    expect(pastedImageFiles(transfer(["Files"], [png]))).toEqual([png]);
    expect(pastedImageFiles(transfer(["text/html", "Files"], [png]))).toEqual([png]);
    expect(pastedImageFiles(transfer(["text/plain", "Files"], [png]))).toEqual([]);
    expect(pastedImageFiles(transfer(["text/plain"]))).toEqual([]);
    expect(pastedImageFiles(null)).toEqual([]);
  });

  it("counts images by type or extension, SVG included", () => {
    expect(isImageFile(file("a.png", ""))).toBe(true);
    expect(isImageFile(file("a", "image/png"))).toBe(true);
    expect(isImageFile(file("a.svg", ""))).toBe(true);
    expect(isImageFile(file("notes.md", "text/markdown"))).toBe(false);
    expect(draggingFiles({ types: ["Files"] } as unknown as DataTransfer)).toBe(true);
    expect(draggingFiles({ types: ["text/plain"] } as unknown as DataTransfer)).toBe(false);
  });
});

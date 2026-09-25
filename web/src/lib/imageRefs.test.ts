import { describe, expect, it } from "vitest";
import type { DocumentMeta } from "../api/client";
import { findOwnerImage, ownerImageUrl, resolveImageRef } from "./imageRefs";

const at = "2026-09-25T09:00:00Z";
const login: DocumentMeta = { id: "img1", name: "Login screen", format: "png", size: 10, revision: 2, createdAt: at, updatedAt: at };
const photo: DocumentMeta = { id: "img2", name: "IMG_0042", format: "jpeg", size: 10, revision: 1, createdAt: at, updatedAt: at };
const plan: DocumentMeta = { id: "doc1", name: "Plan", format: "markdown", size: 10, revision: 1, createdAt: at, updatedAt: at };
const docs = [login, photo, plan];

describe("findOwnerImage", () => {
  it("matches the display name ignoring case", () => {
    expect(findOwnerImage(docs, "Login screen.png")).toBe(login);
    expect(findOwnerImage(docs, "LOGIN SCREEN.PNG")).toBe(login);
    expect(findOwnerImage(docs, "  login screen.png ")).toBe(login);
  });

  it("answers to .jpeg as well as .jpg for a JPEG", () => {
    expect(findOwnerImage(docs, "IMG_0042.jpg")).toBe(photo);
    expect(findOwnerImage(docs, "img_0042.JPEG")).toBe(photo);
    expect(findOwnerImage(docs, "Login screen.jpeg")).toBeUndefined();
  });

  it("needs the extension, and only matches images", () => {
    expect(findOwnerImage(docs, "Login screen")).toBeUndefined();
    expect(findOwnerImage(docs, "Login screen.gif")).toBeUndefined();
    expect(findOwnerImage(docs, "Plan.md")).toBeUndefined();
    expect(findOwnerImage(docs, "")).toBeUndefined();
  });
});

describe("resolveImageRef", () => {
  it("resolves plain, percent-encoded and spaced names to the image route", () => {
    for (const src of ["Login screen.png", "Login%20screen.png", "login%20SCREEN.png"]) {
      expect(resolveImageRef(src, docs)).toEqual({ kind: "owner", doc: login, src: "/api/documents/img1/image?rev=2" });
    }
    expect(ownerImageUrl(photo)).toBe("/api/documents/img2/image?rev=1");
  });

  it("keeps absolute http(s) URLs as they are", () => {
    expect(resolveImageRef("https://example.com/a.png", docs)).toEqual({ kind: "absolute", src: "https://example.com/a.png" });
    expect(resolveImageRef("HTTP://example.com/a.png", docs).kind).toBe("absolute");
    expect(resolveImageRef("//example.com/a.png", docs).kind).toBe("absolute");
  });

  it("makes anything else missing, including a path on this server", () => {
    expect(resolveImageRef("Nope.png", docs)).toEqual({ kind: "missing", name: "Nope.png" });
    expect(resolveImageRef("/api/documents/img9/image", docs).kind).toBe("missing");
    expect(resolveImageRef("./Login screen.png", docs).kind).toBe("missing");
    expect(resolveImageRef("", docs)).toEqual({ kind: "missing", name: "" });
    expect(resolveImageRef(undefined, docs).kind).toBe("missing");
    expect(resolveImageRef("bad%E0%A4%A.png", docs)).toEqual({ kind: "missing", name: "bad%E0%A4%A.png" });
  });
});

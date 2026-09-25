import { describe, expect, it } from "vitest";
import type { DocumentMeta } from "../api/client";
import {
  conflictDocument,
  contentTooLarge,
  displayName,
  DOCUMENT_SANDBOX,
  documentNameError,
  documentWindowKey,
  findDocument,
  formatSize,
  imageDimensions,
  imagesOf,
  isImageFormat,
  isNotFound,
  isTextFormat,
  nameFromFilename,
  ownerKey,
  tooLargeMessage,
  withDoc,
  withoutDoc,
} from "./documents";

function doc(id: string, name: string, format: DocumentMeta["format"] = "markdown"): DocumentMeta {
  return { id, name, format, size: 0, revision: 1, createdAt: "", updatedAt: "" };
}

describe("displayName", () => {
  it("adds the format's extension", () => {
    expect(displayName(doc("1", "Design spec"))).toBe("Design spec.md");
    expect(displayName(doc("2", "Report", "html"))).toBe("Report.html");
  });

  it("names images as the server does, a JPEG as .jpg", () => {
    expect(displayName(doc("3", "Login screen", "png"))).toBe("Login screen.png");
    expect(displayName(doc("4", "Holiday photo", "jpeg"))).toBe("Holiday photo.jpg");
    expect(displayName(doc("5", "Spinner", "gif"))).toBe("Spinner.gif");
    expect(displayName(doc("6", "Mock", "webp"))).toBe("Mock.webp");
  });
});

describe("images and the doc parameter", () => {
  const docs = [doc("1", "Plan"), doc("2", "Login screen", "png"), doc("3", "Holiday photo", "jpeg")];

  it("finds an image by the name the server's links carry", () => {
    // weburl.TicketDocument puts the display name in `doc`, e.g. doc=Holiday+photo.jpg.
    const params = new URLSearchParams("ticket=ACP-1&doc=Holiday+photo.jpg");
    expect(findDocument(docs, params.get("doc")!)?.id).toBe("3");
    expect(findDocument(docs, "login screen.PNG")?.id).toBe("2");
    expect(findDocument(docs, "Login screen.md")).toBeUndefined();
  });

  it("links to an image by its display name", () => {
    expect(withDoc(new URLSearchParams("ticket=ACP-1"), docs[1]).get("doc")).toBe("Login screen.png");
  });

  it("tells text formats from images", () => {
    expect(["markdown", "html", "png", "jpeg", "gif", "webp"].map((f) => isTextFormat(f as DocumentMeta["format"]))).toEqual([
      true,
      true,
      false,
      false,
      false,
      false,
    ]);
  });
});

describe("documentNameError", () => {
  const docs = [doc("1", "Étude"), doc("2", "Plan")];

  it("accepts letters in any script, digits, spaces, _ and -", () => {
    expect(documentNameError("  api-design_v2 ")).toBeNull();
    expect(documentNameError("設計 2")).toBeNull();
    expect(documentNameError("a".repeat(200))).toBeNull();
  });

  it("refuses empty, symbols and overlong names", () => {
    expect(documentNameError("")).toBe("Enter a name");
    expect(documentNameError("   ")).toBe("Enter a name");
    expect(documentNameError("plan.md")).toBe("Use letters, digits, spaces, _ and - only.");
    expect(documentNameError("a/b")).toBe("Use letters, digits, spaces, _ and - only.");
    expect(documentNameError("smile 🙂")).toBe("Use letters, digits, spaces, _ and - only.");
    expect(documentNameError("a".repeat(201))).toBe("Keep the name to 200 characters or fewer.");
  });

  it("refuses a name another document has, ignoring case, but not the document's own", () => {
    expect(documentNameError("étude", docs)).toBe('This ticket already has a document called "Étude.md".');
    expect(documentNameError("PLAN", docs, "2")).toBeNull();
  });
});

describe("formatSize", () => {
  it("matches the server's sizes", () => {
    expect(formatSize(0)).toBe("0 B");
    expect(formatSize(1023)).toBe("1023 B");
    expect(formatSize(1025)).toBe("2 KB");
    expect(formatSize(8 * 1024 * 1024)).toBe("8.0 MB");
    expect(formatSize(8 * 1024 * 1024 + 1)).toBe("8.1 MB");
  });
});

describe("findDocument", () => {
  const docs = [doc("1", "Design spec"), doc("2", "Report", "html")];

  it("finds by display name, ignoring case", () => {
    expect(findDocument(docs, "design SPEC.md")?.id).toBe("1");
    expect(findDocument(docs, " Report.html ")?.id).toBe("2");
  });

  it("finds nothing for the wrong extension or a bare name", () => {
    expect(findDocument(docs, "Design spec.html")).toBeUndefined();
    expect(findDocument(docs, "Design spec")).toBeUndefined();
    expect(findDocument(docs, "")).toBeUndefined();
  });
});

describe("doc parameter", () => {
  it("sets and drops doc, keeping the rest", () => {
    const params = new URLSearchParams("project=ACP&ticket=ACP-7");
    expect(withDoc(params, doc("1", "Design spec")).get("doc")).toBe("Design spec.md");
    expect(withoutDoc(new URLSearchParams("ticket=ACP-7&doc=x.md&q=a")).toString()).toBe("ticket=ACP-7&q=a");
  });
});

describe("ownerKey", () => {
  it("names the owner", () => {
    expect(ownerKey({ ticketId: "t1" })).toBe("ticket:t1");
  });

  it("tells tickets and epics apart", () => {
    expect(ownerKey({ epicId: "e1" })).toBe("epic:e1");
    expect(ownerKey({ epicId: "x" })).not.toBe(ownerKey({ ticketId: "x" }));
  });
});

describe("nameFromFilename", () => {
  it("drops the extension and turns other symbols into spaces", () => {
    expect(nameFromFilename("api-design_v2.md")).toEqual({ name: "api-design_v2", format: "markdown" });
    expect(nameFromFilename("notes v1.2.MD")).toEqual({ name: "notes v1 2", format: "markdown" });
    expect(nameFromFilename("dir/Étude (draft).md")).toEqual({ name: "Étude  draft", format: "markdown" });
  });

  it("refuses other types and names that clean to nothing", () => {
    const refused = "Only .md, .html, .htm, .png, .jpg, .jpeg, .gif and .webp files can be attached.";
    expect(nameFromFilename("notes.txt")).toEqual({ error: refused });
    expect(nameFromFilename("README")).toEqual({ error: refused });
    expect(nameFromFilename(".md")).toEqual({ error: "Enter a name" });
    expect(nameFromFilename("%%.md")).toEqual({ error: "Enter a name" });
  });
});

describe("image files", () => {
  it("takes .png, .jpg, .jpeg, .gif and .webp as images, in any case, a .jpg and .jpeg as JPEG", () => {
    expect(nameFromFilename("Login screen.png")).toEqual({ name: "Login screen", format: "png" });
    expect(nameFromFilename("dir/Holiday photo.JPG")).toEqual({ name: "Holiday photo", format: "jpeg" });
    expect(nameFromFilename("scan.jpeg")).toEqual({ name: "scan", format: "jpeg" });
    expect(nameFromFilename("spinner.gif")).toEqual({ name: "spinner", format: "gif" });
    expect(nameFromFilename("mock-v2.webp")).toEqual({ name: "mock-v2", format: "webp" });
  });

  it("refuses an SVG in the server's words", () => {
    expect(nameFromFilename("logo.SVG")).toEqual({ error: "SVG images can't be attached. Use PNG, JPEG, GIF or WebP." });
  });

  it("says an image, not a document, is over the limit", () => {
    expect(tooLargeMessage(9 * 1024 * 1024, "png")).toBe("This image is 9.0 MB. The limit is 8 MB.");
    expect(tooLargeMessage(9 * 1024 * 1024, "markdown")).toBe("This document is 9.0 MB. The limit is 8 MB.");
    expect(tooLargeMessage(9 * 1024 * 1024)).toBe("This document is 9.0 MB. The limit is 8 MB.");
  });

  it("lists an owner's images in order, with their size in pixels", () => {
    const docs = [doc("1", "Plan"), doc("2", "Shot", "png"), doc("3", "Page", "html"), doc("4", "Photo", "jpeg")];
    expect(imagesOf(docs).map((d) => d.id)).toEqual(["2", "4"]);
    expect(isImageFormat("webp")).toBe(true);
    expect(isImageFormat("html")).toBe(false);
    expect(imageDimensions({ width: 1280, height: 800 })).toBe("1280×800");
    expect(imageDimensions({})).toBe("");
  });

  it("keeps one document window for every image, and one per text document", () => {
    expect(documentWindowKey(doc("2", "Shot", "png"))).toBe(documentWindowKey(doc("4", "Photo", "gif")));
    expect(documentWindowKey(doc("1", "Plan"))).toBe("1");
  });
});

describe("HTML files", () => {
  it("takes .html and .htm as HTML, in any case", () => {
    expect(nameFromFilename("Load test.HTM")).toEqual({ name: "Load test", format: "html" });
    expect(nameFromFilename("report.html")).toEqual({ name: "report", format: "html" });
    expect(nameFromFilename("dir/Report.Html")).toEqual({ name: "Report", format: "html" });
    expect(nameFromFilename("%%.htm")).toEqual({ error: "Enter a name" });
  });

  it("sandboxes the frame with scripts only", () => {
    expect(DOCUMENT_SANDBOX).toBe("allow-scripts");
    for (const token of ["allow-same-origin", "allow-top-navigation", "allow-popups", "allow-forms", "allow-modals", "allow-downloads"]) {
      expect(DOCUMENT_SANDBOX).not.toContain(token);
    }
  });
});

describe("server errors", () => {
  const current = { id: "d1", name: "Plan", format: "markdown", size: 6, revision: 2, createdAt: "", updatedAt: "", content: "theirs" };

  it("reads the current document out of a 409", () => {
    const err = new Error(`API error 409: ${JSON.stringify({ error: "changed", current })}`);
    expect(conflictDocument(err)).toEqual(current);
    expect(conflictDocument(new Error('API error 409: {"error":"changed"}'))).toBeNull();
    expect(conflictDocument(new Error("API error 409: not json"))).toBeNull();
    expect(conflictDocument(new Error('API error 400: {"error":"x"}'))).toBeNull();
    expect(conflictDocument("nope")).toBeNull();
  });

  it("recognises a 404", () => {
    expect(isNotFound(new Error('API error 404: {"error":"document not found"}'))).toBe(true);
    expect(isNotFound(new Error("API error 500: x"))).toBe(false);
    expect(isNotFound(null)).toBe(false);
  });
});

describe("contentTooLarge", () => {
  it("measures UTF-8 bytes against the limit", () => {
    expect(contentTooLarge("x".repeat(8 * 1024 * 1024))).toBeNull();
    expect(contentTooLarge("x".repeat(8 * 1024 * 1024 + 1))).toBe("This document is 8.1 MB. The limit is 8 MB.");
    // Four bytes each in UTF-8, though two UTF-16 units.
    expect(contentTooLarge("😀".repeat(2 * 1024 * 1024 + 1))).toBe("This document is 8.1 MB. The limit is 8 MB.");
  });
});

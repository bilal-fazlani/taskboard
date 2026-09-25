import { describe, expect, it } from "vitest";
import type { DocumentMeta } from "../api/client";
import {
  displayName,
  documentNameError,
  findDocument,
  formatSize,
  ownerKey,
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
});

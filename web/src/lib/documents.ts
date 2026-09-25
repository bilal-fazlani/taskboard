// The document rules the web UI checks before it asks the server, worded
// exactly as the server words them (internal/db/documents.go), plus the `doc`
// query parameter that names the open document.
import type { DocumentFormat, DocumentMeta, DocumentOwnerRef } from "../api/client";

/** The query parameter naming the open document by its display name. */
export const DOC_PARAM = "doc";

export const MAX_DOCUMENT_BYTES = 8 * 1024 * 1024;
const MAX_NAME_LENGTH = 200;

// Letters in any script with their combining marks, decimal digits, space,
// underscore and hyphen: Go's unicode.IsLetter/IsMark/IsDigit.
const NAME_CHAR = /^[\p{L}\p{M}\p{Nd} _-]$/u;

export function extensionFor(format: DocumentFormat): string {
  return format === "html" ? ".html" : ".md";
}

/** How a document is shown, linked and downloaded: "Design spec.md". */
export function displayName(doc: Pick<DocumentMeta, "name" | "format">): string {
  return doc.name + extensionFor(doc.format);
}

/**
 * Why a name would be refused, or null. `others` are the owner's documents,
 * `exceptId` the one being renamed, which may keep its own name.
 */
export function documentNameError(
  raw: string,
  others: readonly Pick<DocumentMeta, "id" | "name" | "format">[] = [],
  exceptId?: string,
  ownerNoun = "ticket",
): string | null {
  const name = raw.trim();
  if (name === "") return "Enter a name";
  const chars = [...name];
  if (chars.length > MAX_NAME_LENGTH) return "Keep the name to 200 characters or fewer.";
  if (!chars.every((c) => NAME_CHAR.test(c))) return "Use letters, digits, spaces, _ and - only.";
  const lower = name.toLowerCase();
  const taken = others.find((d) => d.id !== exceptId && d.name.toLowerCase() === lower);
  if (taken) return `This ${ownerNoun} already has a document called "${displayName(taken)}".`;
  return null;
}

/** A byte count as the server shows it (models.FormatSize): sizes round up. */
export function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.ceil(bytes / 1024)} KB`;
  return `${(Math.ceil((bytes * 10) / (1024 * 1024)) / 10).toFixed(1)} MB`;
}

/** The document a `doc` value names: its display name, ignoring case. */
export function findDocument<T extends Pick<DocumentMeta, "name" | "format">>(
  docs: readonly T[],
  ref: string,
): T | undefined {
  const wanted = ref.trim().toLowerCase();
  if (!wanted) return undefined;
  return docs.find((d) => displayName(d).toLowerCase() === wanted);
}

export function withDoc(params: URLSearchParams, doc: Pick<DocumentMeta, "name" | "format">): URLSearchParams {
  const next = new URLSearchParams(params);
  next.set(DOC_PARAM, displayName(doc));
  return next;
}

export function withoutDoc(params: URLSearchParams): URLSearchParams {
  const next = new URLSearchParams(params);
  next.delete(DOC_PARAM);
  return next;
}

/** A stable key for an owner, for effects and caches. */
export function ownerKey(owner: DocumentOwnerRef): string {
  return `ticket:${owner.ticketId}`;
}

// The document rules the web UI checks before it asks the server, worded
// exactly as the server words them (internal/db/documents.go), plus the `doc`
// query parameter that names the open document.
import type { DocumentFormat, DocumentMeta, DocumentOwnerRef, DocumentWithContent } from "../api/client";

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

const FORMAT_BY_EXTENSION: Record<string, DocumentFormat> = { ".md": "markdown", ".html": "html", ".htm": "html" };
const EXTENSION_MESSAGE = "Only .md, .html and .htm files can be attached.";

/** What the upload's file picker offers. */
export const UPLOAD_ACCEPT = Object.keys(FORMAT_BY_EXTENSION).join(",");

/**
 * The sandbox an HTML document's frame runs in: scripts only. It must match
 * the server's Content-Security-Policy (documentSandbox in
 * internal/server/documents.go). Never add allow-same-origin (the page would
 * act as the board), allow-top-navigation or allow-popups; allow-forms,
 * allow-modals and allow-downloads were ruled out too.
 */
export const DOCUMENT_SANDBOX = "allow-scripts";

/**
 * A document name and format from a file's name, as the server makes them
 * (DocumentNameFromFilename): the extension picks the format and goes, and
 * every character the name rules refuse becomes a space.
 */
export function nameFromFilename(filename: string): { name: string; format: DocumentFormat } | { error: string } {
  const base = filename.split(/[\\/]/).pop() ?? "";
  const dot = base.lastIndexOf(".");
  const format = dot >= 0 ? FORMAT_BY_EXTENSION[base.slice(dot).toLowerCase()] : undefined;
  if (!format) return { error: EXTENSION_MESSAGE };
  const name = [...base.slice(0, dot)].map((c) => (NAME_CHAR.test(c) ? c : " ")).join("").trim();
  const error = documentNameError(name);
  return error ? { error } : { name, format };
}

const API_ERROR = /^API error (\d{3}): ?([\s\S]*)$/;

function apiError(error: unknown): { status: string; body: string } | null {
  const raw = error instanceof Error ? error.message : "";
  const match = API_ERROR.exec(raw);
  return match ? { status: match[1], body: match[2] } : null;
}

/** The document as it is now, from a 409 refusing a stale save. */
export function conflictDocument(error: unknown): DocumentWithContent | null {
  const api = apiError(error);
  if (api?.status !== "409") return null;
  try {
    const current = (JSON.parse(api.body) as { current?: DocumentWithContent }).current;
    return current && typeof current.id === "string" ? current : null;
  } catch {
    return null;
  }
}

/** Whether a request failed because what it named is not there. */
export function isNotFound(error: unknown): boolean {
  return apiError(error)?.status === "404";
}

/** The server's size message for content over the limit, or null. */
export function contentTooLarge(content: string): string | null {
  const bytes = new TextEncoder().encode(content).length;
  return bytes > MAX_DOCUMENT_BYTES ? tooLargeMessage(bytes) : null;
}

/** The server's words for a document of `bytes` over the limit. */
export function tooLargeMessage(bytes: number): string {
  return `This document is ${formatSize(bytes)}. The limit is 8 MB.`;
}

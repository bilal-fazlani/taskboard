// The document rules the web UI checks before it asks the server, worded
// exactly as the server words them (internal/db/documents.go), plus the `doc`
// query parameter that names the open document.
import type {
  DocumentFormat,
  DocumentMeta,
  DocumentOwnerRef,
  DocumentWithContent,
  ImageDocumentFormat,
  ImagePlace,
  TextDocumentFormat,
} from "../api/client";

/** The query parameter naming the open document by its display name. */
export const DOC_PARAM = "doc";

export const MAX_DOCUMENT_BYTES = 8 * 1024 * 1024;
const MAX_NAME_LENGTH = 200;

// Letters in any script with their combining marks, decimal digits, space,
// underscore and hyphen: Go's unicode.IsLetter/IsMark/IsDigit.
const NAME_CHAR = /^[\p{L}\p{M}\p{Nd} _-]$/u;

const EXTENSIONS: Record<DocumentFormat, string> = {
  markdown: ".md",
  html: ".html",
  png: ".png",
  jpeg: ".jpg",
  gif: ".gif",
  webp: ".webp",
};

/** The extension a format shows with, as the server has it (models.DocumentExtension): a JPEG is ".jpg". */
export function extensionFor(format: DocumentFormat): string {
  return EXTENSIONS[format] ?? ".md";
}

/** Whether a format holds text (markdown or HTML) rather than an image. */
export function isTextFormat(format: DocumentFormat): format is TextDocumentFormat {
  return format === "markdown" || format === "html";
}

/** Whether a format is an image (PNG, JPEG, GIF or WebP). */
export function isImageFormat(format: DocumentFormat): format is ImageDocumentFormat {
  return !isTextFormat(format);
}

/** An owner's images, in the order its documents are listed. */
export function imagesOf<T extends Pick<DocumentMeta, "format">>(docs: readonly T[]): T[] {
  return docs.filter((d) => isImageFormat(d.format));
}

/** An image's size in pixels as shown, "1280×800", or "" when it is not known. */
export function imageDimensions(doc: Pick<DocumentMeta, "width" | "height">): string {
  return doc.width && doc.height ? `${doc.width}×${doc.height}` : "";
}

/** "2 of 3 images", or "" when the image is not in the list. */
export function imagePosition(doc: Pick<DocumentMeta, "id">, images: readonly Pick<DocumentMeta, "id">[]): string {
  const index = images.findIndex((d) => d.id === doc.id);
  if (index < 0) return "";
  return `${index + 1} of ${images.length} ${images.length === 1 ? "image" : "images"}`;
}

/** The image `offset` places from `doc` among the owner's (-1 before, 1 after), or null past either end. */
export function adjacentImage<T extends Pick<DocumentMeta, "id">>(doc: Pick<DocumentMeta, "id">, images: readonly T[], offset: number): T | null {
  const index = images.findIndex((d) => d.id === doc.id);
  if (index < 0) return null;
  return images[index + offset] ?? null;
}

/**
 * The key the document window is mounted under. Every image shares one, so
 * stepping from image to image with ← and → keeps the window (and its focus)
 * rather than opening a new one.
 */
export function documentWindowKey(doc: Pick<DocumentMeta, "id" | "format">): string {
  return isImageFormat(doc.format) ? "images" : doc.id;
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
  return "ticketId" in owner ? `ticket:${owner.ticketId}` : `epic:${owner.epicId}`;
}

const FORMAT_BY_EXTENSION: Record<string, DocumentFormat> = {
  ".md": "markdown",
  ".html": "html",
  ".htm": "html",
  ".png": "png",
  ".jpg": "jpeg",
  ".jpeg": "jpeg",
  ".gif": "gif",
  ".webp": "webp",
};
// The server's words (msgDocExtension and msgDocSVG in internal/db/documents.go).
const EXTENSION_MESSAGE = "Only .md, .html, .htm, .png, .jpg, .jpeg, .gif and .webp files can be attached.";
const SVG_MESSAGE = "SVG images can't be attached. Use PNG, JPEG, GIF or WebP.";

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
  const extension = dot >= 0 ? base.slice(dot).toLowerCase() : "";
  const format = FORMAT_BY_EXTENSION[extension];
  if (!format) return { error: extension === ".svg" ? SVG_MESSAGE : EXTENSION_MESSAGE };
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

/** The server's words for a document of `bytes` over the limit, or an image's (ImageTooLargeMessage). */
export function tooLargeMessage(bytes: number, format?: DocumentFormat): string {
  const what = format && isImageFormat(format) ? "image" : "document";
  return `This ${what} is ${formatSize(bytes)}. The limit is 8 MB.`;
}

/** How a place that uses an image is named: "the description" or "Plan.md". */
export function placeLabel(place: ImagePlace): string {
  return place.kind === "description" ? "the description" : place.name;
}

/**
 * What deleting an image in use does, for its delete confirm: "It's used in
 * 2 places: the description and Plan.md. They'll show a missing image."
 * Null when nothing uses it.
 */
export function imageUsageWarning(places: readonly ImagePlace[]): string | null {
  if (places.length === 0) return null;
  const labels = places.map(placeLabel);
  const list = labels.length === 1 ? labels[0] : `${labels.slice(0, -1).join(", ")} and ${labels[labels.length - 1]}`;
  const one = places.length === 1;
  return `It's used in ${places.length} ${one ? "place" : "places"}: ${list}. ${one ? "It'll" : "They'll"} show a missing image.`;
}

// Images pasted or dropped into owner text (a ticket's description, a
// markdown document): which files count, what they are called, the reference
// that goes into the text, and where that reference is after later edits.
import type { DocumentFormat, DocumentMeta } from "../api/client";
import { displayName, documentNameError, isImageFormat, MAX_DOCUMENT_BYTES, nameFromFilename, tooLargeMessage } from "./documents";

/** The name a pasted image takes before a number is added to it. */
export const PASTED_IMAGE_NAME = "Pasted image";

// A dropped file counts as an image by its type or its extension; SVG counts
// too, so it is refused with a reason rather than ignored.
const IMAGE_EXTENSION = /\.(png|jpe?g|gif|webp|svg)$/i;

/** Whether a dropped or pasted file is meant as an image (anything else is left alone). */
export function isImageFile(file: Pick<File, "name" | "type">): boolean {
  return file.type.startsWith("image/") || IMAGE_EXTENSION.test(file.name);
}

// The extension a clipboard image's type is uploaded with: its format's own
// (a JPEG shows as .jpg), or the subtype, which the name rules then refuse.
const EXTENSION_BY_TYPE: Record<string, string> = {
  "image/png": ".png",
  "image/jpeg": ".jpg",
  "image/gif": ".gif",
  "image/webp": ".webp",
  "image/svg+xml": ".svg",
};

function extensionForType(type: string): string {
  return EXTENSION_BY_TYPE[type.toLowerCase()] ?? `.${type.split("/")[1] || "unknown"}`;
}

/**
 * The first free "Pasted image", "Pasted image 2", "Pasted image 3"… among
 * `taken` (names without extensions), ignoring case. A name `text` already
 * mentions with an extension ("Pasted image 2.png", say a reference left by
 * an upload that failed) is not free either, so a new image never answers
 * an old reference.
 */
export function pastedImageName(taken: Iterable<string>, text = ""): string {
  const lower = new Set([...taken].map((n) => n.toLowerCase()));
  const mentioned = text.toLowerCase();
  const free = (name: string) => !lower.has(name.toLowerCase()) && !mentioned.includes(`${name.toLowerCase()}.`);
  if (free(PASTED_IMAGE_NAME)) return PASTED_IMAGE_NAME;
  for (let n = 2; ; n++) {
    const name = `${PASTED_IMAGE_NAME} ${n}`;
    if (free(name)) return name;
  }
}

/** A file ready to upload: the name it goes up with, and what the text calls it. */
export type PreparedImage = { file: File; name: string; format: DocumentFormat; shown: string };

/**
 * The checks every upload runs before it is sent, as the Upload button does:
 * the type, from the file name's extension (SVG and anything but PNG, JPEG,
 * GIF and WebP are refused), 8 MB, and a name the owner does not have yet.
 * `taken` are the owner's documents and the names claimed by uploads under
 * way. A paste is named `name` (its file name means nothing); a dropped file
 * keeps its own, converted by the document name rules.
 */
export function prepareImage(
  file: File,
  taken: readonly Pick<DocumentMeta, "id" | "name" | "format">[],
  ownerNoun: string,
  name?: string,
): PreparedImage | { error: string; shown: string } {
  const filename = name !== undefined ? name + extensionForType(file.type) : file.name;
  const parsed = nameFromFilename(filename);
  if ("error" in parsed) return { error: parsed.error, shown: filename };
  const shown = displayName(parsed);
  if (!isImageFormat(parsed.format)) return { error: "Only PNG, JPEG, GIF and WebP images can be added here.", shown };
  if (file.size > MAX_DOCUMENT_BYTES) return { error: tooLargeMessage(file.size, parsed.format), shown };
  const nameError = documentNameError(parsed.name, taken, undefined, ownerNoun);
  if (nameError) return { error: nameError, shown };
  // The server names the image from the file name it is sent with.
  const upload = new File([file], shown, { type: file.type });
  return { file: upload, name: parsed.name, format: parsed.format, shown };
}

/**
 * The reference that shows an image by its display name, as the ticket asks:
 * ![](Login screen.png). A name with a space and an underscore could read as
 * emphasis in that bare form (_x_), so it goes angle-bracketed instead:
 * ![](<my _draft_ shot.png>). Both are forms a rename keeps in step.
 */
export function imageReference(shown: string): string {
  return shown.includes(" ") && shown.includes("_") ? `![](<${shown}>)` : `![](${shown})`;
}

/**
 * Where text at `start` (of `length` characters) in `prev` is in `next`, or
 * null when the edit between them touched it. The edit is taken as one
 * change between the longest common prefix and suffix; when that reading is
 * ambiguous (the same characters typed right beside the text), the text is
 * found where it would be either way. An edit that changed several places at
 * once (a reload, say) still finds the text if it is there exactly once.
 */
export function followRange(prev: string, next: string, start: number, text: string): number | null {
  if (prev === next) return start;
  const max = Math.min(prev.length, next.length);
  let p = 0;
  while (p < max && prev[p] === next[p]) p++;
  let s = 0;
  while (s < max - p && prev[prev.length - 1 - s] === next[next.length - 1 - s]) s++;
  const delta = next.length - prev.length;
  const end = start + text.length;
  if (end <= p) return start;
  if (start >= prev.length - s) return start + delta;
  if (next.slice(start, end) === text) return start;
  if (next.slice(start + delta, end + delta) === text) return start + delta;
  const at = next.indexOf(text);
  return at >= 0 && next.indexOf(text, at + 1) < 0 ? at : null;
}

/** The files an image paste would upload: none when the clipboard carries text, which pastes as ever. */
export function pastedImageFiles(data: Pick<DataTransfer, "files" | "types"> | null): File[] {
  if (!data) return [];
  if (Array.from(data.types ?? []).includes("text/plain")) return [];
  return Array.from(data.files ?? []).filter(isImageFile);
}

/** Whether a drag carries files (which the text never takes as they are). */
export function draggingFiles(data: Pick<DataTransfer, "types"> | null): boolean {
  return !!data && Array.from(data.types ?? []).includes("Files");
}

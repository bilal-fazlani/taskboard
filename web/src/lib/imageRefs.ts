// Images inside text: a ticket's description and markdown documents, or an
// epic's markdown documents, show that owner's images by name. The forms a
// reference may take (renames rewrite exactly these):
//
//   ![alt](Login screen.png)       plain, spaces and all (remarkSpacedImageRefs)
//   ![alt](<Login screen.png>)     angle-bracketed
//   ![alt](Login%20screen.png)     percent-encoded
//
// The name is the image's display name with its extension, ignoring case; a
// JPEG shows as .jpg and answers to .jpeg too (as the server's lookups do).
// Absolute http(s) URLs are shown as they are. Anything else, including a
// path on this server or another owner's image name, is a missing image.
//
// An HTML document refers to them by relative URL, <img src="Login
// screen.png"> (or Login%20screen.png, ./Login screen.png, or any other
// relative URL that lands beside the page), which the browser resolves
// against the raw page's URL; the server's referenced-image route
// (internal/server/document_image_refs.go) answers it.
import type { DocumentMeta } from "../api/client";
import { displayName } from "./documents";

type ImageDoc = Pick<DocumentMeta, "id" | "name" | "format" | "revision" | "width" | "height">;

export type ImageTarget =
  | { kind: "absolute"; src: string }
  | { kind: "owner"; doc: ImageDoc; src: string }
  | { kind: "missing"; name: string };

const IMAGE_FORMATS = new Set(["png", "jpeg", "gif", "webp"]);

/** The image route for an owner's image; the revision makes a replaced image a new URL. */
export function ownerImageUrl(doc: Pick<DocumentMeta, "id" | "revision">): string {
  return `/api/documents/${encodeURIComponent(doc.id)}/image?rev=${doc.revision}`;
}

function decoded(src: string): string {
  try {
    return decodeURIComponent(src);
  } catch {
    return src;
  }
}

/**
 * The owner image an image name refers to: its display name ignoring case,
 * or, for a JPEG, the name with .jpeg. Only images match.
 */
export function findOwnerImage<T extends ImageDoc>(documents: readonly T[], ref: string): T | undefined {
  const wanted = ref.trim().toLowerCase();
  if (!wanted) return undefined;
  return documents.find(
    (d) =>
      IMAGE_FORMATS.has(d.format) &&
      (displayName(d).toLowerCase() === wanted || (d.format === "jpeg" && `${d.name}.jpeg`.toLowerCase() === wanted)),
  );
}

/** What an image's src (as react-markdown hands it over) shows. */
export function resolveImageRef(src: string | undefined, documents: readonly ImageDoc[]): ImageTarget {
  const raw = (src ?? "").trim();
  if (/^https?:\/\//i.test(raw) || raw.startsWith("//")) return { kind: "absolute", src: raw };
  const name = decoded(raw).trim();
  const doc = findOwnerImage(documents, name);
  if (doc) return { kind: "owner", doc, src: ownerImageUrl(doc) };
  return { kind: "missing", name };
}

// A minimal mdast node: only what the plugin below reads and writes.
interface MdNode {
  type: string;
  value?: string;
  url?: string;
  alt?: string;
  title?: string | null;
  children?: MdNode[];
}

// An image reference CommonMark leaves as text because its name has spaces
// in it, as agents naturally write them: ![alt](Login screen.png). Only
// names ending in an image extension count, so ordinary text with brackets
// is left alone.
const SPACED_IMAGE_REF = /!\[([^\]\n]*)\]\(\s*([^()<>\n]*?\S\.(?:png|jpe?g|gif|webp))\s*\)/gi;

function splitText(value: string): MdNode[] | null {
  const out: MdNode[] = [];
  let last = 0;
  for (const match of value.matchAll(SPACED_IMAGE_REF)) {
    const at = match.index ?? 0;
    if (at > last) out.push({ type: "text", value: value.slice(last, at) });
    out.push({ type: "image", url: match[2].trim(), alt: match[1], title: null });
    last = at + match[0].length;
  }
  if (out.length === 0) return null;
  if (last < value.length) out.push({ type: "text", value: value.slice(last) });
  return out;
}

function rewrite(node: MdNode): void {
  if (!node.children) return;
  const next: MdNode[] = [];
  for (const child of node.children) {
    const split = child.type === "text" && child.value ? splitText(child.value) : null;
    if (split) next.push(...split);
    else {
      rewrite(child);
      next.push(child);
    }
  }
  node.children = next;
}

/** A remark plugin turning plain references with spaces into images. Code is never touched: its text is not a text node. */
export function remarkSpacedImageRefs() {
  return (tree: unknown) => rewrite(tree as MdNode);
}

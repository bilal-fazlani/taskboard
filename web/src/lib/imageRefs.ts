// Images inside text: a ticket's description and markdown documents, or an
// epic's markdown documents, show that owner's images by name; so do the
// owner's HTML documents. The name is the image's display name with its
// extension, ignoring case; a JPEG shows as .jpg and answers to .jpeg too (as
// the server's lookups do). Renames keep exactly the forms below in step, and
// nothing else (internal/models/image_ref.go carries the same list).
//
// Markdown:
//   ![alt](Login screen.png)             bare, spaces and all (remarkSpacedImageRefs);
//                                        no title in this form
//   ![alt](<Login screen.png>)           angle-bracketed, with or without a title:
//   ![alt](<Login screen.png> "Title")
//   ![alt](Login%20screen.png)           percent-encoded (any %XX escape of the
//   ![alt](Login%20screen.png "Title")   name's characters), with or without a title
//   ![alt][r]  ![r][]  ![r]              reference style, whose definition gives the
//   [r]: <Login screen.png>              name angle-bracketed or percent-encoded
//   [r]: Login%20screen.png "Title"      (a name without spaces may also be bare),
//                                        with or without a title
// Absolute http(s) URLs are shown as they are. Anything else, including
// ./Login screen.png, a path on this server or another owner's image name,
// is a missing image.
//
// HTML (the browser resolves these against the raw page's URL, and the
// server's referenced-image route, internal/server/document_image_refs.go,
// answers them):
//   src="…"          on any element (img, source, …)
//   srcset="… 2x"    each URL in it (spaces must be %20 there)
//   url(…)           in a style attribute or a <style> element, quoted or not
// where the URL is the name bare (Login screen.png), with ./ in front
// (./Login screen.png), or percent-encoded (Login%20screen.png). Other forms
// that happen to reach the route, such as ../<id>/Login screen.png,
// /api/documents/<id>/Login screen.png, an absolute URL, an href, or a URL a
// script builds, are not kept in step.
import { api, type DocumentMeta } from "../api/client";
import { displayName } from "./documents";

type ImageDoc = Pick<DocumentMeta, "id" | "name" | "format" | "revision" | "width" | "height">;

export type ImageTarget =
  | { kind: "absolute"; src: string }
  | { kind: "owner"; doc: ImageDoc; src: string }
  | { kind: "missing"; name: string };

const IMAGE_FORMATS = new Set(["png", "jpeg", "gif", "webp"]);

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
  if (doc) return { kind: "owner", doc, src: api.documents.imageUrl(doc.id, doc.revision) };
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
  position?: { start: { offset?: number }; end: { offset?: number } };
}

// An image reference CommonMark leaves as text because its name has spaces
// in it, as agents naturally write them: ![alt](Login screen.png). Only
// names ending in an image extension count, so ordinary text with brackets
// is left alone.
const SPACED_IMAGE_REF = /!\[([^\]\n]*)\]\(\s*([^()<>\n]*?\S\.(?:png|jpe?g|gif|webp))\s*\)/gi;

// Whether the n-th copy of `text` in the markdown a text node came from is
// escaped (\![alt](…)), or cannot be found as written there (!\[alt](…)):
// either way the author did not write an image. Without the source, nothing
// is escaped.
function escapedInSource(source: string | undefined, node: MdNode, text: string, n: number): boolean {
  const start = node.position?.start.offset;
  const end = node.position?.end.offset;
  if (source === undefined || start === undefined || end === undefined) return false;
  const written = source.slice(start, end);
  let at = -1;
  for (let i = 0; i <= n; i++) {
    at = written.indexOf(text, at + 1);
    if (at < 0) return true;
  }
  let slashes = 0;
  for (let i = at - 1; i >= 0 && written[i] === "\\"; i--) slashes++;
  return slashes % 2 === 1;
}

function splitText(node: MdNode, source: string | undefined): MdNode[] | null {
  const value = node.value ?? "";
  const out: MdNode[] = [];
  const seen = new Map<string, number>();
  let last = 0;
  for (const match of value.matchAll(SPACED_IMAGE_REF)) {
    const n = seen.get(match[0]) ?? 0;
    seen.set(match[0], n + 1);
    if (escapedInSource(source, node, match[0], n)) continue;
    const at = match.index ?? 0;
    if (at > last) out.push({ type: "text", value: value.slice(last, at) });
    out.push({ type: "image", url: match[2].trim(), alt: match[1], title: null });
    last = at + match[0].length;
  }
  if (out.length === 0) return null;
  if (last < value.length) out.push({ type: "text", value: value.slice(last) });
  return out;
}

function rewrite(node: MdNode, source: string | undefined): void {
  if (!node.children) return;
  const next: MdNode[] = [];
  for (const child of node.children) {
    const split = child.type === "text" && child.value ? splitText(child, source) : null;
    if (split) next.push(...split);
    else {
      rewrite(child, source);
      next.push(child);
    }
  }
  node.children = next;
}

/**
 * A remark plugin turning bare references with spaces into images. Code is
 * never touched (its text is not a text node), nor is a reference the author
 * escaped.
 */
export function remarkSpacedImageRefs() {
  return (tree: unknown, file?: { value?: unknown }) =>
    rewrite(tree as MdNode, typeof file?.value === "string" ? file.value : undefined);
}

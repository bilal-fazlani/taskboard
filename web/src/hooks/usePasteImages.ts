import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { api, type DocumentFormat, type DocumentMeta, type DocumentOwnerRef } from "../api/client";
import { serverMessage } from "../lib/epics";
import {
  draggingFiles,
  followRange,
  imageReference,
  isImageFile,
  pastedImageFiles,
  pastedImageName,
  prepareImage,
  type PreparedImage,
} from "../lib/pasteImages";

/** An image on its way up, by the name the text calls it. */
export type ImageUpload = { id: number; shown: string };
/** Why an image pasted or dropped was not added. */
export type ImageUploadProblem = { id: number; text: string };

type Claim = Pick<DocumentMeta, "id" | "name" | "format"> & { docId?: string };
type Tracked = { start: number | null; text: string };

// The server's refusal of a name the owner already has (msgDocNameTaken).
const NAME_TAKEN = /already has a document called/;
// How often a paste looks for another free name when the server says the one
// it picked was taken meanwhile (an agent's, not yet in the list).
const RENAME_ATTEMPTS = 5;

function problemText(shown: string, reason: string): string {
  return `${shown} wasn't uploaded: ${reason}`;
}

// Whether the textarea took the edit through the browser's own editing,
// which keeps it on the undo stack. Not every browser (nor jsdom) does.
function editNatively(textarea: HTMLTextAreaElement, text: string, expected: string): boolean {
  try {
    const ok = text === "" ? document.execCommand("delete") : document.execCommand("insertText", false, text);
    return ok && textarea.value === expected;
  } catch {
    return false;
  }
}

// A position in the text before an edit replacing [start, end) with `length`
// characters, moved to where it is after it.
function mapPosition(pos: number, start: number, end: number, length: number): number {
  if (pos <= start) return pos;
  if (pos >= end) return pos - (end - start) + length;
  return start + length;
}

/**
 * Pasting an image from the clipboard, or dropping image files, into a
 * textarea holding an owner's markdown (a ticket's description, a markdown
 * document). Each image is checked (type, 8 MB, a free name) and its
 * reference, ![](Name.png), goes in at the cursor at once, as typing would:
 * through `onChange`, and through the browser's own editing where it can, so
 * Undo takes it back. The upload runs underneath; `uploads` lists those under
 * way. A paste is named "Pasted image", "Pasted image 2"…, the first name the
 * owner does not have, ignoring case; a dropped file keeps its file name,
 * converted by the name rules. Several at once each get their own reference,
 * one per line, in order.
 *
 * An upload refused or failed takes out exactly the reference it put in,
 * wherever later typing moved it, and says why in `problems`; a reference
 * the user has since edited is left as they made it. Nothing else in the text
 * is touched. While the text is not being edited (`live` false: a document
 * saved or cancelled before its upload ended) nothing is taken out; the
 * reason still shows, saying when the saved text (`savedText`) still refers
 * to the image. A failed paste's name stays taken while any text may still
 * refer to it, so a later paste never takes over a stale reference. Text pastes, text drags and dropped files that are not images
 * behave as before, except that a file is never dropped into the text as it
 * is (the browser would open it and leave the page).
 *
 * Without an owner (a ticket not created yet) nothing is uploaded, and
 * `problems` says images can be added once it exists.
 */
export function usePasteImages({
  owner,
  documents,
  ownerNoun = "ticket",
  value,
  onChange,
  textareaRef,
  onUploaded,
  live = true,
  savedText,
}: {
  owner: DocumentOwnerRef | null;
  /** The owner's documents, null while they load. */
  documents: readonly DocumentMeta[] | null;
  ownerNoun?: string;
  value: string;
  /** The text's setter, as the textarea's own onChange calls it. */
  onChange: (next: string) => void;
  textareaRef: React.RefObject<HTMLTextAreaElement | null>;
  /** An image was added: the owner reloads its documents. */
  onUploaded?: (doc: DocumentMeta) => void;
  /** Whether `value` is the text being edited now (false once a document's editing has ended). */
  live?: boolean;
  /** The owner's text as last saved, which a failed image's reference may already be in. */
  savedText?: () => string | null;
}) {
  const [uploads, setUploads] = useState<ImageUpload[]>([]);
  const [problems, setProblems] = useState<ImageUploadProblem[]>([]);
  // The last image added, for screen readers.
  const [added, setAdded] = useState<string | null>(null);

  // The text the tracked references' positions are in, and those positions.
  const seen = useRef(value);
  const tracked = useRef(new Map<number, Tracked>());
  // Names taken by uploads under way, or added and not yet in the list, and
  // names the server said were taken.
  const claims = useRef(new Map<number, Claim>());
  const takenByServer = useRef(new Set<string>());
  const selectionAfter = useRef<[number, number] | null>(null);
  const seq = useRef(0);
  const mounted = useRef(true);

  const latest = useRef({ owner, documents, ownerNoun, onChange, onUploaded, live, savedText });
  useEffect(() => {
    latest.current = { owner, documents, ownerNoun, onChange, onUploaded, live, savedText };
  });
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  // Moves every tracked reference from the text seen last to `next`.
  const advance = useCallback((next: string) => {
    const prev = seen.current;
    if (prev === next) return;
    for (const ref of tracked.current.values()) {
      if (ref.start !== null) ref.start = followRange(prev, next, ref.start, ref.text);
    }
    seen.current = next;
  }, []);

  // Whatever changed the text (typing, a reload, our own edits), the
  // references follow it; a caret an edit of ours moved is put back.
  useLayoutEffect(() => {
    advance(value);
    const textarea = textareaRef.current;
    const selection = selectionAfter.current;
    selectionAfter.current = null;
    if (selection && textarea && textarea.value === value) textarea.setSelectionRange(selection[0], selection[1]);
  }, [value, advance, textareaRef]);

  // A claim for an added image lasts until the list carries the image.
  useEffect(() => {
    if (!documents) return;
    for (const [id, claim] of claims.current) {
      if (claim.docId && documents.some((d) => d.id === claim.docId)) claims.current.delete(id);
    }
  }, [documents]);

  const takenNames = (): Pick<DocumentMeta, "id" | "name" | "format">[] => [
    ...(latest.current.documents ?? []),
    ...claims.current.values(),
    ...[...takenByServer.current].map((name) => ({ id: "", name, format: "png" as DocumentFormat })),
  ];

  // Replaces `text` at `start` with `replacement`, if it is still there, as
  // an edit of the user's own would: natively when the textarea has focus
  // (keeping the caret where it was), through onChange otherwise.
  const replaceAt = (start: number, text: string, replacement: string): boolean => {
    const current = seen.current;
    const end = start + text.length;
    if (current.slice(start, end) !== text) return false;
    const next = current.slice(0, start) + replacement + current.slice(end);
    const textarea = textareaRef.current;
    if (textarea && document.activeElement === textarea && textarea.value === current) {
      const from = textarea.selectionStart;
      const to = textarea.selectionEnd;
      const caret: [number, number] = [
        mapPosition(from, start, end, replacement.length),
        mapPosition(to, start, end, replacement.length),
      ];
      textarea.setSelectionRange(start, end);
      if (editNatively(textarea, replacement, next)) {
        textarea.setSelectionRange(caret[0], caret[1]);
        advance(next);
        return true;
      }
      textarea.setSelectionRange(from, to);
      selectionAfter.current = caret;
    }
    advance(next);
    latest.current.onChange(next);
    return true;
  };

  const settle = (id: number) => {
    tracked.current.delete(id);
    if (mounted.current) setUploads((list) => list.filter((u) => u.id !== id));
  };

  const fail = (id: number, shown: string, reason: string) => {
    const ref = tracked.current.get(id);
    tracked.current.delete(id);
    if (!mounted.current) return;
    const { live, savedText } = latest.current;
    const removed = live && !!ref && ref.start !== null && replaceAt(ref.start, ref.text, "");
    const inSaved = !!ref && (savedText?.() ?? "").includes(ref.text);
    // The name is freed only when no text can still refer to it: the editor's
    // (a reference the user edited) or the saved copy.
    if (!inSaved && (removed || !live)) claims.current.delete(id);
    settle(id);
    const note = inSaved && !removed ? " The saved text still refers to it." : "";
    setProblems((list) => [...list, { id, text: problemText(shown, reason) + note }]);
  };

  const upload = async (id: number, owner: DocumentOwnerRef, image: PreparedImage, pasted: boolean, attempt = 0) => {
    try {
      const doc = await api.documents.createImage(owner, image.file);
      const claim = claims.current.get(id);
      if (claim) claim.docId = doc.id;
      settle(id);
      if (mounted.current) setAdded(image.shown);
      latest.current.onUploaded?.(doc);
    } catch (err) {
      const reason = serverMessage(err, "The image was not uploaded.");
      const ref = tracked.current.get(id);
      if (pasted && NAME_TAKEN.test(reason) && attempt < RENAME_ATTEMPTS && ref && ref.start !== null && mounted.current && latest.current.live) {
        // Someone took the name meanwhile: the paste takes the next free one,
        // and its reference says so.
        takenByServer.current.add(image.name);
        claims.current.delete(id);
        const renamed = prepareImage(image.file, takenNames(), latest.current.ownerNoun, pastedImageName(takenNames().map((d) => d.name)));
        if (!("error" in renamed)) {
          const text = imageReference(renamed.shown);
          const start = ref.start;
          tracked.current.delete(id);
          if (replaceAt(start, ref.text, text)) {
            tracked.current.set(id, { start, text });
            claims.current.set(id, { id: `claim-${id}`, name: renamed.name, format: renamed.format });
            setUploads((list) => list.map((u) => (u.id === id ? { id, shown: renamed.shown } : u)));
            return upload(id, owner, renamed, pasted, attempt + 1);
          }
          tracked.current.set(id, ref);
        }
      }
      fail(id, image.shown, reason);
    }
  };

  const add = (files: File[], pasted: boolean) => {
    const { owner, ownerNoun } = latest.current;
    if (!owner) {
      setProblems([{ id: ++seq.current, text: `Images can be pasted or dropped once the ${ownerNoun} is created.` }]);
      return;
    }
    const items: { id: number; image: PreparedImage; text: string }[] = [];
    const refused: ImageUploadProblem[] = [];
    for (const file of files) {
      const taken = takenNames();
      const name = pasted ? pastedImageName(taken.map((d) => d.name)) : undefined;
      const image = prepareImage(file, taken, ownerNoun, name);
      const id = ++seq.current;
      if ("error" in image) {
        refused.push({ id, text: problemText(image.shown, image.error) });
        continue;
      }
      claims.current.set(id, { id: `claim-${id}`, name: image.name, format: image.format });
      items.push({ id, image, text: imageReference(image.shown) });
    }
    setProblems(refused);
    if (items.length === 0) return;

    const joined = items.map((i) => i.text).join("\n");
    const textarea = textareaRef.current;
    const current = seen.current;
    const from = textarea && textarea.value === current ? textarea.selectionStart : current.length;
    const to = textarea && textarea.value === current ? textarea.selectionEnd : current.length;
    const next = current.slice(0, from) + joined + current.slice(to);
    textarea?.focus();
    if (textarea && textarea.value === current) textarea.setSelectionRange(from, to);
    if (textarea && editNatively(textarea, joined, next)) advance(next);
    else {
      advance(next);
      selectionAfter.current = [from + joined.length, from + joined.length];
      latest.current.onChange(next);
    }
    let at = from;
    for (const item of items) {
      tracked.current.set(item.id, { start: at, text: item.text });
      at += item.text.length + 1;
    }
    setUploads((list) => [...list, ...items.map((i) => ({ id: i.id, shown: i.image.shown }))]);
    for (const item of items) void upload(item.id, owner, item.image, pasted);
  };

  const onPaste = (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
    const files = pastedImageFiles(e.clipboardData);
    if (files.length === 0) return;
    e.preventDefault();
    add(files, true);
  };

  const onDragOver = (e: React.DragEvent<HTMLTextAreaElement>) => {
    if (!draggingFiles(e.dataTransfer)) return;
    e.preventDefault();
    const images = Array.from(e.dataTransfer.items ?? []).some((i) => i.kind === "file" && i.type.startsWith("image/"));
    e.dataTransfer.dropEffect = images ? "copy" : "none";
  };

  const onDrop = (e: React.DragEvent<HTMLTextAreaElement>) => {
    if (!draggingFiles(e.dataTransfer)) return;
    e.preventDefault();
    const files = Array.from(e.dataTransfer.files ?? []).filter(isImageFile);
    if (files.length > 0) add(files, false);
  };

  const dismissProblems = useCallback(() => setProblems([]), []);

  return { textareaProps: { onPaste, onDragOver, onDrop }, uploads, problems, added, dismissProblems };
}

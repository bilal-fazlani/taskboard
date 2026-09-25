import { useCallback, useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import type { DocumentMeta } from "../api/client";
import { DOC_PARAM, findDocument, withDoc, withoutDoc } from "../lib/documents";
import { latestSearchParams } from "../lib/latestSearch";
import { useOverlayHistory } from "./useOverlayHistory";

export interface DocParamState {
  /** The open document, or null. */
  selected: DocumentMeta | null;
  /** The modal holds unsaved edits, as it last reported. */
  dirty: boolean;
  /**
   * The open document left the list while it held unsaved edits: it was
   * deleted. It stays on screen so the modal can offer to save it anew.
   */
  deleted: boolean;
  /**
   * The URL stopped naming the open document (Back) while it holds unsaved
   * edits. It stays on screen so the modal can ask before it goes; the
   * modal answers with close() or cancelClose().
   */
  closeRequested: boolean;
  /** Why a document the URL named is not open, until dismissed. */
  notice: string | null;
  dismissNotice: () => void;
  /** Open a document as a new history entry. */
  open: (doc: DocumentMeta) => void;
  /**
   * Open a document just created here as a new history entry, once the list
   * has it: until then the URL could only name something missing.
   */
  openWhenListed: (doc: DocumentMeta) => void;
  /**
   * Close it: one Back, or a replace when it came from a link. After a Back
   * the URL is already where it asked to go, and is left there.
   */
  close: () => void;
  /** Keep a document open after a Back, which pushes its parameter back. */
  cancelClose: () => void;
  /** Point the URL at a document's new name after a rename from the UI. */
  renamed: (doc: DocumentMeta) => void;
  /**
   * Show another document in place of the open one (the image viewer's ←
   * and →). The entry is replaced, not pushed, so one × or Back still
   * closes it.
   */
  step: (doc: DocumentMeta) => void;
  /**
   * "Save as a new document" made this copy of the deleted document. The
   * deleted one stays held until the list has the copy, which the URL then
   * names.
   */
  recreated: (doc: DocumentMeta) => void;
  /** Told by the modal whether it holds unsaved edits. */
  onDirtyChange: (dirty: boolean) => void;
}

/**
 * The open document, named in the URL by its display name (`doc=Plan.md`),
 * over the ticket editor. Nothing is decided until the documents have
 * loaded. Once one is open it is followed by id, so a rename elsewhere moves
 * the URL to the new name instead of closing it; one that leaves the list
 * was deleted, and closes with a notice. A name that never matched (a bad
 * link, a wrong extension) gets its own notice.
 *
 * Unsaved edits are never dropped without asking, as with useTicketParam: a
 * document the modal reports dirty is held on screen when Back drops the
 * parameter (`closeRequested`) or when it leaves the list (`deleted`, with
 * the parameter left in place and no notice), until the modal answers.
 */
export function useDocParam(documents: readonly DocumentMeta[] | null, ownerNoun = "ticket"): DocParamState {
  const [params] = useSearchParams();
  const overlays = useOverlayHistory();
  const ref = params.get(DOC_PARAM) ?? "";
  const found = documents && ref ? (findDocument(documents, ref) ?? null) : null;

  // Whether the modal holds unsaved edits, as it reports them. State, not a
  // ref: the render below decides on it.
  const [dirty, setDirty] = useState(false);

  const [shownId, setShownId] = useState<string | null>(null);
  if (found && shownId !== found.id) setShownId(found.id);
  if (!ref && shownId !== null) setShownId(null);

  // The document the URL names, or, once its name has changed, the one it
  // named before, found by id.
  const byId = !found && ref && shownId && documents ? (documents.find((d) => d.id === shownId) ?? null) : null;
  const current = found ?? byId;

  // A rename made here, until the list carries the new name. The URL already
  // names it, so the list's old name must neither show nor pull it back.
  const [renamedTo, setRenamedTo] = useState<DocumentMeta | null>(null);
  const stale = renamedTo !== null && current !== null && current.id === renamedTo.id && current.name !== renamedTo.name;
  if (renamedTo && (!ref || (current && !stale))) setRenamedTo(null);

  const shown = stale && current && renamedTo ? { ...current, name: renamedTo.name } : current;
  const followed = byId && !stale ? byId : null;

  // The document last shown, kept while it holds unsaved edits and the URL
  // has dropped it (Back) or the list has (deleted). Let go only once the
  // modal reports nothing unsaved.
  const [held, setHeld] = useState<DocumentMeta | null>(null);
  if (current && held !== current) setHeld(current);
  else if (!current && held && !dirty) setHeld(null);
  const holding = !current && dirty && held !== null;
  const selected = shown ?? (holding ? held : null);
  const deleted = holding && documents !== null && !documents.some((d) => d.id === held.id);
  const closeRequested = holding && !ref;

  // A document created here that the URL should name once the list has it:
  // opened as a new entry (New), or put in place of the deleted one it
  // copies ("Save as a new document"). Done once the URL names it.
  const [awaiting, setAwaiting] = useState<{ doc: DocumentMeta; replace: boolean } | null>(null);
  const listed = awaiting && documents ? (documents.find((d) => d.id === awaiting.doc.id) ?? null) : null;
  if (awaiting && found?.id === awaiting.doc.id) setAwaiting(null);

  useEffect(() => {
    if (!awaiting || !listed) return;
    const latest = latestSearchParams(params);
    if (findDocument([listed], latest.get(DOC_PARAM) ?? "")) return;
    const next = withDoc(latest, listed);
    if (awaiting.replace) overlays.replace(next);
    else overlays.push(next);
  }, [awaiting, listed, params, overlays]);

  // The parameter close() is taking away, so the render before the URL
  // changes does not report it missing.
  const [closing, setClosing] = useState<string | null>(null);
  if (!ref && closing !== null) setClosing(null);

  const [notice, setNotice] = useState<string | null>(null);

  useEffect(() => {
    if (followed) overlays.replace(withDoc(latestSearchParams(params), followed));
  }, [followed, params, overlays]);

  // A document the URL names that is not there: one that was open was
  // deleted, anything else never matched. The notice is set once per value
  // of the parameter, during render as the other derived state here is, and
  // the parameter is dropped. shownId is left alone: it clears itself once
  // the URL drops the parameter. A deleted document with unsaved edits is
  // held instead, and one being closed is on its way out.
  const missing = ref !== "" && documents !== null && current === null && !dirty && closing !== ref;
  const [reported, setReported] = useState<string | null>(null);
  if (missing && reported !== ref) {
    setReported(ref);
    setNotice(shownId ? `${ref} was deleted.` : `Couldn't find ${ref} on this ${ownerNoun}.`);
  }
  if (!ref && reported !== null) setReported(null);

  useEffect(() => {
    if (missing) overlays.replace(withoutDoc(latestSearchParams(params)));
  }, [missing, params, overlays]);

  const open = useCallback(
    (doc: DocumentMeta) => {
      setNotice(null);
      setDirty(false);
      overlays.push(withDoc(latestSearchParams(params), doc));
    },
    [params, overlays],
  );

  const openWhenListed = useCallback((doc: DocumentMeta) => {
    setNotice(null);
    setDirty(false);
    setAwaiting({ doc, replace: false });
  }, []);

  // The modal keeps reporting its unsaved text until the copy replaces it,
  // so the deleted document stays held, with no notice, in the meantime.
  const recreated = useCallback((doc: DocumentMeta) => setAwaiting({ doc, replace: true }), []);

  const close = useCallback(() => {
    setDirty(false);
    setHeld(null);
    const latest = latestSearchParams(params);
    const now = latest.get(DOC_PARAM);
    // A Back already dropped the parameter: nothing to undo.
    if (!now) return;
    setClosing(now);
    overlays.closeOne(withoutDoc(latest));
  }, [params, overlays]);

  const cancelClose = useCallback(() => {
    // Back popped the document's entry, so push it again rather than
    // replacing what Back landed on: a second Back then asks again.
    if (held) overlays.push(withDoc(latestSearchParams(params), held));
  }, [held, params, overlays]);

  const renamed = useCallback(
    (doc: DocumentMeta) => {
      setDirty(false);
      setShownId(doc.id);
      setRenamedTo(doc);
      overlays.replace(withDoc(latestSearchParams(params), doc));
    },
    [params, overlays],
  );
  const step = useCallback(
    (doc: DocumentMeta) => {
      setNotice(null);
      overlays.replace(withDoc(latestSearchParams(params), doc));
    },
    [params, overlays],
  );
  const dismissNotice = useCallback(() => setNotice(null), []);

  // setDirty is stable, so the modal's report never re-runs on its own.
  return {
    selected,
    dirty,
    deleted,
    closeRequested,
    notice,
    dismissNotice,
    open,
    openWhenListed,
    close,
    cancelClose,
    renamed,
    step,
    recreated,
    onDirtyChange: setDirty,
  };
}

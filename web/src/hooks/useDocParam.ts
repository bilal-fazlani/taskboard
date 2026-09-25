import { useCallback, useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import type { DocumentMeta } from "../api/client";
import { DOC_PARAM, findDocument, withDoc, withoutDoc } from "../lib/documents";
import { latestSearchParams } from "../lib/latestSearch";
import { useOverlayHistory } from "./useOverlayHistory";

export interface DocParamState {
  /** The open document, or null. */
  selected: DocumentMeta | null;
  /** Why a document the URL named is not open, until dismissed. */
  notice: string | null;
  dismissNotice: () => void;
  /** Open a document as a new history entry. */
  open: (doc: DocumentMeta) => void;
  /** Close it: one Back, or a replace when it came from a link. */
  close: () => void;
  /** Point the URL at a document's new name after a rename from the UI. */
  renamed: (doc: DocumentMeta) => void;
}

/**
 * The open document, named in the URL by its display name (`doc=Plan.md`),
 * over the ticket editor. Nothing is decided until the documents have
 * loaded. Once one is open it is followed by id, so a rename elsewhere moves
 * the URL to the new name instead of closing it; one that leaves the list
 * was deleted, and closes with a notice. A name that never matched (a bad
 * link, a wrong extension) gets its own notice.
 */
export function useDocParam(documents: readonly DocumentMeta[] | null, ownerNoun = "ticket"): DocParamState {
  const [params] = useSearchParams();
  const overlays = useOverlayHistory();
  const ref = params.get(DOC_PARAM) ?? "";
  const found = documents && ref ? (findDocument(documents, ref) ?? null) : null;

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

  const selected = stale && current && renamedTo ? { ...current, name: renamedTo.name } : current;
  const followed = byId && !stale ? byId : null;

  const [notice, setNotice] = useState<string | null>(null);

  useEffect(() => {
    if (followed) overlays.replace(withDoc(latestSearchParams(params), followed));
  }, [followed, params, overlays]);

  // A document the URL names that is not there: one that was open was
  // deleted, anything else never matched. The notice is set once per value
  // of the parameter, during render as the other derived state here is, and
  // the parameter is dropped. shownId is left alone: it clears itself once
  // the URL drops the parameter.
  const missing = ref !== "" && documents !== null && current === null;
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
      overlays.push(withDoc(latestSearchParams(params), doc));
    },
    [params, overlays],
  );
  const close = useCallback(() => overlays.closeOne(withoutDoc(latestSearchParams(params))), [params, overlays]);
  const renamed = useCallback(
    (doc: DocumentMeta) => {
      setShownId(doc.id);
      setRenamedTo(doc);
      overlays.replace(withDoc(latestSearchParams(params), doc));
    },
    [params, overlays],
  );
  const dismissNotice = useCallback(() => setNotice(null), []);

  return { selected, notice, dismissNotice, open, close, renamed };
}

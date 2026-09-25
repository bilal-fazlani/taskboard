import { useCallback, useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { EPIC_PARAM, findEpic, withEpic, withEpicName, withoutEpic } from "../lib/epicParam";
import { latestSearchParams } from "../lib/latestSearch";
import { useOverlayHistory } from "./useOverlayHistory";

export interface EpicParamState<T> {
  /** The epic the modal shows, or null when none is open. */
  selected: T | null;
  /**
   * The URL stopped naming the open epic (Back) while its open document
   * holds unsaved text. The modal stays mounted so the document can ask;
   * the answer is close() or cancelClose().
   */
  closeRequested: boolean;
  /** Open an epic's modal as a new history entry. */
  open: (epic: T) => void;
  /**
   * Close the modal, going back past every entry it pushed (documents opened
   * inside it included); one opened from a link drops the parameter in
   * place. After a Back the URL is already where it asked to go.
   */
  close: () => void;
  /** Keep the modal open after a Back, which pushes its parameter back. */
  cancelClose: () => void;
  /** Point the URL at an epic's new name after a rename from the modal. */
  renamed: (epic: T) => void;
  /** Told by the modal whether it holds unsaved edits (its open document's). */
  onDirtyChange: (dirty: boolean) => void;
}

/**
 * The epic modal on the Epics view, named in the URL (`epic=<name>`).
 * Nothing is decided until the list has loaded, and only the shown
 * project's list may be passed, so an epic of another project neither opens
 * nor is dropped by a list that is not its own; once one has loaded, an epic
 * it lacks (a bad link, or deleted) drops the parameter.
 *
 * An open epic is followed by id, so a rename elsewhere moves the URL to the
 * new name rather than closing the modal. A rename made here (renamed())
 * names the new name at once, and the list's old name neither shows nor
 * pulls the URL back until the list catches up.
 *
 * Unsaved text is never dropped without asking, as with useTicketParam: while
 * the modal reports itself dirty, a Back that drops the parameter keeps the
 * epic on screen with `closeRequested` set, so the document inside can ask.
 */
export function useEpicParam<T extends { id: string; name: string }>(epics: readonly T[] | null): EpicParamState<T> {
  const [params] = useSearchParams();
  const overlays = useOverlayHistory();
  const ref = params.get(EPIC_PARAM) ?? "";
  const found = useMemo(() => (epics && ref ? (findEpic(epics, ref) ?? null) : null), [epics, ref]);

  // Whether the modal holds unsaved edits, as it reports them. State, not a
  // ref: the render below decides on it.
  const [dirty, setDirty] = useState(false);

  // The epic last found by the URL, so a rename is followed by id.
  const [shownId, setShownId] = useState<string | null>(null);
  if (found && shownId !== found.id) setShownId(found.id);
  if (!ref && shownId !== null) setShownId(null);
  const byId = !found && ref && shownId && epics ? (epics.find((e) => e.id === shownId) ?? null) : null;
  const current = found ?? byId;

  // A rename made here, until the list carries the new name.
  const [renamedTo, setRenamedTo] = useState<T | null>(null);
  const stale = renamedTo !== null && current !== null && current.id === renamedTo.id && current.name !== renamedTo.name;
  if (renamedTo && (!ref || (current && !stale))) setRenamedTo(null);
  const shown = stale && current && renamedTo ? { ...current, name: renamedTo.name } : current;
  const followed = byId && !stale ? byId : null;

  useEffect(() => {
    if (followed) overlays.replace(withEpicName(latestSearchParams(params), followed));
  }, [followed, params, overlays]);

  // The epic last shown, kept while the modal holds unsaved edits and the
  // URL has dropped it. Let go only once the modal reports nothing unsaved.
  const [held, setHeld] = useState<T | null>(null);
  if (current && held !== current) setHeld(current);
  else if (!current && held && !dirty) setHeld(null);
  const holding = !current && dirty && held !== null;
  const selected = shown ?? (holding ? held : null);
  const closeRequested = holding && !ref;

  // Replaces rather than pops: the epic went without the user asking.
  const missing = ref !== "" && epics !== null && current === null && !dirty;
  useEffect(() => {
    if (missing) overlays.replace(withoutEpic(latestSearchParams(params)));
  }, [missing, params, overlays]);

  const open = useCallback(
    (epic: T) => {
      setDirty(false);
      overlays.push(withEpic(latestSearchParams(params), epic));
    },
    [params, overlays],
  );

  const close = useCallback(() => {
    setDirty(false);
    setHeld(null);
    // A Back already dropped the parameter: nothing to undo.
    if (!latestSearchParams(params).get(EPIC_PARAM)) return;
    overlays.closeAll();
  }, [params, overlays]);

  const cancelClose = useCallback(() => {
    // Back popped the epic's entry, so push it again: a second Back then
    // asks again, and a later close still goes back past it.
    if (held) overlays.push(withEpic(latestSearchParams(params), held));
  }, [held, params, overlays]);

  const renamed = useCallback(
    (epic: T) => {
      setShownId(epic.id);
      setRenamedTo(epic);
      overlays.replace(withEpicName(latestSearchParams(params), epic));
    },
    [params, overlays],
  );

  // setDirty is stable, so the modal's report never re-runs on its own.
  return { selected, closeRequested, open, close, cancelClose, renamed, onDirtyChange: setDirty };
}

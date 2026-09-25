import { useCallback, useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useOverlayHistory } from "./useOverlayHistory";
import { latestSearchParams } from "../lib/latestSearch";
import {
  findTicket,
  ticketRef,
  ticketUrl,
  withTicket,
  withoutTicket,
  type TicketIdentity,
} from "../lib/ticketParam";

export interface TicketParamState<T> {
  /** The ticket the editor shows, or null when none is open. */
  selected: T | null;
  /**
   * The URL has stopped naming the open ticket (Back, to the view or to the
   * previous ticket), but it holds unsaved edits, so the editor is still
   * mounted on it and has to ask before it goes.
   */
  closeRequested: boolean;
  /** Open a ticket's editor, which puts its key in the URL as a new history entry. */
  open: (ticket: TicketIdentity) => void;
  /**
   * Show another loaded ticket in the open editor, found by id, as a link
   * between tickets does, as a new history entry. Unsaved edits are the
   * editor's to ask about first.
   */
  switchTo: (id: string) => void;
  /**
   * Close the editor, going back past every entry it pushed; one opened
   * straight from a link drops the ticket parameter in place. After a Back
   * the URL is already where it asked to go, and is left there.
   */
  close: () => void;
  /** Keep the editor open after all, which pushes its ticket parameter back. */
  cancelClose: () => void;
  /** Told by the editor whether it holds unsaved edits. */
  onDirtyChange: (dirty: boolean) => void;
  /** The open ticket's shareable URL, for the editor header. */
  url?: string;
}

/**
 * The open ticket, kept in the URL rather than in component state, so every
 * view opens and closes its editor the same way and any view's URL can be
 * shared.
 *
 * `tickets` is whatever the view has loaded. Until they arrive, or when the
 * parameter names a ticket that is not among them, nothing opens and the
 * parameter is left alone — so the editor appears by itself once a load
 * finishes.
 *
 * A ticket that opened and then left a later load is a different matter: it
 * was deleted, so the parameter is dropped rather than left naming nothing,
 * and the editor closes with it (asking first, if it holds unsaved edits).
 * Only a reference that resolved here is dropped, so neither a first load nor
 * a refetch that failed and kept the previous list can take a parameter away.
 *
 * History: every ticket opened, from a view or from a link inside the editor,
 * is its own entry (see useOverlayHistory), so Back returns to the previous
 * ticket and Forward comes back. Closing the editor goes back past every
 * entry it pushed, so opening and closing leaves history as it was. An editor
 * opened straight from a link pushed nothing; closing it replaces instead.
 *
 * A Back that lands on another ticket while this one has unsaved edits keeps
 * this one on screen with `closeRequested` set, the same as a Back that drops
 * the parameter: the editor asks, then close() shows the ticket Back landed
 * on, or cancelClose() puts this one back as a new entry.
 *
 * Unsaved edits are never dropped without asking, whatever asked for the
 * close. A Back that drops the parameter therefore cannot unmount the editor
 * by itself: while the editor reports itself dirty it stays mounted with
 * `closeRequested` set, and it answers with close() or cancelClose().
 *
 * Each URL change starts from the URL as it is right now (see
 * latestSearchParams), so a filter change that has not finished rendering is
 * never rolled back.
 */
export function useTicketParam<T extends TicketIdentity>(
  tickets: readonly T[] | null | undefined,
): TicketParamState<T> {
  const [params] = useSearchParams();
  const overlays = useOverlayHistory();
  const ref = ticketRef(params);
  const fromUrl = useMemo(() => findTicket(tickets ?? [], ref) ?? null, [tickets, ref]);

  // Whether the editor holds unsaved edits, as it reports them. It has to be
  // state, not a ref: the render below decides on it.
  const [dirty, setDirty] = useState(false);

  // The ticket the editor shows. It follows the URL, except that a ticket
  // with unsaved edits is kept when the URL stops naming it (Back, or Back to
  // another ticket), so the editor can ask first. Derived during render
  // rather than in an effect, so a close with nothing to lose unmounts the
  // editor in the same commit and never paints a frame of an editor on its
  // way out. It is let go only once the editor reports nothing unsaved, which
  // is the whole point: a save that failed leaves the edits unsaved and the
  // editor dirty, so the ticket is still held and nothing unmounts under it.
  const [held, setHeld] = useState<T | null>(null);
  if (fromUrl) {
    if (held?.id !== fromUrl.id && !(held && dirty)) setHeld(fromUrl);
  } else if (held && !dirty) {
    setHeld(null);
  }

  // A held ticket the user chose to keep editing although it is gone: there
  // is no parameter worth putting back for it, so the request to close is
  // answered here instead.
  const [keptGone, setKeptGone] = useState<string | null>(null);

  // The URL's copy when it names the held ticket, so a refetch's newer
  // version reaches the editor; the held copy when the URL has moved on.
  const selected = held && fromUrl?.id !== held.id ? held : (fromUrl ?? held);
  const closeRequested = held !== null && fromUrl?.id !== held.id && keptGone !== held.id;

  // A reference that resolved here and then stopped naming a loaded ticket
  // names a deleted one; a key that never named anything is left alone.
  const [resolvedRef, setResolvedRef] = useState("");
  if (fromUrl && resolvedRef !== ref) setResolvedRef(ref);
  const vanished = ref !== "" && fromUrl === null && resolvedRef === ref;

  useEffect(() => {
    // Replaces rather than pops: the ticket went without the user asking, so
    // the view they are on stays where it is.
    if (vanished) overlays.replace(withoutTicket(latestSearchParams(params)));
  }, [vanished, params, overlays]);

  // Whether the ticket the editor holds has left the loaded tickets, which is
  // how a deletion elsewhere reaches this hook. A list that has not loaded
  // says nothing about it.
  const gone = held !== null && tickets != null && !tickets.some((t) => t.id === held.id);

  const open = useCallback(
    (ticket: TicketIdentity) => {
      setDirty(false);
      setKeptGone(null);
      overlays.push(withTicket(latestSearchParams(params), ticket));
    },
    [params, overlays],
  );

  const switchTo = useCallback(
    (id: string) => {
      const ticket = tickets?.find((t) => t.id === id);
      if (!ticket) return;
      setDirty(false);
      setKeptGone(null);
      overlays.push(withTicket(latestSearchParams(params), ticket));
    },
    [tickets, params, overlays],
  );

  const close = useCallback(() => {
    const holding = held;
    setDirty(false);
    setKeptGone(null);
    setHeld(null);
    const current = ticketRef(latestSearchParams(params));
    // Back already dropped the parameter, or moved it to another ticket: the
    // URL is already where the user asked to go.
    if (current === "") return;
    if (holding && !findTicket([holding], current)) return;
    overlays.closeAll();
  }, [held, params, overlays]);

  const cancelClose = useCallback(() => {
    if (!held) return;
    // Nothing to put back for a deleted ticket: a parameter naming it would
    // only be dropped again. The editor stays open on the edits instead.
    if (gone) {
      setKeptGone(held.id);
      return;
    }
    // Back popped this ticket's entry, so push it again rather than replacing
    // what Back landed on: a second Back then asks again, instead of leaving
    // with the edits unsaved and nothing asked. A later close goes back past
    // this entry too, so the history does not grow.
    overlays.push(withTicket(latestSearchParams(params), held));
  }, [held, gone, params, overlays]);

  const url = useMemo(
    () =>
      selected && typeof window !== "undefined"
        ? ticketUrl(window.location.origin, selected)
        : undefined,
    [selected],
  );

  // setDirty is stable, so the editor's report never re-runs on its own.
  return { selected, closeRequested, open, switchTo, close, cancelClose, onDirtyChange: setDirty, url };
}

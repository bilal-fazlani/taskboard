import { useCallback, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useOverlayHistory } from "./useOverlayHistory";
import { latestSearchParams } from "../lib/latestSearch";
import { findTicket, ticketRef, ticketUrl, withTicket, type TicketIdentity } from "../lib/ticketParam";

export interface TicketParamState<T> {
  /** The ticket the editor shows, or null when none is open. */
  selected: T | null;
  /**
   * The open ticket left the loaded tickets: it was deleted elsewhere. It
   * stays selected, with or without unsaved edits, so the editor can show it
   * read-only rather than closing or asking; only Close lets it go.
   */
  deleted: boolean;
  /**
   * The URL has stopped naming the open ticket (Back, to the view or to the
   * previous ticket), but it holds unsaved edits, so the editor is still
   * mounted on it and has to ask before it goes. Never set for a deleted
   * ticket: there is nothing left to ask about.
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
 * was deleted, so the editor keeps showing the last known copy, plus any
 * local edits, and `deleted` is set. Nothing asks and nothing unmounts: the
 * parameter stays put (a reload with it then opens nothing, ACP-25's
 * unknown-key rule) until the editor's own Close drops it, which is the only
 * way out of that state.
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
 * on, or cancelClose() puts this one back as a new entry. A deleted ticket
 * never sets `closeRequested`, even when Back is what drops the parameter for
 * everything else on screen: a read-only editor has nothing to ask about.
 *
 * Unsaved edits are never dropped without asking, whatever asked for the
 * close, unless the ticket itself is gone. A Back that drops the parameter
 * therefore cannot unmount a dirty, still-live editor by itself: it stays
 * mounted with `closeRequested` set, and answers with close() or
 * cancelClose().
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

  // Whether the ticket currently held has left the loaded tickets — deleted
  // elsewhere. Read before held is synced below, so a deleted ticket is never
  // cleared by a quiet refresh, clean or dirty. A list that has not loaded
  // says nothing about it.
  const gone = held !== null && tickets != null && !tickets.some((t) => t.id === held.id);

  // Whether the URL still names the ticket being held, by its own id or
  // display key — checked against the held ticket itself, not `tickets`, so
  // it says nothing about whether that ticket still exists. A deleted
  // ticket's read-only editor is kept on this instead of `fromUrl`: once a
  // ticket is gone, `fromUrl` can only ever be null (it is never found in
  // `tickets` again) or, if its display key is later reused by an unrelated
  // new ticket, resolve to that different ticket — neither of which may move
  // a read-only editor off the one it is actually showing.
  const stillNamed = held !== null && findTicket([held], ref) !== undefined;

  if (gone) {
    // Back (or Forward, or a link) moving the URL away from a deleted ticket
    // closes its read-only editor outright, the same as pressing Close:
    // there is nothing left to ask about. Until then it stays locked on
    // exactly this ticket.
    if (!stillNamed) setHeld(null);
  } else if (fromUrl) {
    if (held?.id !== fromUrl.id && !(held && dirty)) setHeld(fromUrl);
  } else if (held && !dirty) {
    setHeld(null);
  }

  // The URL's copy when it names the held ticket, so a refetch's newer
  // version reaches the editor; the held copy when the URL has moved on.
  const selected = held && fromUrl?.id !== held.id ? held : (fromUrl ?? held);
  // Never set for a deleted ticket: there is nothing to ask about, and the
  // parameter is left in place rather than dropped, so the editor stays
  // mounted and read-only until its own Close takes it away.
  const deleted = gone;
  const closeRequested = held !== null && !gone && fromUrl?.id !== held.id;

  const open = useCallback(
    (ticket: TicketIdentity) => {
      setDirty(false);
      overlays.push(withTicket(latestSearchParams(params), ticket));
    },
    [params, overlays],
  );

  const switchTo = useCallback(
    (id: string) => {
      const ticket = tickets?.find((t) => t.id === id);
      if (!ticket) return;
      setDirty(false);
      overlays.push(withTicket(latestSearchParams(params), ticket));
    },
    [tickets, params, overlays],
  );

  const close = useCallback(() => {
    const holding = held;
    setDirty(false);
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
    // Back popped this ticket's entry, so push it again rather than replacing
    // what Back landed on: a second Back then asks again, instead of leaving
    // with the edits unsaved and nothing asked. A later close goes back past
    // this entry too, so the history does not grow.
    overlays.push(withTicket(latestSearchParams(params), held));
  }, [held, params, overlays]);

  const url = useMemo(
    () =>
      selected && typeof window !== "undefined"
        ? ticketUrl(window.location.origin, selected)
        : undefined,
    [selected],
  );

  // setDirty is stable, so the editor's report never re-runs on its own.
  return { selected, deleted, closeRequested, open, switchTo, close, cancelClose, onDirtyChange: setDirty, url };
}

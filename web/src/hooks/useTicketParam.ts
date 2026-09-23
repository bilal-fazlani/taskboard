import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
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
   * The URL has stopped naming the open ticket, but it holds unsaved edits, so
   * the editor is still mounted and has to ask before it goes.
   */
  closeRequested: boolean;
  /** Open a ticket's editor, which puts its key in the URL. */
  open: (ticket: TicketIdentity) => void;
  /**
   * Show another loaded ticket in the open editor, found by id, as a link
   * between tickets does. Unsaved edits are the editor's to ask about first.
   */
  switchTo: (id: string) => void;
  /** Close the editor, which drops only the ticket parameter. */
  close: () => void;
  /** Keep the editor open after all, which puts the ticket parameter back. */
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
 * History: opening pushes an entry, so the browser's Back button closes the
 * editor, and closing from inside the editor pops that entry rather than
 * replacing it, so opening and closing leaves the history as it was and Back
 * still goes wherever the view came from. An editor opened straight from a
 * link pushed nothing, so closing that one replaces instead.
 *
 * Switching from one ticket to another replaces the entry rather than pushing
 * one, so the editor stays a single entry however many tickets it moved
 * through: one close, or one Back, leaves it.
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
  const [params, setParams] = useSearchParams();
  const navigate = useNavigate();
  const ref = ticketRef(params);
  const fromUrl = useMemo(() => findTicket(tickets ?? [], ref) ?? null, [tickets, ref]);

  // Whether the editor holds unsaved edits, as it reports them. It has to be
  // state, not a ref: the render below decides on it.
  const [dirty, setDirty] = useState(false);
  // Whether opening the editor pushed the history entry that closing should
  // pop. Nothing renders differently for it, so a ref is enough.
  const pushedRef = useRef(false);

  // The ticket the editor keeps showing after the URL has stopped naming it,
  // so it can ask about unsaved edits first. Derived during render rather than
  // in an effect, so a close with nothing to lose unmounts the editor in the
  // same commit and never paints a frame of an editor on its way out.
  // It is let go only once the editor reports nothing unsaved, which is the
  // whole point: a save that failed leaves the edits unsaved and the editor
  // dirty, so the ticket is still held and nothing unmounts underneath it.
  const [held, setHeld] = useState<T | null>(null);
  if (fromUrl) {
    if (held?.id !== fromUrl.id) setHeld(fromUrl);
  } else if (held && !dirty) {
    setHeld(null);
  }

  // A held ticket the user chose to keep editing although it is gone: there
  // is no parameter worth putting back for it, so the request to close is
  // answered here instead.
  const [keptGone, setKeptGone] = useState<string | null>(null);

  const selected = fromUrl ?? held;
  const closeRequested = fromUrl === null && held !== null && keptGone !== held.id;

  // The reference the editor last opened on. A reference that resolved here
  // and then stopped naming a loaded ticket names a deleted one, which is
  // what tells this apart from a key that never named anything: that one is
  // left in the URL, this one goes.
  const [resolvedRef, setResolvedRef] = useState("");
  if (fromUrl && resolvedRef !== ref) setResolvedRef(ref);
  const vanished = ref !== "" && fromUrl === null && resolvedRef === ref;

  useEffect(() => {
    // Replaces rather than pops: the ticket went without the user asking, so
    // the view they are on stays where it is.
    if (vanished) setParams(withoutTicket(latestSearchParams(params)), { replace: true });
  }, [vanished, params, setParams]);

  // Whether the ticket the editor holds has left the loaded tickets, which is
  // how a deletion elsewhere reaches this hook. A list that has not loaded
  // says nothing about it.
  const gone = held !== null && tickets != null && !tickets.some((t) => t.id === held.id);

  const open = useCallback(
    (ticket: TicketIdentity) => {
      setDirty(false);
      setKeptGone(null);
      pushedRef.current = true;
      setParams(withTicket(latestSearchParams(params), ticket));
    },
    [params, setParams],
  );

  const switchTo = useCallback(
    (id: string) => {
      const ticket = tickets?.find((t) => t.id === id);
      if (!ticket) return;
      setDirty(false);
      setKeptGone(null);
      setParams(withTicket(latestSearchParams(params), ticket), { replace: true });
    },
    [tickets, params, setParams],
  );

  const close = useCallback(() => {
    setDirty(false);
    setKeptGone(null);
    setHeld(null);
    // A close the URL asked for has already dropped the parameter, and the
    // entry opening pushed went with it.
    if (ticketRef(latestSearchParams(params)) === "") {
      pushedRef.current = false;
      return;
    }
    if (pushedRef.current) {
      pushedRef.current = false;
      navigate(-1);
      return;
    }
    setParams(withoutTicket(latestSearchParams(params)), { replace: true });
  }, [params, setParams, navigate]);

  const cancelClose = useCallback(() => {
    if (!held) return;
    // Nothing to put back when the ticket itself was deleted: a parameter
    // naming it would only be dropped again, and the question would come
    // straight back. The editor stays open on the edits instead, until the
    // user saves them elsewhere or lets them go.
    if (gone) {
      setKeptGone(held.id);
      return;
    }
    // Back popped the entry opening pushed, so push it again rather than
    // replacing what Back landed on: a second Back then drops the parameter
    // and asks again, instead of leaving the app with the edits unsaved and
    // nothing asked. Closing later pops this entry as usual, so the history
    // does not grow.
    pushedRef.current = true;
    setParams(withTicket(latestSearchParams(params), held));
  }, [held, gone, params, setParams]);

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

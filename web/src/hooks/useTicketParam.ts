import { useCallback, useMemo, useRef, useState } from "react";
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
 * History: opening pushes an entry, so the browser's Back button closes the
 * editor, and closing from inside the editor pops that entry rather than
 * replacing it, so opening and closing leaves the history as it was and Back
 * still goes wherever the view came from. An editor opened straight from a
 * link pushed nothing, so closing that one replaces instead.
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
  const [held, setHeld] = useState<T | null>(null);
  if (fromUrl) {
    if (held?.id !== fromUrl.id) setHeld(fromUrl);
  } else if (held && !dirty) {
    setHeld(null);
  }

  const selected = fromUrl ?? held;
  const closeRequested = fromUrl === null && held !== null;

  const open = useCallback(
    (ticket: TicketIdentity) => {
      setDirty(false);
      pushedRef.current = true;
      setParams(withTicket(latestSearchParams(params), ticket));
    },
    [params, setParams],
  );

  const close = useCallback(() => {
    setDirty(false);
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
    // Back popped the entry opening pushed, so push it again rather than
    // replacing what Back landed on: a second Back then drops the parameter
    // and asks again, instead of leaving the app with the edits unsaved and
    // nothing asked. Closing later pops this entry as usual, so the history
    // does not grow.
    if (!held) return;
    pushedRef.current = true;
    setParams(withTicket(latestSearchParams(params), held));
  }, [held, params, setParams]);

  const url = useMemo(
    () =>
      selected && typeof window !== "undefined"
        ? ticketUrl(window.location.origin, selected)
        : undefined,
    [selected],
  );

  // setDirty is stable, so the editor's report never re-runs on its own.
  return { selected, closeRequested, open, close, cancelClose, onDirtyChange: setDirty, url };
}

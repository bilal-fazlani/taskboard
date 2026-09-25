import { useCallback, useEffect, useMemo, useRef } from "react";
import { useLocation, useNavigate, useSearchParams } from "react-router-dom";
import { latestLocationState, latestSearchParams } from "../lib/latestSearch";
import { boardNavigation, keyBelow, onRefused } from "../lib/historyTraversal";
import { closedState, hasOverlay, overlayState, pushState, withoutOverlays } from "../lib/overlayHistory";

export interface OverlayHistory {
  /** How many overlay entries sit on top of the view they opened from. */
  depth: number;
  /** Open an overlay as a new history entry. */
  push: (next: URLSearchParams) => void;
  /** Change the current entry's URL without adding one, keeping its depth. */
  replace: (next: URLSearchParams) => void;
  /**
   * Close the top overlay: one Back when it was pushed, otherwise (it came
   * from a link, or the entry below is gone) replace the entry with `without`.
   */
  closeOne: (without: URLSearchParams) => void;
  /** Close every overlay and return to the view they opened from. */
  closeAll: () => void;
}

/**
 * History for the overlays in the query string (see lib/overlayHistory).
 * Every change starts from the latest URL and state, not the rendered ones,
 * because React Router renders navigations in a transition.
 */
export function useOverlayHistory(): OverlayHistory {
  const [params, setParams] = useSearchParams();
  const location = useLocation();
  const navigate = useNavigate();
  const depth = overlayState(location.state).depth;

  // Set by closeAll when going back lands on an entry that still has an
  // overlay (the editor was opened from a link); cleared once it is stripped.
  const stripRef = useRef(false);
  useEffect(() => {
    if (!stripRef.current || depth !== 0) return;
    stripRef.current = false;
    const latest = latestSearchParams(params);
    if (hasOverlay(latest)) {
      setParams(withoutOverlays(latest), { replace: true, state: latestLocationState(location.state) });
    }
  }, [location.key, location.state, depth, params, setParams]);

  const push = useCallback(
    (next: URLSearchParams) => {
      const baseHasOverlay = hasOverlay(latestSearchParams(params));
      setParams(next, { state: pushState(latestLocationState(location.state), baseHasOverlay) });
    },
    [params, setParams, location.state],
  );

  const replace = useCallback(
    (next: URLSearchParams) => setParams(next, { replace: true, state: latestLocationState(location.state) }),
    [setParams, location.state],
  );

  // Going back uses the Navigation API where the browser has it, so entries an
  // HTML document added inside its frame are skipped (lib/historyTraversal).
  // When the entry to go back to is gone, the overlays close in place instead.
  const closeOne = useCallback(
    (without: URLSearchParams) => {
      if (overlayState(latestLocationState(location.state)).depth === 0) {
        replace(without);
        return;
      }
      const navigation = boardNavigation();
      if (!navigation) {
        navigate(-1);
        return;
      }
      // The entries below are gone, so nothing counts below this one now.
      onRefused(navigation.back(), () =>
        setParams(without, { replace: true, state: closedState(latestLocationState(location.state)) }),
      );
    },
    [navigate, replace, setParams, location.state],
  );

  const closeAll = useCallback(() => {
    const now = overlayState(latestLocationState(location.state));
    if (now.depth === 0) {
      replace(withoutOverlays(latestSearchParams(params)));
      return;
    }
    const closeInPlace = () => {
      stripRef.current = false;
      setParams(withoutOverlays(latestSearchParams(params)), {
        replace: true,
        state: closedState(latestLocationState(location.state)),
      });
    };
    const navigation = boardNavigation();
    stripRef.current = now.fromLink;
    if (!navigation) {
      navigate(-now.depth);
      return;
    }
    const key = keyBelow(navigation, now.depth);
    if (key === null) closeInPlace();
    else onRefused(navigation.traverseTo(key), closeInPlace);
  }, [navigate, replace, setParams, params, location.state]);

  return useMemo(() => ({ depth, push, replace, closeOne, closeAll }), [depth, push, replace, closeOne, closeAll]);
}

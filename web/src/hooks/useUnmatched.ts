import { useCallback, useEffect } from "react";
import { useSearchParams } from "react-router-dom";
import { hasInvalidUnmatched, parseUnmatched, withUnmatched, type UnmatchedMode } from "../lib/filters";
import { latestSearchParams } from "../lib/latestSearch";

export interface UnmatchedState {
  mode: UnmatchedMode;
  setMode: (mode: UnmatchedMode) => void;
}

// Whether Dependencies dims or hides the cards the filters don't match, read
// from and written to the URL (see filters.ts). Like a filter, a change starts
// from the latest URL and replaces the history entry. An `unmatched` value
// that means nothing is dropped from the URL, leaving the view in dim mode.
// Only Dependencies uses this: Kanban and Table leave the parameter alone, so
// it's still there on the way back.
export function useUnmatched(): UnmatchedState {
  const [params, setParams] = useSearchParams();
  const mode = parseUnmatched(params);
  const invalid = hasInvalidUnmatched(params);
  useEffect(() => {
    if (!invalid) return;
    // Checked again against the latest URL, which a newer change may already
    // have put right.
    const latest = latestSearchParams(params);
    if (hasInvalidUnmatched(latest)) setParams(withUnmatched(latest, "dim"), { replace: true });
  }, [invalid, params, setParams]);
  const setMode = useCallback(
    (next: UnmatchedMode) => setParams(withUnmatched(latestSearchParams(params), next), { replace: true }),
    [params, setParams],
  );
  return { mode, setMode };
}

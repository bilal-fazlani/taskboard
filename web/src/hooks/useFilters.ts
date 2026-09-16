import { useCallback, useMemo } from "react";
import { useSearchParams } from "react-router-dom";
import { hasFilters, parseFilters, withFilter, withoutFilters, type FilterKey, type Filters } from "../lib/filters";
import { latestSearchParams } from "../lib/latestSearch";

export interface FilterState {
  filters: Filters;
  /** Whether any filter is set. */
  active: boolean;
  /** The filters in the URL right now, which can be ahead of `filters` while a navigation renders. */
  latestFilters: () => Filters;
  setFilter: (key: FilterKey, value: string) => void;
  clearFilters: () => void;
}

// The view filters, read from and written to the URL's query string. Each
// change starts from the latest URL (see latestSearchParams) and replaces the
// history entry rather than pushing one, so typing a search doesn't leave a
// back-button stop per keystroke. Other query parameters are kept.
export function useFilters(): FilterState {
  const [params, setParams] = useSearchParams();
  const filters = useMemo(() => parseFilters(params), [params]);
  const latestFilters = useCallback(() => parseFilters(latestSearchParams(params)), [params]);
  const setFilter = useCallback(
    (key: FilterKey, value: string) =>
      setParams(withFilter(latestSearchParams(params), key, value), { replace: true }),
    [params, setParams],
  );
  const clearFilters = useCallback(
    () => setParams(withoutFilters(latestSearchParams(params)), { replace: true }),
    [params, setParams],
  );
  return { filters, active: hasFilters(filters), latestFilters, setFilter, clearFilters };
}

import { useCallback, useMemo } from "react";
import { useSearchParams } from "react-router-dom";
import {
  NARROWING_KEYS,
  hasFilters,
  parseFilters,
  withFilter,
  withoutFilters,
  withoutValues,
  type FilterKey,
  type FilterValue,
  type Filters,
} from "../lib/filters";
import { latestLocationState, latestSearchParams } from "../lib/latestSearch";

export interface FilterState {
  filters: Filters;
  /**
   * Whether any filter but the project is set. Every view always has a
   * project, so it's the other filters that make a view filtered.
   */
  active: boolean;
  /** The filters in the URL right now, which can be ahead of `filters` while a navigation renders. */
  latestFilters: () => Filters;
  /** Set one filter: a value for the project and the search, the values for any other. */
  setFilter: <K extends FilterKey>(key: K, value: Filters[K]) => void;
  /** Remove several filters at once. */
  dropFilters: (keys: readonly FilterKey[]) => void;
  /** Remove single values of filters, for ones that no longer name anything, keeping the filters' other values. */
  dropValues: (values: readonly FilterValue[]) => void;
  /** Remove every filter but the project, which a view always keeps. */
  clearFilters: () => void;
}

// The view filters, read from and written to the URL's query string. Each
// change starts from the latest URL (see latestSearchParams) and replaces the
// history entry rather than pushing one, so typing a search doesn't leave a
// back-button stop per keystroke. Other query parameters are kept.
// The entry's location state (the overlay depth, see lib/overlayHistory) is kept.
export function useFilters(): FilterState {
  const [params, setParams] = useSearchParams();
  const filters = useMemo(() => parseFilters(params), [params]);
  const latestFilters = useCallback(() => parseFilters(latestSearchParams(params)), [params]);
  const setFilter = useCallback(
    <K extends FilterKey>(key: K, value: Filters[K]) =>
      setParams(withFilter(latestSearchParams(params), key, value), {
        replace: true,
        state: latestLocationState(undefined),
      }),
    [params, setParams],
  );
  const dropFilters = useCallback(
    (keys: readonly FilterKey[]) => {
      if (keys.length === 0) return;
      setParams(withoutFilters(latestSearchParams(params), keys), {
        replace: true,
        state: latestLocationState(undefined),
      });
    },
    [params, setParams],
  );
  const dropValues = useCallback(
    (values: readonly FilterValue[]) => {
      if (values.length === 0) return;
      setParams(withoutValues(latestSearchParams(params), values), {
        replace: true,
        state: latestLocationState(undefined),
      });
    },
    [params, setParams],
  );
  const clearFilters = useCallback(
    () =>
      setParams(withoutFilters(latestSearchParams(params), NARROWING_KEYS), {
        replace: true,
        state: latestLocationState(undefined),
      }),
    [params, setParams],
  );
  return {
    filters,
    active: hasFilters(filters, NARROWING_KEYS),
    latestFilters,
    setFilter,
    dropFilters,
    dropValues,
    clearFilters,
  };
}

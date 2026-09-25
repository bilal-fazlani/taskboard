// Dropping filters from the URL once they name something that is gone.
//
// Filter state lives in the URL (see filters.ts) and survives both a reload
// and a live refetch — but only while it is still valid. A filter naming a
// project or label that has since been deleted would otherwise leave the view
// empty for a reason the user cannot see, with a dropdown offering a value
// that exists nowhere but in the URL.
//
// An epic goes the same way once it names no epic of the selected project,
// which is also what drops it on a switch to another project; `none` names no
// record and always stays.
//
// The epic and label filters take several values, and only the values that
// name nothing are dropped: the filter keeps the others.
//
// Only those three are checked. Status and priority are fixed sets, so they can
// never name something deleted; a repo is a string the tickets themselves
// carry, and the search is free text, so neither names a record either.

import { isNoEpic, type FilterKey, type FilterValue, type Filters } from "./filters";

/** The filters whose value names a record that can be deleted. */
export const CHECKED_FILTERS: readonly FilterKey[] = ["project", "epic", "label"];

/**
 * The names a view knows to exist right now: project prefixes, the selected
 * project's epic names and label names. A list that hasn't loaded is null, and nothing is dropped against
 * it — an empty list would otherwise read as "nothing exists" and throw away
 * a perfectly good filter on the way to the first render.
 */
export interface KnownNames {
  projects: readonly string[] | null;
  /** The epics of the project the view shows; left out, like null, when not known. */
  epics?: readonly string[] | null;
  labels: readonly string[] | null;
}

const names = (known: readonly string[], value: string) =>
  known.some((name) => name.toLowerCase() === value.toLowerCase());

/**
 * The filter values that name nothing, each as the URL writes it, compared
 * the way matchesFilters compares them: ignoring case, as `?project=acp`
 * filters the same tickets as `?project=ACP`.
 */
export function staleFilters(filters: Filters, known: KnownNames): FilterValue[] {
  const stale: FilterValue[] = [];
  const check = (key: FilterKey, values: readonly string[], loaded: readonly string[] | null) => {
    if (loaded === null) return;
    for (const value of values) if (value !== "" && !names(loaded, value)) stale.push({ key, value });
  };
  check("project", [filters.project], known.projects);
  check(
    "epic",
    filters.epic.filter((epic) => !isNoEpic(epic)),
    known.epics ?? null,
  );
  check("label", filters.label, known.labels);
  return stale;
}

// The project a view shows when its URL names none.
//
// Every view (Dependencies, Kanban, Table) always shows one project: there is
// no "All projects". A URL without a `project` (a first visit, a link or
// bookmark without one, or one whose project was deleted and dropped) gets
// one picked for it: the project last shown on any view, remembered in
// localStorage, or else the first project in the list. With no projects at
// all there is nothing to pick, and the view shows its empty state.
//
// localStorage can be missing or throw (private windows, blocked site data),
// so every access is wrapped: storage that can't be used just means there is
// no memory.

/** The localStorage key holding the last project shown, by prefix. */
export const LAST_PROJECT_KEY = "taskboard.lastProject";

const same = (a: string, b: string) => a.toLowerCase() === b.toLowerCase();

/** The prefix in `prefixes` that `value` names, ignoring case, as the filters compare it. */
export function namedProject(prefixes: readonly string[], value: string | null): string | null {
  if (!value) return null;
  return prefixes.find((prefix) => same(prefix, value)) ?? null;
}

/**
 * The project to show when the URL names none that exists: the remembered one
 * if it still exists, else the first in the list, else "" when there are no
 * projects at all.
 */
export function defaultProject(prefixes: readonly string[], remembered: string | null): string {
  return namedProject(prefixes, remembered) ?? prefixes[0] ?? "";
}

/** The remembered project's prefix, or null when there is none or storage can't be read. */
export function readLastProject(): string | null {
  try {
    return globalThis.localStorage?.getItem(LAST_PROJECT_KEY) ?? null;
  } catch {
    return null;
  }
}

/** Remembers the project shown, for the next view without one. Storage that can't be written is ignored. */
export function rememberProject(prefix: string): void {
  try {
    globalThis.localStorage?.setItem(LAST_PROJECT_KEY, prefix);
  } catch {
    // No storage, no memory.
  }
}

/**
 * Whether a view is still waiting for its project to be picked: the URL names
 * none yet, and there are projects to pick from (or the list is still on its
 * way). A view shows its loading state meanwhile rather than every project's
 * tickets. With no projects at all nothing will be picked, and the view shows
 * its empty state.
 */
export function awaitingProject(project: string, projects: readonly unknown[] | null): boolean {
  return project === "" && (projects === null || projects.length > 0);
}

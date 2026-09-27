// The project the Now page last showed: a prefix, or "" for "All projects".
//
// Now keeps its own memory, apart from the views' last project (see
// defaultProject.ts): a pick on Kanban, Table, Dependencies, Epics or
// Activity doesn't move Now, and Now never moves the views. Every Now URL
// names what it shows, a prefix or `all` (ALL_PROJECTS), and whatever it
// names is remembered: a pick in the dropdown, a link, or Back to an earlier
// entry. A Now URL without a project only means "open on the remembered
// pick": it is replaced by one naming that project when it still exists,
// else by `all`, as with nothing remembered.
//
// localStorage can be missing or throw (private windows, blocked site data),
// so every access is wrapped: storage that can't be used just means there is
// no memory, which is "All projects".

import { namedProject, type PickableProject } from "./defaultProject";

/** The localStorage key holding the project Now last showed, by prefix; "" for every project. */
export const NOW_PROJECT_KEY = "taskboard.nowProject";

/**
 * The `project` value that names "All projects" in a Now URL. It matches in
 * any letter case, like prefixes, and the server refuses it as a prefix, so
 * it never names a project.
 */
export const ALL_PROJECTS = "all";

/** Whether a `project` value names "All projects", ignoring case. */
export function namesAllProjects(value: string): boolean {
  return value.toLowerCase() === ALL_PROJECTS;
}

/** Now's remembered pick: a prefix, or "" for every project, when none was made or storage can't be read. */
export function readNowProject(): string {
  try {
    return globalThis.localStorage?.getItem(NOW_PROJECT_KEY) ?? "";
  } catch {
    return "";
  }
}

/** Remembers Now's pick, a prefix or "" for every project. Storage that can't be written is ignored. */
export function rememberNowProject(prefix: string): void {
  try {
    globalThis.localStorage?.setItem(NOW_PROJECT_KEY, prefix);
  } catch {
    // No storage, no memory.
  }
}

/**
 * The `project` to write into a Now URL without one: the remembered project,
 * spelled as the list spells it, if it still exists; else ALL_PROJECTS (it
 * has been deleted since, or All or nothing was remembered).
 */
export function restoredNowProject(projects: readonly PickableProject[], remembered: string): string {
  return namedProject(projects.map((p) => p.prefix), remembered) ?? ALL_PROJECTS;
}

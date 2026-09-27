// The project the Now page last showed: a prefix, or "" for "All projects".
//
// Now keeps its own memory, apart from the views' last project (see
// defaultProject.ts): a pick on Kanban, Table, Dependencies, Epics or
// Activity doesn't move Now, and Now never moves the views. Only a pick in
// Now's dropdown, or a URL naming a project, changes it. Opening Now with no
// project in the URL restores it, when that project still exists; else
// the page shows every project, as it does with nothing remembered.
//
// localStorage can be missing or throw (private windows, blocked site data),
// so every access is wrapped: storage that can't be used just means there is
// no memory, which is "All projects".

import { namedProject, type PickableProject } from "./defaultProject";

/** The localStorage key holding the project Now last showed, by prefix; "" for every project. */
export const NOW_PROJECT_KEY = "taskboard.nowProject";

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
 * The project to restore into a Now URL without one, spelled as the list
 * spells it: the remembered one if it still exists, else "" for every
 * project (it has been deleted since, or nothing was remembered).
 */
export function restoredNowProject(projects: readonly PickableProject[], remembered: string): string {
  return namedProject(projects.map((p) => p.prefix), remembered) ?? "";
}

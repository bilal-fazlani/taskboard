// The ticket view last shown: Dependencies, Kanban or Table.
//
// The Epics view opens an epic's tickets in whichever of the three was used
// last, so following an epic lands where the user was already working, and a
// ticket's canonical link, /?ticket=<KEY>, opens the ticket there too (see
// pages/Home.tsx). It is remembered in localStorage like the last project
// (see defaultProject.ts), and storage that is missing or throws just means
// there is no memory, which falls back to Kanban.

import { isCurrentPath } from "./navigation";

/** The localStorage key holding the path of the last ticket view shown. */
export const LAST_VIEW_KEY = "taskboard.lastView";

/** The paths of the views that show tickets, which an epic's row can open. */
export const TICKET_VIEW_PATHS = ["/dependencies", "/kanban", "/table"] as const;

export type TicketViewPath = (typeof TICKET_VIEW_PATHS)[number];

/** Where an epic's row goes when no ticket view has been shown yet: Kanban. */
export const FALLBACK_VIEW: TicketViewPath = "/kanban";

// Dependencies was served at / until Now became the home page, so a memory
// stored before then holds "/" for it.
const OLD_DEPENDENCIES_PATH = "/";

/**
 * The ticket view at `path`, matched by the sidebar's rule (one trailing slash
 * and letter case ignored, see isCurrentPath), or null for any other page.
 */
export function ticketViewAt(path: string | null): TicketViewPath | null {
  if (path === null) return null;
  return TICKET_VIEW_PATHS.find((view) => isCurrentPath(path, view)) ?? null;
}

/** The last ticket view shown, or Kanban when none was or storage can't be read. */
export function readLastView(): TicketViewPath {
  try {
    const path = globalThis.localStorage?.getItem(LAST_VIEW_KEY) ?? null;
    if (path === OLD_DEPENDENCIES_PATH) return "/dependencies";
    return ticketViewAt(path) ?? FALLBACK_VIEW;
  } catch {
    return FALLBACK_VIEW;
  }
}

/**
 * Remembers the view shown, under its own path, when it is a ticket view;
 * any other page is ignored.
 */
export function rememberView(path: string): void {
  const view = ticketViewAt(path);
  if (!view) return;
  try {
    globalThis.localStorage?.setItem(LAST_VIEW_KEY, view);
  } catch {
    // No storage, no memory.
  }
}

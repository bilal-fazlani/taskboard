// The ticket view last shown: Dependencies, Kanban or Table.
//
// The Epics view opens an epic's tickets in whichever of the three was used
// last, so following an epic lands where the user was already working. It is
// remembered in localStorage like the last project (see defaultProject.ts),
// and storage that is missing or throws just means there is no memory, which
// falls back to Kanban.

/** The localStorage key holding the path of the last ticket view shown. */
export const LAST_VIEW_KEY = "taskboard.lastView";

/** The paths of the views that show tickets, which an epic's row can open. */
export const TICKET_VIEW_PATHS = ["/", "/kanban", "/table"] as const;

export type TicketViewPath = (typeof TICKET_VIEW_PATHS)[number];

/** Where an epic's row goes when no ticket view has been shown yet: Kanban. */
export const FALLBACK_VIEW: TicketViewPath = "/kanban";

export function isTicketView(path: string | null): path is TicketViewPath {
  return path !== null && (TICKET_VIEW_PATHS as readonly string[]).includes(path);
}

/** The last ticket view shown, or Kanban when none was or storage can't be read. */
export function readLastView(): TicketViewPath {
  try {
    const path = globalThis.localStorage?.getItem(LAST_VIEW_KEY) ?? null;
    return isTicketView(path) ? path : FALLBACK_VIEW;
  } catch {
    return FALLBACK_VIEW;
  }
}

/** Remembers the path shown when it is a ticket view; any other page is ignored. */
export function rememberView(path: string): void {
  if (!isTicketView(path)) return;
  try {
    globalThis.localStorage?.setItem(LAST_VIEW_KEY, path);
  } catch {
    // No storage, no memory.
  }
}

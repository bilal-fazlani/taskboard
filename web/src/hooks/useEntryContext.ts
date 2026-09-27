import { useCallback, useEffect, useRef, useState } from "react";
import { api, type EntryOwnerRef } from "../api/client";
import { useLiveRefresh } from "./useLiveRefresh";

/** One level's entries as the ticket page's side column counts them. */
export interface EntryCounts {
  decisions: number;
  learnings: number;
  openNotes: number;
}

export interface EntryContext {
  /** Null when the ticket has no epic. */
  epic: EntryCounts | null;
  project: EntryCounts;
}

/** How many current entries of one type an owner has: the total of a one-entry page. */
function countOf(owner: EntryOwnerRef, type: string): Promise<number> {
  return api.entries.list(owner, { types: [type], limit: 1 }).then((page) => page.total);
}

/**
 * The entries around a ticket, counted: its epic's and its project's current
 * decisions and learnings (from the entry reads) and open notes (from the
 * ticket's read, which counts them). Loaded on mount and on every live
 * change, since a note on the epic or project doesn't touch the ticket. Null
 * until the first load, and a failed load keeps what is on screen.
 */
export function useEntryContext(ticketId: string, projectId: string, epicId: string | undefined) {
  const [value, setValue] = useState<{ key: string; context: EntryContext } | null>(null);
  const key = `${ticketId}:${projectId}:${epicId ?? ""}`;
  const seq = useRef(0);

  const reload = useCallback(() => {
    const n = ++seq.current;
    const levelCounts = (owner: EntryOwnerRef, openNotes: number) =>
      Promise.all([countOf(owner, "decision"), countOf(owner, "learning")]).then(
        ([decisions, learnings]): EntryCounts => ({ decisions, learnings, openNotes }),
      );
    Promise.resolve()
      .then(() => api.tickets.get(ticketId))
      .then((full) =>
        Promise.all([
          epicId ? levelCounts({ epicId }, full.epicOpenNotes ?? 0) : Promise.resolve(null),
          levelCounts({ projectId }, full.projectOpenNotes ?? 0),
        ]),
      )
      .then(([epic, project]) => {
        if (n === seq.current) setValue({ key, context: { epic, project } });
      })
      .catch(() => {
        // A failed load leaves the counts on screen as they were.
      });
  }, [key, ticketId, projectId, epicId]);

  useEffect(() => {
    reload();
  }, [reload]);
  useLiveRefresh(reload);

  return value?.key === key ? value.context : null;
}

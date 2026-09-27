import { useCallback, useEffect, useRef, useState } from "react";
import { api, type Entry, type EntryAgent, type EntryOwnerRef } from "../api/client";
import { useLiveRefresh } from "./useLiveRefresh";

/** Pages read per load at most: 100 entries a page, so up to 1000 entries. */
const MAX_PAGES = 10;

export interface OwnerEntries {
  /** Every entry, replaced ones included, newest first. */
  entries: Entry[];
  /** The agents they name, by id. */
  agents: Record<string, EntryAgent>;
}

function ownerKey(owner: EntryOwnerRef): string {
  if ("ticketId" in owner) return `ticket:${owner.ticketId}`;
  if ("epicId" in owner) return `epic:${owner.epicId}`;
  return `project:${owner.projectId}`;
}

function ownerFromKey(key: string): EntryOwnerRef {
  const at = key.indexOf(":");
  const kind = key.slice(0, at);
  const id = key.slice(at + 1);
  if (kind === "ticket") return { ticketId: id };
  if (kind === "epic") return { epicId: id };
  return { projectId: id };
}

/**
 * An owner's entries, replaced ones included, with the agents they name:
 * loaded on mount and again on every live change, so an agent's new entry
 * shows up without a reload. Each load keeps only its newest reply. Null
 * until the first load; a failed first load sets `failed`, and a failed
 * reload keeps what is on screen.
 */
export function useOwnerEntries(owner: EntryOwnerRef) {
  const key = ownerKey(owner);
  const [loaded, setLoaded] = useState<{ key: string; value: OwnerEntries } | null>(null);
  const [failedKey, setFailedKey] = useState<string | null>(null);
  const seq = useRef(0);

  const reload = useCallback(() => {
    const n = ++seq.current;
    const target = ownerFromKey(key);
    const readAll = async (): Promise<OwnerEntries> => {
      const out: OwnerEntries = { entries: [], agents: {} };
      let before: string | undefined;
      for (let i = 0; i < MAX_PAGES; i++) {
        const page = await api.entries.list(target, { includeReplaced: true, limit: 100, before });
        out.entries.push(...page.entries);
        Object.assign(out.agents, page.agents);
        if (!page.hasMore || !page.nextBefore) break;
        before = page.nextBefore;
      }
      return out;
    };
    // Through a promise so a test's API mock without `entries` fails the
    // load rather than the render.
    Promise.resolve()
      .then(readAll)
      .then((value) => {
        if (n !== seq.current) return;
        setLoaded({ key, value });
        setFailedKey(null);
      })
      .catch(() => {
        if (n === seq.current) setFailedKey(key);
      });
  }, [key]);

  useEffect(() => {
    reload();
  }, [reload]);
  useLiveRefresh(reload);

  const value = loaded?.key === key ? loaded.value : null;
  return { value, failed: value === null && failedKey === key, reload };
}

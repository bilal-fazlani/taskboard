import { useCallback, useEffect, useRef, useState } from "react";
import { api, type DocumentMeta, type DocumentOwnerRef } from "../api/client";
import { ownerKey } from "../lib/documents";
import { useLiveRefresh } from "./useLiveRefresh";

/**
 * An owner's documents, loaded on mount and again on every live change, so
 * an agent's new document shows up without a reload. Each load keeps only
 * its newest reply. Null until the first load; a failed first load sets
 * `failed`, and a failed reload keeps what is on screen.
 */
export function useOwnerDocuments(owner: DocumentOwnerRef) {
  const { ticketId } = owner;
  const key = ownerKey(owner);
  const [loaded, setLoaded] = useState<{ key: string; documents: DocumentMeta[] } | null>(null);
  const [failedKey, setFailedKey] = useState<string | null>(null);
  const seq = useRef(0);

  // Settles once this load is in (or superseded, or failed), so a caller
  // can wait for the list to carry a document it has just created.
  const reload = useCallback((): Promise<void> => {
    const n = ++seq.current;
    // Through a promise so a test's API mock without `documents` fails the
    // load rather than the render.
    return Promise.resolve()
      .then(() => api.documents.list({ ticketId }))
      .then((docs) => {
        if (n !== seq.current) return;
        setLoaded({ key, documents: Array.isArray(docs) ? docs : [] });
        setFailedKey(null);
      })
      .catch(() => {
        if (n === seq.current) setFailedKey(key);
      });
  }, [key, ticketId]);

  useEffect(() => {
    void reload();
  }, [reload]);
  useLiveRefresh(reload);

  const documents = loaded?.key === key ? loaded.documents : null;
  return { documents, failed: documents === null && failedKey === key, reload };
}

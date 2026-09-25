import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../api/client";
import { useLiveRefresh } from "./useLiveRefresh";

/** How long the search waits after the last keystroke before asking. */
export const DOCUMENT_SEARCH_DEBOUNCE_MS = 250;

/**
 * The ids of the tickets in `project` whose documents' names or readable
 * text hold the search text, for matchesFilters. Documents can be megabytes,
 * and their readable text is worked out on the server, so the server
 * searches them: this asks once the typing settles and again on every live
 * change. Null for an empty search, and until a first answer arrives; after
 * that the last answer stands while the next one loads, so cards matched by
 * a document don't blink out and back while typing. A failed request leaves
 * the last answer, or the search to the text the browser already has.
 */
export function useDocumentMatches(q: string, project: string): ReadonlySet<string> | null {
  const query = q.trim();
  const [ids, setIds] = useState<ReadonlySet<string> | null>(null);
  // Numbers each request, so only the newest one's answer is kept.
  const seq = useRef(0);
  // A cleared search forgets its answer, so the next one starts from none.
  // State adjusted while rendering, since the effect below must not set it.
  if (!query && ids !== null) setIds(null);

  const load = useCallback(() => {
    if (!query) return;
    const n = ++seq.current;
    // Through a promise, so a client without the call fails like a request.
    Promise.resolve()
      .then(() => api.documents.search(query, project))
      .then((result) => {
        if (n === seq.current) setIds(new Set(result?.ticketIds ?? []));
      })
      .catch(() => {});
  }, [query, project]);

  useEffect(() => {
    // A newer query, or none, overtakes any request still on its way.
    seq.current++;
    if (!query) return;
    const timer = setTimeout(load, DOCUMENT_SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [load, query]);

  useLiveRefresh(load);

  return query ? ids : null;
}

import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../api/client";
import { useLiveRefresh } from "./useLiveRefresh";

/** How long the search waits after the last keystroke before asking. */
export const DOCUMENT_SEARCH_DEBOUNCE_MS = 250;

export interface DocumentSearch {
  /** What useDocumentMatches answers. */
  ids: ReadonlySet<string> | null;
  /**
   * Whether this search in this project has had its answer, rather than the
   * last one standing in while it loads. It turns true once, when the first
   * request for the search comes back, and stays true through every later
   * one, answered on a live change. A failed request counts as its answer:
   * the ids it leaves stand, and a later success, arriving on a live change,
   * is no longer the first. An empty search has nothing to wait for.
   */
  settled: boolean;
}

// The search and project an answer is to.
const searchKey = (query: string, project: string) => `${project}\n${query}`;

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
  return useDocumentSearch(q, project).ids;
}

/** useDocumentMatches, also saying whether its answer is to this search. */
export function useDocumentSearch(q: string, project: string): DocumentSearch {
  const query = q.trim();
  // The last answer, and the search it was the answer to.
  const [answer, setAnswer] = useState<{ ids: ReadonlySet<string> | null; to: string } | null>(null);
  // Numbers each request, so only the newest one's answer is kept.
  const seq = useRef(0);
  // A cleared search forgets its answer, so the next one starts from none.
  // State adjusted while rendering, since the effect below must not set it.
  if (!query && answer !== null) setAnswer(null);

  const load = useCallback(() => {
    if (!query) return;
    const n = ++seq.current;
    const to = searchKey(query, project);
    // Through a promise, so a client without the call fails like a request.
    Promise.resolve()
      .then(() => api.documents.search(query, project))
      .then((result) => {
        if (n === seq.current) setAnswer({ ids: new Set(result?.ticketIds ?? []), to });
      })
      .catch(() => {
        // The ids stand, but the search has had its answer.
        if (n === seq.current) setAnswer((prev) => (prev?.to === to ? prev : { ids: prev?.ids ?? null, to }));
      });
  }, [query, project]);

  useEffect(() => {
    // A newer query, or none, overtakes any request still on its way.
    seq.current++;
    if (!query) return;
    const timer = setTimeout(load, DOCUMENT_SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [load, query]);

  useLiveRefresh(load);

  if (!query) return { ids: null, settled: true };
  return { ids: answer?.ids ?? null, settled: answer?.to === searchKey(query, project) };
}

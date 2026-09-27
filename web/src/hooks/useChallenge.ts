import { useCallback, useState } from "react";
import type { Entry } from "../api/client";

/**
 * The entry a new note challenges, set by an entry's Challenge. Each
 * Challenge counts up (`seq`), so the note box takes focus again even when
 * the same entry is challenged twice. `challenge` is the entries' action;
 * undefined when read only, which leaves the cards without one.
 */
export function useChallenge(readOnly: boolean) {
  const [about, setAbout] = useState<{ entry: Entry; seq: number } | null>(null);
  const start = useCallback(
    (entry: Entry) => setAbout((prev) => ({ entry, seq: (prev?.seq ?? 0) + 1 })),
    [],
  );
  const clear = useCallback(() => setAbout(null), []);
  return {
    about: about?.entry ?? null,
    focusSeq: about?.seq ?? 0,
    challenge: readOnly ? undefined : start,
    clear,
  };
}

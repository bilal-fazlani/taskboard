# Ticket 0: Back and Forward through tickets — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every ticket opened in the editor, including one reached by following a Depends on or Blocks link, is its own browser history entry, so Back and Forward step through them; closing the editor still leaves history as it was before it opened.

**Architecture:** A small `useOverlayHistory` hook records, in React Router's location state, how many overlay entries (ticket, and later document and epic) sit on top of the view they opened from. Opening pushes with depth + 1; closing one layer goes back one entry; closing everything goes back `depth` entries. `useTicketParam` switches from its `pushedRef` flag to this hook, and learns to hold a dirty editor when Back lands on a different ticket.

**Tech Stack:** React 19, React Router 7 (`BrowserRouter`, `useSearchParams`, `useLocation`, `useNavigate`), Vitest + jsdom.

**Spec:** `docs/superpowers/specs/2026-09-25-documents-design.md`, section "Ticket 0: Back and Forward".

## Global constraints

See [README.md](README.md#global-constraints). Web tests mock the API and never reach a server; this ticket touches no API at all.

## Review Focus

- A filter change while the editor is open replaces the history entry: it must keep the overlay depth, or the next close would replace instead of pop and leave a stray entry. Pinned in Task 3.
- Back from a dirty ticket B to the previous ticket A must not silently drop B's edits: B stays on screen with the discard question. Pinned in Task 3.
- Cancelling that question puts B back as a new entry, and a later close still empties the history it pushed. Pinned in Task 3.
- An editor opened straight from a link (no entry pushed) that then follows a link and closes must end on the plain view, not on the linked ticket. Pinned in Task 3.
- Forward after Back reopens the ticket. Pinned in Task 3.

---

### Task 1: Overlay history state helpers

**Files:**
- Create: `web/src/lib/overlayHistory.ts`
- Test: `web/src/lib/overlayHistory.test.ts`
- Modify: `web/src/lib/latestSearch.ts` (add `latestLocationState`)

**Interfaces:**
- Produces:
  - `OVERLAY_PARAMS: readonly string[]` = `["ticket", "doc", "epic"]`
  - `interface OverlayState { depth: number; fromLink: boolean }`
  - `overlayState(state: unknown): OverlayState`
  - `pushState(current: unknown, baseHasOverlay: boolean): Record<string, unknown>`
  - `hasOverlay(params: URLSearchParams): boolean`
  - `withoutOverlays(params: URLSearchParams): URLSearchParams`
  - `latestLocationState(fallback: unknown): unknown` (in `latestSearch.ts`)

- [ ] **Step 1: Write the failing test**

```ts
// web/src/lib/overlayHistory.test.ts
import { describe, expect, it } from "vitest";
import { hasOverlay, overlayState, pushState, withoutOverlays } from "./overlayHistory";

describe("overlayState", () => {
  it("reads depth and fromLink from location state", () => {
    expect(overlayState({ overlayDepth: 2, overlayFromLink: true })).toEqual({ depth: 2, fromLink: true });
  });

  it("treats missing or malformed state as the plain view", () => {
    expect(overlayState(null)).toEqual({ depth: 0, fromLink: false });
    expect(overlayState(undefined)).toEqual({ depth: 0, fromLink: false });
    expect(overlayState({ overlayDepth: "2" })).toEqual({ depth: 0, fromLink: false });
    expect(overlayState({ overlayDepth: -1 })).toEqual({ depth: 0, fromLink: false });
  });
});

describe("pushState", () => {
  it("starts at depth 1 and records whether the base entry already had an overlay", () => {
    expect(pushState(null, false)).toEqual({ overlayDepth: 1, overlayFromLink: false });
    expect(pushState(null, true)).toEqual({ overlayDepth: 1, overlayFromLink: true });
  });

  it("adds one and carries fromLink from the entry below", () => {
    expect(pushState({ overlayDepth: 1, overlayFromLink: true }, false)).toEqual({
      overlayDepth: 2,
      overlayFromLink: true,
    });
  });

  it("keeps other state fields", () => {
    expect(pushState({ other: 1 }, false)).toEqual({ other: 1, overlayDepth: 1, overlayFromLink: false });
  });
});

describe("overlay params", () => {
  it("knows which parameters are overlays", () => {
    expect(hasOverlay(new URLSearchParams("status=todo"))).toBe(false);
    expect(hasOverlay(new URLSearchParams("ticket=ACP-7"))).toBe(true);
    expect(hasOverlay(new URLSearchParams("epic=Launch"))).toBe(true);
  });

  it("drops every overlay and keeps the rest in order", () => {
    const next = withoutOverlays(new URLSearchParams("project=ACP&ticket=ACP-7&doc=Plan.md&status=todo"));
    expect(next.toString()).toBe("project=ACP&status=todo");
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/lib/overlayHistory.test.ts`
Expected: FAIL, cannot resolve `./overlayHistory`.

- [ ] **Step 3: Write the implementation**

```ts
// web/src/lib/overlayHistory.ts
// Overlays are what opens on top of a view and lives in its query string: the
// ticket editor (`ticket`), a document (`doc`) and the epic modal (`epic`).
// Each overlay the user opens is its own browser history entry, so Back and
// Forward step through them. How many such entries sit on top of the view the
// overlays opened from is kept in the entry's location state, which the
// browser restores with the entry: Back from depth 2 lands on an entry that
// still says 1. Closing everything goes back that many entries at once, which
// leaves history as it was before the first overlay opened.
//
// `fromLink` records whether the entry under the first push already had an
// overlay, which is how an editor opened straight from a link looks. Going
// back to it lands on that linked overlay, so the caller strips it after.

export const OVERLAY_PARAMS: readonly string[] = ["ticket", "doc", "epic"];

export interface OverlayState {
  depth: number;
  fromLink: boolean;
}

export function overlayState(state: unknown): OverlayState {
  const s = (state ?? {}) as { overlayDepth?: unknown; overlayFromLink?: unknown };
  const depth = typeof s.overlayDepth === "number" && Number.isInteger(s.overlayDepth) && s.overlayDepth > 0
    ? s.overlayDepth
    : 0;
  return { depth, fromLink: depth > 0 && s.overlayFromLink === true };
}

/** The location state for an entry pushed on top of one whose state is `current`. */
export function pushState(current: unknown, baseHasOverlay: boolean): Record<string, unknown> {
  const below = overlayState(current);
  const rest = current !== null && typeof current === "object" ? (current as Record<string, unknown>) : {};
  return {
    ...rest,
    overlayDepth: below.depth + 1,
    overlayFromLink: below.depth === 0 ? baseHasOverlay : below.fromLink,
  };
}

export function hasOverlay(params: URLSearchParams): boolean {
  return OVERLAY_PARAMS.some((key) => params.has(key));
}

export function withoutOverlays(params: URLSearchParams): URLSearchParams {
  const next = new URLSearchParams(params);
  for (const key of OVERLAY_PARAMS) next.delete(key);
  return next;
}
```

Append to `web/src/lib/latestSearch.ts`:

```ts
// The location state of the current history entry, read from window.history
// for the same reason latestSearchParams reads window.location: the router's
// own `location.state` can lag a transition behind. BrowserRouter keeps the
// state it was given under `usr`.
export function latestLocationState(fallback: unknown): unknown {
  if (typeof window === "undefined") return fallback;
  const entry = window.history.state as { usr?: unknown } | null;
  return entry?.usr;
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd web && npx vitest run src/lib/overlayHistory.test.ts`
Expected: PASS, 7 tests.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/overlayHistory.ts web/src/lib/overlayHistory.test.ts web/src/lib/latestSearch.ts
git commit -m "feat: add overlay history state helpers

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: `useOverlayHistory`, and filter replaces keep the overlay state

**Files:**
- Create: `web/src/hooks/useOverlayHistory.ts`
- Modify: `web/src/hooks/useFilters.ts:38-53` (pass `state` on every replace)
- Modify: `web/src/hooks/useUnmatched.ts:26-29` (same)

**Interfaces:**
- Consumes: Task 1 helpers.
- Produces:
  ```ts
  interface OverlayHistory {
    depth: number;                                  // entries on top of the base view
    push: (next: URLSearchParams) => void;          // new entry, depth + 1
    replace: (next: URLSearchParams) => void;       // same entry, depth kept
    closeOne: (without: URLSearchParams) => void;   // back one entry, or replace with `without` at depth 0
    closeAll: () => void;                           // back to the base view, overlays stripped
  }
  function useOverlayHistory(): OverlayHistory
  ```

This hook is exercised through `useTicketParam` in Task 3; it has no test of its own.

- [ ] **Step 1: Write the hook**

```ts
// web/src/hooks/useOverlayHistory.ts
import { useCallback, useEffect, useMemo, useRef } from "react";
import { useLocation, useNavigate, useSearchParams } from "react-router-dom";
import { latestLocationState, latestSearchParams } from "../lib/latestSearch";
import { hasOverlay, overlayState, pushState, withoutOverlays } from "../lib/overlayHistory";

export interface OverlayHistory {
  /** How many overlay entries sit on top of the view they opened from. */
  depth: number;
  /** Open an overlay as a new history entry. */
  push: (next: URLSearchParams) => void;
  /** Change the current entry's URL without adding one, keeping its depth. */
  replace: (next: URLSearchParams) => void;
  /**
   * Close the top overlay: one Back when it was pushed, otherwise (it came
   * from a link) replace the entry with `without`.
   */
  closeOne: (without: URLSearchParams) => void;
  /** Close every overlay and return to the view they opened from. */
  closeAll: () => void;
}

/**
 * History for the overlays in the query string (see lib/overlayHistory).
 * Every change starts from the latest URL and state, not the rendered ones,
 * because React Router renders navigations in a transition.
 */
export function useOverlayHistory(): OverlayHistory {
  const [params, setParams] = useSearchParams();
  const location = useLocation();
  const navigate = useNavigate();
  const depth = overlayState(location.state).depth;

  // Set by closeAll when going back lands on an entry that still has an
  // overlay (the editor was opened from a link); cleared once it is stripped.
  const stripRef = useRef(false);
  useEffect(() => {
    if (!stripRef.current || depth !== 0) return;
    stripRef.current = false;
    const latest = latestSearchParams(params);
    if (hasOverlay(latest)) {
      setParams(withoutOverlays(latest), { replace: true, state: latestLocationState(location.state) });
    }
  }, [location.key, location.state, depth, params, setParams]);

  const push = useCallback(
    (next: URLSearchParams) => {
      const baseHasOverlay = hasOverlay(latestSearchParams(params));
      setParams(next, { state: pushState(latestLocationState(location.state), baseHasOverlay) });
    },
    [params, setParams, location.state],
  );

  const replace = useCallback(
    (next: URLSearchParams) => setParams(next, { replace: true, state: latestLocationState(location.state) }),
    [setParams, location.state],
  );

  const closeOne = useCallback(
    (without: URLSearchParams) => {
      if (overlayState(latestLocationState(location.state)).depth > 0) navigate(-1);
      else replace(without);
    },
    [navigate, replace, location.state],
  );

  const closeAll = useCallback(() => {
    const now = overlayState(latestLocationState(location.state));
    if (now.depth === 0) {
      replace(withoutOverlays(latestSearchParams(params)));
      return;
    }
    stripRef.current = now.fromLink;
    navigate(-now.depth);
  }, [navigate, replace, params, location.state]);

  return useMemo(() => ({ depth, push, replace, closeOne, closeAll }), [depth, push, replace, closeOne, closeAll]);
}
```

- [ ] **Step 2: Keep the overlay state when filters replace the entry**

In `web/src/hooks/useFilters.ts`, import `latestLocationState` from `../lib/latestSearch` and pass the current state on each of the three `setParams(..., { replace: true })` calls:

```ts
  const setFilter = useCallback(
    (key: FilterKey, value: string) =>
      setParams(withFilter(latestSearchParams(params), key, value), {
        replace: true,
        state: latestLocationState(undefined),
      }),
    [params, setParams],
  );
  const dropFilters = useCallback(
    (keys: readonly FilterKey[]) => {
      if (keys.length === 0) return;
      setParams(withoutFilters(latestSearchParams(params), keys), {
        replace: true,
        state: latestLocationState(undefined),
      });
    },
    [params, setParams],
  );
  const clearFilters = useCallback(
    () =>
      setParams(withoutFilters(latestSearchParams(params), NARROWING_KEYS), {
        replace: true,
        state: latestLocationState(undefined),
      }),
    [params, setParams],
  );
```

Add one line to the comment above the hook: `// The entry's location state (the overlay depth, see lib/overlayHistory) is kept.`

In `web/src/hooks/useUnmatched.ts`, do the same for both `setParams(withUnmatched(...), { replace: true })` calls: add `state: latestLocationState(undefined)`.

- [ ] **Step 3: Type-check**

Run: `cd web && npx tsc -b`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/hooks/useOverlayHistory.ts web/src/hooks/useFilters.ts web/src/hooks/useUnmatched.ts
git commit -m "feat: add useOverlayHistory and keep overlay state across filter changes

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: `useTicketParam` pushes an entry per ticket

**Files:**
- Modify: `web/src/hooks/useTicketParam.ts` (whole hook body; the doc comment's "History" paragraph)
- Modify: `web/src/lib/ticketParam.ts` (`withTicket` also drops `doc`)
- Test: `web/src/hooks/useTicketParam.dom.test.tsx`

**Interfaces:**
- Consumes: `useOverlayHistory()` from Task 2.
- Produces: `TicketParamState<T>` keeps its exact shape (`selected`, `closeRequested`, `open`, `switchTo`, `close`, `cancelClose`, `onDirtyChange`, `url`). `closeRequested` now also means "the URL moved to a different ticket while this one has unsaved edits".

- [ ] **Step 1: Write the failing tests**

In `web/src/hooks/useTicketParam.dom.test.tsx`, replace the test `"switches to another ticket by id, keeping the filters and the history entry"` with the tests below, and add the rest inside the same `describe`:

```tsx
  it("switches to another ticket as a new entry, and one close leaves every ticket entry", async () => {
    await mount("/kanban?status=todo");
    await act(async () => navigate("/table"));
    await act(async () => state.open(tickets[0]));
    await act(async () => state.switchTo("01BBB"));
    expect(url()).toBe("/table?ticket=ACP-25");
    expect(state.selected?.id).toBe("01BBB");

    await act(async () => {
      state.close();
      await settle();
    });
    expect(url()).toBe("/table");
    expect(state.selected).toBeNull();
    // Both ticket entries went with the close, so Back leaves the view.
    await act(async () => {
      window.history.back();
      await settle();
    });
    expect(url()).toBe("/kanban?status=todo");
  });

  it("steps back through the tickets followed, and forward again", async () => {
    await mount("/kanban");
    await act(async () => state.open(tickets[0]));
    await act(async () => state.switchTo("01BBB"));

    await act(async () => {
      window.history.back();
      await settle();
    });
    expect(url()).toBe("/kanban?ticket=ACP-7");
    expect(state.selected?.id).toBe("01AAA");

    await act(async () => {
      window.history.back();
      await settle();
    });
    expect(url()).toBe("/kanban");
    expect(state.selected).toBeNull();

    await act(async () => {
      window.history.forward();
      await settle();
    });
    expect(url()).toBe("/kanban?ticket=ACP-7");
    expect(state.selected?.id).toBe("01AAA");
  });

  it("holds a dirty ticket when Back lands on the previous one, and shows that one on discard", async () => {
    await mount("/kanban");
    await act(async () => state.open(tickets[0]));
    await act(async () => state.switchTo("01BBB"));
    await act(async () => state.onDirtyChange(true));

    await act(async () => {
      window.history.back();
      await settle();
    });
    expect(url()).toBe("/kanban?ticket=ACP-7");
    expect(state.selected?.id).toBe("01BBB");
    expect(state.closeRequested).toBe(true);

    // Discarding goes where Back asked to go: ticket A, not the board.
    await act(async () => {
      state.close();
      await settle();
    });
    expect(url()).toBe("/kanban?ticket=ACP-7");
    expect(state.selected?.id).toBe("01AAA");
    expect(state.closeRequested).toBe(false);
  });

  it("puts the dirty ticket back when that Back is cancelled, and a close still empties history", async () => {
    await mount("/kanban");
    await act(async () => navigate("/table"));
    await act(async () => state.open(tickets[0]));
    await act(async () => state.switchTo("01BBB"));
    await act(async () => state.onDirtyChange(true));
    await act(async () => {
      window.history.back();
      await settle();
    });

    await act(async () => state.cancelClose());
    expect(url()).toBe("/table?ticket=ACP-25");
    expect(state.selected?.id).toBe("01BBB");
    expect(state.closeRequested).toBe(false);

    await act(async () => {
      state.close();
      await settle();
    });
    expect(url()).toBe("/table");
    await act(async () => {
      window.history.back();
      await settle();
    });
    expect(url()).toBe("/kanban");
  });

  it("follows a link from a ticket opened straight from a link, and closes to the plain view", async () => {
    await mount("/table?project=ACP&ticket=ACP-7");
    await act(async () => state.switchTo("01BBB"));
    expect(url()).toBe("/table?project=ACP&ticket=ACP-25");
    await act(async () => {
      state.close();
      await settle();
    });
    expect(url()).toBe("/table?project=ACP");
    expect(state.selected).toBeNull();
  });

  it("still closes by popping after a filter changed while the editor was open", async () => {
    await mount("/kanban");
    await act(async () => navigate("/table"));
    await act(async () => state.open(tickets[0]));
    await act(async () => filters.setFilter("status", "todo"));
    expect(url()).toBe("/table?ticket=ACP-7&status=todo");

    await act(async () => {
      state.close();
      await settle();
    });
    // Popped, not replaced: the next Back leaves the view.
    expect(url()).toBe("/table");
    await act(async () => {
      window.history.back();
      await settle();
    });
    expect(url()).toBe("/kanban");
  });
```

Also update `"switches from a ticket opened straight from a link, and closes in place"`: its expectations stay the same (`/table`), and it now goes through the strip path. Keep it.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/hooks/useTicketParam.dom.test.tsx`
Expected: the new tests FAIL (Back from ACP-25 goes to `/kanban`, the filter test ends on `/table?status=todo`, and so on). Existing tests pass.

- [ ] **Step 3: Drop `doc` with the ticket**

In `web/src/lib/ticketParam.ts`, change `withTicket` so a document never outlives its ticket:

```ts
/**
 * The params with the ticket parameter set to a ticket's display key, and no
 * open document, since a document belongs to the ticket it was opened on.
 * Every other parameter, the filters included, is kept as it was.
 */
export function withTicket(params: URLSearchParams, ticket: TicketIdentity): URLSearchParams {
  const next = new URLSearchParams(params);
  next.set(TICKET_PARAM, ticketRefFor(ticket));
  next.delete("doc");
  return next;
}
```

- [ ] **Step 4: Rewrite the hook body**

In `web/src/hooks/useTicketParam.ts`:

Replace the imports with:

```ts
import { useCallback, useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useOverlayHistory } from "./useOverlayHistory";
import { latestSearchParams } from "../lib/latestSearch";
import {
  findTicket,
  ticketRef,
  ticketUrl,
  withTicket,
  withoutTicket,
  type TicketIdentity,
} from "../lib/ticketParam";
```

Replace the "History:" and "Switching…" paragraphs of the doc comment with:

```ts
 * History: every ticket opened, from a view or from a link inside the editor,
 * is its own entry (see useOverlayHistory), so Back returns to the previous
 * ticket and Forward comes back. Closing the editor goes back past every
 * entry it pushed, so opening and closing leaves history as it was. An editor
 * opened straight from a link pushed nothing; closing it replaces instead.
 *
 * A Back that lands on another ticket while this one has unsaved edits keeps
 * this one on screen with `closeRequested` set, the same as a Back that drops
 * the parameter: the editor asks, then close() shows the ticket Back landed
 * on, or cancelClose() puts this one back as a new entry.
```

Replace the function body from `const [params, setParams] = useSearchParams();` down to (and including) the `cancelClose` callback with:

```ts
  const [params] = useSearchParams();
  const history = useOverlayHistory();
  const ref = ticketRef(params);
  const fromUrl = useMemo(() => findTicket(tickets ?? [], ref) ?? null, [tickets, ref]);

  // Whether the editor holds unsaved edits, as it reports them.
  const [dirty, setDirty] = useState(false);

  // The ticket the editor shows. It follows the URL, except that a ticket
  // with unsaved edits is kept when the URL stops naming it (Back, or Back to
  // another ticket), so the editor can ask first. Derived during render so a
  // close with nothing to lose unmounts the editor in the same commit.
  const [held, setHeld] = useState<T | null>(null);
  if (fromUrl) {
    if (held?.id !== fromUrl.id && !(held && dirty)) setHeld(fromUrl);
  } else if (held && !dirty) {
    setHeld(null);
  }

  // A held ticket the user chose to keep editing although it is gone.
  const [keptGone, setKeptGone] = useState<string | null>(null);

  // The URL's copy when it names the held ticket, so a refetch's newer
  // version reaches the editor; the held copy when the URL has moved on.
  const selected = held && fromUrl?.id !== held.id ? held : (fromUrl ?? held);
  const closeRequested = held !== null && fromUrl?.id !== held.id && keptGone !== held.id;

  // A reference that resolved here and then stopped naming a loaded ticket
  // names a deleted one; a key that never named anything is left alone.
  const [resolvedRef, setResolvedRef] = useState("");
  if (fromUrl && resolvedRef !== ref) setResolvedRef(ref);
  const vanished = ref !== "" && fromUrl === null && resolvedRef === ref;

  useEffect(() => {
    // Replaces rather than pops: the ticket went without the user asking.
    if (vanished) history.replace(withoutTicket(latestSearchParams(params)));
  }, [vanished, params, history]);

  const gone = held !== null && tickets != null && !tickets.some((t) => t.id === held.id);

  const open = useCallback(
    (ticket: TicketIdentity) => {
      setDirty(false);
      setKeptGone(null);
      history.push(withTicket(latestSearchParams(params), ticket));
    },
    [params, history],
  );

  const switchTo = useCallback(
    (id: string) => {
      const ticket = tickets?.find((t) => t.id === id);
      if (!ticket) return;
      setDirty(false);
      setKeptGone(null);
      history.push(withTicket(latestSearchParams(params), ticket));
    },
    [tickets, params, history],
  );

  const close = useCallback(() => {
    const holding = held;
    setDirty(false);
    setKeptGone(null);
    setHeld(null);
    const current = ticketRef(latestSearchParams(params));
    // Back already dropped the parameter, or moved it to another ticket: the
    // URL is already where the user asked to go.
    if (current === "") return;
    if (holding && !findTicket([holding], current)) return;
    history.closeAll();
  }, [held, params, history]);

  const cancelClose = useCallback(() => {
    if (!held) return;
    // Nothing to put back for a deleted ticket: a parameter naming it would
    // only be dropped again. The editor stays open on the edits instead.
    if (gone) {
      setKeptGone(held.id);
      return;
    }
    history.push(withTicket(latestSearchParams(params), held));
  }, [held, gone, params, history]);
```

Leave the `url` memo and the `return` as they are.

- [ ] **Step 5: Run the hook tests**

Run: `cd web && npx vitest run src/hooks/useTicketParam.dom.test.tsx`
Expected: PASS, every test including the six new ones.

- [ ] **Step 6: Run the whole web suite**

Run: `cd web && npm test`
Expected: PASS. The page tests (`viewState.dom.test.tsx`, `projectScope.dom.test.tsx`, `newTicket.dom.test.tsx`) exercise the editor through the pages; if one asserts that a link between tickets replaces the entry, update its expectation to the new pushed behaviour and say so in the commit body.

- [ ] **Step 7: Manual check against a throwaway board**

```bash
mkdir -p .tmp && go run ./cmd/taskboard --db ./.tmp/t0.db project create Demo --prefix DEMO
go run ./cmd/taskboard --db ./.tmp/t0.db ticket create --project DEMO --title "First"
go run ./cmd/taskboard --db ./.tmp/t0.db ticket create --project DEMO --title "Second" --depends-on DEMO-1
make dev DEV_DB=./.tmp/t0.db DEV_PORT=3011
```

In the browser pane on `http://localhost:3011/?project=DEMO`: open DEMO-2, click DEMO-1 under Depends on, press Back (DEMO-2 shows), Back (board), Forward (DEMO-2). Then open DEMO-2, follow to DEMO-1, press × (board), press Back (the page before the board). Stop the server by its PID.

- [ ] **Step 8: Commit**

```bash
git add web/src/hooks/useTicketParam.ts web/src/lib/ticketParam.ts web/src/hooks/useTicketParam.dom.test.tsx
git commit -m "feat: make each ticket opened its own history entry

Back now returns to the previous ticket and Forward comes back. Closing
the editor still goes back past every entry it pushed.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

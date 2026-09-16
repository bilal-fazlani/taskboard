// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { act, useEffect } from "react";
import { createRoot, type Root } from "react-dom/client";
import { BrowserRouter, useNavigate, type NavigateFunction } from "react-router-dom";
import { useFilters, type FilterState } from "./useFilters";
import { useTicketParam, type TicketParamState } from "./useTicketParam";
import type { TicketIdentity } from "../lib/ticketParam";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// Under the real BrowserRouter, which renders each navigation in a transition,
// so these cover the path the three views take. No API is reached: the hook
// only reads the URL and the tickets it is handed.
let root: Root;
let container: HTMLDivElement;
let state: TicketParamState<TicketIdentity>;
let filters: FilterState;
let navigate: NavigateFunction;

const tickets: TicketIdentity[] = [
  { id: "01AAA", number: 7, projectPrefix: "ACP" },
  { id: "01BBB", number: 25, projectPrefix: "ACP" },
];

function Harness({
  tickets,
  onCommit,
}: {
  tickets: TicketIdentity[];
  onCommit: (t: TicketParamState<TicketIdentity>, f: FilterState, n: NavigateFunction) => void;
}) {
  const ticketState = useTicketParam(tickets);
  const filterState = useFilters();
  const navigate = useNavigate();
  useEffect(() => onCommit(ticketState, filterState, navigate));
  return null;
}

// Waits for jsdom to apply a history traversal, which takes several turns of
// the event loop.
async function settle() {
  for (let i = 0; i < 30; i++) await new Promise((resolve) => setTimeout(resolve, 5));
}

async function mount(url: string, loaded: TicketIdentity[] = tickets) {
  window.history.replaceState(null, "", url);
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => {
    root.render(
      <BrowserRouter>
        <Harness
          tickets={loaded}
          onCommit={(t, f, n) => {
            state = t;
            filters = f;
            navigate = n;
          }}
        />
      </BrowserRouter>,
    );
  });
}

beforeEach(() => {
  window.history.replaceState(null, "", "/");
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

const url = () => window.location.pathname + decodeURIComponent(window.location.search);

describe("useTicketParam under BrowserRouter", () => {
  it("opens a ticket by putting its key in the current view's URL", async () => {
    await mount("/kanban");
    await act(async () => state.open(tickets[0]));
    expect(url()).toBe("/kanban?ticket=ACP-7");
    expect(state.selected?.id).toBe("01AAA");
  });

  it("keeps the filters when opening and closing", async () => {
    await mount("/table?project=ACP&status=todo&q=url");
    await act(async () => state.open(tickets[1]));
    expect(url()).toBe("/table?project=ACP&status=todo&q=url&ticket=ACP-25");
    await act(async () => {
      state.close();
      await settle();
    });
    expect(url()).toBe("/table?project=ACP&status=todo&q=url");
    expect(state.selected).toBeNull();
    expect(filters.filters).toMatchObject({ project: "ACP", status: "todo", q: "url" });
  });

  it("opens the ticket the URL names on load, by key or by id", async () => {
    await mount("/?ticket=acp-25");
    expect(state.selected?.id).toBe("01BBB");
    await act(() => root.unmount());
    container.remove();
    await mount("/kanban?ticket=01AAA");
    expect(state.selected?.id).toBe("01AAA");
  });

  it("opens nothing, and keeps the parameter, for a key no loaded ticket carries", async () => {
    await mount("/?ticket=ACP-999");
    expect(state.selected).toBeNull();
    expect(url()).toBe("/?ticket=ACP-999");
  });

  it("opens nothing until the tickets have loaded", async () => {
    await mount("/?ticket=ACP-7", []);
    expect(state.selected).toBeNull();
    expect(url()).toBe("/?ticket=ACP-7");
  });

  it("does not undo a filter change that has not rendered yet", async () => {
    await mount("/kanban");
    const before = state;
    await act(async () => {
      filters.setFilter("status", "todo");
      before.open(tickets[0]);
    });
    expect(url()).toBe("/kanban?status=todo&ticket=ACP-7");
  });

  it("closes a ticket that was opened straight from a link", async () => {
    await mount("/?ticket=ACP-7");
    expect(state.selected?.id).toBe("01AAA");
    await act(async () => {
      state.close();
      await settle();
    });
    expect(url()).toBe("/");
    expect(state.selected).toBeNull();
  });

  it("lets Back close a clean editor without reopening it", async () => {
    await mount("/kanban?status=todo");
    await act(async () => state.open(tickets[0]));
    expect(url()).toBe("/kanban?status=todo&ticket=ACP-7");
    // Opening pushed an entry, so going back lands on the unopened view. With
    // no unsaved edits the editor is dropped in the same commit: it is never
    // left mounted asking a question nobody needs to answer.
    await act(async () => {
      window.history.back();
      await settle();
    });
    expect(url()).toBe("/kanban?status=todo");
    expect(state.selected).toBeNull();
    expect(state.closeRequested).toBe(false);
  });

  it("keeps a dirty editor mounted when Back drops the parameter, and lets it close", async () => {
    await mount("/kanban");
    await act(async () => state.open(tickets[0]));
    await act(async () => state.onDirtyChange(true));

    await act(async () => {
      window.history.back();
      await settle();
    });
    // The URL has moved on, but the editor is still mounted and told to ask.
    expect(url()).toBe("/kanban");
    expect(state.selected?.id).toBe("01AAA");
    expect(state.closeRequested).toBe(true);

    // Discarding the edits closes it, and the parameter is already gone.
    await act(async () => state.close());
    expect(state.selected).toBeNull();
    expect(state.closeRequested).toBe(false);
    expect(url()).toBe("/kanban");
  });

  it("puts the parameter back when a dirty editor cancels a Back", async () => {
    await mount("/table?status=todo");
    await act(async () => state.open(tickets[1]));
    await act(async () => state.onDirtyChange(true));

    await act(async () => {
      window.history.back();
      await settle();
    });
    expect(state.closeRequested).toBe(true);

    await act(async () => state.cancelClose());
    expect(url()).toBe("/table?status=todo&ticket=ACP-25");
    expect(state.selected?.id).toBe("01BBB");
    expect(state.closeRequested).toBe(false);

    // Pressing Back again asks again rather than letting the edits go.
    await act(async () => {
      window.history.back();
      await settle();
    });
    expect(url()).toBe("/table?status=todo");
    expect(state.selected?.id).toBe("01BBB");
    expect(state.closeRequested).toBe(true);

    await act(async () => state.cancelClose());
    expect(url()).toBe("/table?status=todo&ticket=ACP-25");

    // The cancel pushed the entry back, so closing pops it and the history is
    // no longer than it was.
    await act(async () => {
      state.close();
      await settle();
    });
    expect(url()).toBe("/table?status=todo");
    expect(state.selected).toBeNull();
  });

  it("closes by popping the entry that opening pushed", async () => {
    await mount("/kanban?status=todo");
    // A view visited before the ticket, so there is somewhere for Back to go
    // that a duplicate entry would hide.
    await act(async () => navigate("/table"));

    await act(async () => state.open(tickets[0]));
    expect(url()).toBe("/table?ticket=ACP-7");
    await act(async () => {
      state.close();
      await settle();
    });
    expect(url()).toBe("/table");
    expect(state.selected).toBeNull();

    // Opening and closing together left the history as it was, so the first
    // Back leaves the view rather than being swallowed by an entry the close
    // left behind.
    await act(async () => {
      window.history.back();
      await settle();
    });
    expect(url()).toBe("/kanban?status=todo");
  });

  it("replaces rather than pops when the editor was opened straight from a link", async () => {
    await mount("/table?ticket=ACP-25");
    expect(state.selected?.id).toBe("01BBB");

    await act(async () => {
      state.close();
      await settle();
    });
    // Nothing was pushed, so nothing is popped: closing stays on the view the
    // link landed on rather than jumping back to whatever preceded it.
    expect(url()).toBe("/table");
    expect(state.selected).toBeNull();
  });
});

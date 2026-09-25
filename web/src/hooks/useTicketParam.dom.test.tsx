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

function tree(loaded: TicketIdentity[]) {
  return (
    <BrowserRouter>
      <Harness
        tickets={loaded}
        onCommit={(t, f, n) => {
          state = t;
          filters = f;
          navigate = n;
        }}
      />
    </BrowserRouter>
  );
}

async function mount(url: string, loaded: TicketIdentity[] = tickets) {
  window.history.replaceState(null, "", url);
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => {
    root.render(tree(loaded));
  });
}

/** What a refetch does: the same view, rendered with the tickets it loaded. */
async function reload(loaded: TicketIdentity[]) {
  await act(async () => {
    root.render(tree(loaded));
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

  it("drops the parameter and closes when the open ticket is deleted", async () => {
    await mount("/kanban?status=todo");
    await act(async () => state.open(tickets[0]));
    expect(url()).toBe("/kanban?status=todo&ticket=ACP-7");

    // A live refresh brings back every ticket but the open one.
    await reload([tickets[1]]);
    expect(url()).toBe("/kanban?status=todo");
    expect(state.selected).toBeNull();
    expect(state.closeRequested).toBe(false);
  });

  it("drops the parameter of a ticket opened straight from a link", async () => {
    await mount("/?project=ACP&ticket=ACP-25");
    expect(state.selected?.id).toBe("01BBB");
    await reload([tickets[0]]);
    expect(url()).toBe("/?project=ACP");
    expect(state.selected).toBeNull();
  });

  it("drops the parameter when the last ticket goes", async () => {
    await mount("/table?ticket=ACP-7");
    await reload([]);
    expect(url()).toBe("/table");
    expect(state.selected).toBeNull();
  });

  it("keeps a parameter that never named a loaded ticket", async () => {
    // A hand-typed or stale key opens nothing and is left alone: nothing was
    // deleted, the tickets simply do not carry it.
    await mount("/?ticket=ACP-999");
    await reload(tickets);
    expect(url()).toBe("/?ticket=ACP-999");
  });

  it("keeps the parameter while the tickets are still loading", async () => {
    await mount("/?ticket=ACP-7", []);
    expect(url()).toBe("/?ticket=ACP-7");
    // ...and opens the editor once they arrive.
    await reload(tickets);
    expect(url()).toBe("/?ticket=ACP-7");
    expect(state.selected?.id).toBe("01AAA");
  });

  it("holds a dirty editor open and asks, rather than dropping it with the ticket", async () => {
    await mount("/kanban?ticket=ACP-7");
    await act(async () => state.onDirtyChange(true));
    await reload([tickets[1]]);

    // The parameter is gone, but the editor is still mounted and told to ask.
    expect(url()).toBe("/kanban");
    expect(state.selected?.id).toBe("01AAA");
    expect(state.closeRequested).toBe(true);

    // Keeping the edits leaves the editor open with the ticket gone: there is
    // no parameter worth putting back, so the question is not asked again.
    await act(async () => state.cancelClose());
    expect(url()).toBe("/kanban");
    expect(state.closeRequested).toBe(false);
    expect(state.selected?.id).toBe("01AAA");
    await reload([tickets[1]]);
    expect(url()).toBe("/kanban");
    expect(state.closeRequested).toBe(false);
    expect(state.selected?.id).toBe("01AAA");

    // Letting them go closes it for good.
    await act(async () => state.close());
    expect(state.selected).toBeNull();
    expect(url()).toBe("/kanban");
  });

  it("holds the ticket for as long as the editor has unsaved edits", async () => {
    // What a failed save looks like from here: the editor stayed dirty, so
    // the ticket it is open on must not be let go, deleted or not.
    await mount("/kanban?ticket=ACP-7");
    await act(async () => state.onDirtyChange(true));
    await reload([tickets[1]]);
    expect(state.selected?.id).toBe("01AAA");

    await act(async () => state.cancelClose());
    await reload([tickets[1]]);
    expect(state.selected?.id).toBe("01AAA");

    // Only once the edits are gone does the editor go with them.
    await act(async () => state.onDirtyChange(false));
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

  it("switches from a ticket opened straight from a link, and closes in place", async () => {
    await mount("/table?ticket=ACP-7");
    await act(async () => state.switchTo("01BBB"));
    expect(url()).toBe("/table?ticket=ACP-25");
    await act(async () => {
      state.close();
      await settle();
    });
    expect(url()).toBe("/table");
  });

  it("ignores a switch to a ticket that is not loaded", async () => {
    await mount("/kanban");
    await act(async () => state.open(tickets[0]));
    await act(async () => state.switchTo("01ZZZ"));
    expect(url()).toBe("/kanban?ticket=ACP-7");
    expect(state.selected?.id).toBe("01AAA");
  });
});

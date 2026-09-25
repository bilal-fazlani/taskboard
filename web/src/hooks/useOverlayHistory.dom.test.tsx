// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { act, useEffect } from "react";
import { createRoot, type Root } from "react-dom/client";
import { BrowserRouter, useNavigate, type NavigateFunction } from "react-router-dom";
import { useFilters, type FilterState } from "./useFilters";
import { useOverlayHistory, type OverlayHistory } from "./useOverlayHistory";
import { useUnmatched, type UnmatchedState } from "./useUnmatched";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// Under the real BrowserRouter, as the views run it. The hook is exercised
// through useTicketParam too (useTicketParam.dom.test.tsx); these pin its own
// four moves and that the filter replaces keep the depth it counts on.
let root: Root;
let container: HTMLDivElement;
let overlays: OverlayHistory;
let filters: FilterState;
let unmatched: UnmatchedState;
let navigate: NavigateFunction;

function Harness() {
  const o = useOverlayHistory();
  const f = useFilters();
  const u = useUnmatched();
  const n = useNavigate();
  useEffect(() => {
    overlays = o;
    filters = f;
    unmatched = u;
    navigate = n;
  });
  return null;
}

async function settle() {
  for (let i = 0; i < 30; i++) await new Promise((resolve) => setTimeout(resolve, 5));
}

async function mount(url: string) {
  window.history.replaceState(null, "", url);
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => {
    root.render(
      <BrowserRouter>
        <Harness />
      </BrowserRouter>,
    );
  });
}

async function back() {
  await act(async () => {
    window.history.back();
    await settle();
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
const params = (s: string) => new URLSearchParams(s);

describe("useOverlayHistory under BrowserRouter", () => {
  it("counts each push and keeps the depth on a replace", async () => {
    await mount("/kanban");
    expect(overlays.depth).toBe(0);
    await act(async () => overlays.push(params("ticket=ACP-7")));
    expect(overlays.depth).toBe(1);
    await act(async () => overlays.push(params("ticket=ACP-25")));
    expect(overlays.depth).toBe(2);
    await act(async () => overlays.replace(params("ticket=ACP-25&status=todo")));
    expect(url()).toBe("/kanban?ticket=ACP-25&status=todo");
    expect(overlays.depth).toBe(2);
  });

  it("closeOne goes back one entry when it pushed, and replaces at depth 0", async () => {
    await mount("/kanban");
    await act(async () => overlays.push(params("ticket=ACP-7")));
    await act(async () => overlays.push(params("ticket=ACP-7&doc=Plan.md")));
    await act(async () => {
      overlays.closeOne(params("ticket=ACP-7"));
      await settle();
    });
    expect(url()).toBe("/kanban?ticket=ACP-7");
    expect(overlays.depth).toBe(1);
  });

  it("closeOne replaces with what it is given when nothing was pushed", async () => {
    await mount("/table?ticket=ACP-7&doc=Plan.md");
    expect(overlays.depth).toBe(0);
    await act(async () => {
      overlays.closeOne(params("ticket=ACP-7"));
      await settle();
    });
    expect(url()).toBe("/table?ticket=ACP-7");
  });

  it("closeAll goes back past every pushed entry", async () => {
    await mount("/kanban");
    await act(async () => navigate("/table?status=todo"));
    await act(async () => overlays.push(params("status=todo&ticket=ACP-7")));
    await act(async () => overlays.push(params("status=todo&ticket=ACP-25")));
    await act(async () => {
      overlays.closeAll();
      await settle();
    });
    expect(url()).toBe("/table?status=todo");
    expect(overlays.depth).toBe(0);
    await back();
    expect(url()).toBe("/kanban");
  });

  it("closeAll from a linked-in overlay ends on the plain view", async () => {
    await mount("/table?project=ACP&ticket=ACP-7");
    await act(async () => overlays.push(params("project=ACP&ticket=ACP-25")));
    await act(async () => {
      overlays.closeAll();
      await settle();
    });
    expect(url()).toBe("/table?project=ACP");
    expect(overlays.depth).toBe(0);
  });

  it("closeAll with nothing pushed strips the overlays in place", async () => {
    await mount("/table?project=ACP&ticket=ACP-7&doc=Plan.md");
    await act(async () => {
      overlays.closeAll();
      await settle();
    });
    expect(url()).toBe("/table?project=ACP");
  });

  it("keeps the depth through filter changes", async () => {
    await mount("/kanban");
    await act(async () => overlays.push(params("ticket=ACP-7")));
    await act(async () => filters.setFilter("status", ["todo"]));
    expect(overlays.depth).toBe(1);
    await act(async () => filters.dropFilters(["status"]));
    expect(overlays.depth).toBe(1);
    await act(async () => filters.setFilter("q", "x"));
    await act(async () => filters.clearFilters());
    expect(url()).toBe("/kanban?ticket=ACP-7");
    expect(overlays.depth).toBe(1);
  });

  it("keeps the depth through an unmatched mode change and its repair", async () => {
    await mount("/");
    await act(async () => overlays.push(params("ticket=ACP-7")));
    await act(async () => unmatched.setMode("hide"));
    expect(url()).toBe("/?ticket=ACP-7&unmatched=hide");
    expect(overlays.depth).toBe(1);

    // An invalid value is put right by a replace, which keeps the depth too.
    await act(async () => overlays.push(params("ticket=ACP-25&unmatched=bogus")));
    expect(overlays.depth).toBe(2);
    await act(async () => settle());
    expect(url()).toBe("/?ticket=ACP-25");
    expect(overlays.depth).toBe(2);
  });
});

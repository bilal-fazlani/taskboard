// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, useEffect } from "react";
import { createRoot, type Root } from "react-dom/client";
import { BrowserRouter, useNavigate, type NavigateFunction } from "react-router-dom";
import FilterPanel from "./FilterPanel";
import { useFilters, type FilterState } from "../hooks/useFilters";

// Web tests never reach a server: the panel only loads labels.
vi.mock("../api/client", () => ({
  api: { labels: { list: () => Promise.resolve([{ id: "1", name: "web", color: "#fff", ticketCount: 0 }]) } },
}));

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// These run under the real BrowserRouter, which renders each navigation in a
// transition, so they cover the path the app takes.
let root: Root;
let container: HTMLDivElement;
let state: FilterState;
let navigate: NavigateFunction;

// Hands the hook's state and the router's navigate to the test after each
// commit.
function Harness({ panel, onCommit }: { panel: boolean; onCommit: (s: FilterState, n: NavigateFunction) => void }) {
  const state = useFilters();
  const navigate = useNavigate();
  useEffect(() => onCommit(state, navigate));
  return panel ? (
    <FilterPanel
      state={state}
      projects={[{ id: "p", name: "Alpha", prefix: "ALP", description: "", icon: "", color: "", status: "", createdAt: "", updatedAt: "" }]}
      repos={[]}
    />
  ) : null;
}

async function mount(url: string, panel = false) {
  window.history.replaceState(null, "", url);
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => {
    root.render(
      <BrowserRouter>
        <Harness
          panel={panel}
          onCommit={(s, n) => {
            state = s;
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

const search = () => decodeURIComponent(window.location.search);
const box = () => container.querySelector<HTMLInputElement>('input[type="search"]')!;
const select = (name: string) => container.querySelector<HTMLSelectElement>(`select[aria-label="${name}"]`)!;

// Types into the box the way a browser does, so React's onChange fires.
function type(value: string) {
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
  setter.call(box(), value);
  box().dispatchEvent(new Event("input", { bubbles: true }));
}

describe("useFilters under BrowserRouter", () => {
  it("keeps both of two changes made before a render", async () => {
    await mount("/kanban?ticket=ACP-7");
    const before = state;
    await act(async () => {
      before.setFilter("status", "todo");
      before.setFilter("label", "web");
    });
    expect(search()).toBe("?ticket=ACP-7&status=todo&label=web");
    expect(state.filters).toMatchObject({ status: "todo", label: "web" });
  });

  it("keeps the ticket and other unknown parameters when setting and removing filters", async () => {
    await mount("/table?ticket=ACP-7&project=ALP&zoom=2");
    await act(async () => state.setFilter("q", "hook"));
    expect(search()).toBe("?ticket=ACP-7&project=ALP&zoom=2&q=hook");
    await act(async () => state.setFilter("project", ""));
    expect(search()).toBe("?ticket=ACP-7&zoom=2&q=hook");
    expect(window.location.pathname).toBe("/table");
  });

  it("clears only the filter parameters, even right after a change", async () => {
    await mount("/?project=ALP&ticket=ACP-7&status=todo&zoom=2");
    const before = state;
    await act(async () => {
      before.setFilter("label", "web");
      before.clearFilters();
    });
    expect(search()).toBe("?ticket=ACP-7&zoom=2");
    expect(state.active).toBe(false);
  });

  it("replaces the history entry rather than adding one", async () => {
    await mount("/kanban");
    const length = window.history.length;
    await act(async () => state.setFilter("status", "done"));
    expect(window.history.length).toBe(length);
  });
});

describe("filter panel search box", () => {
  it("keeps a leading space in the box and out of the URL", async () => {
    await mount("/kanban?ticket=ACP-7", true);
    await act(async () => type(" "));
    expect(box().value).toBe(" ");
    expect(search()).toBe("?ticket=ACP-7");
    await act(async () => type(" da"));
    expect(box().value).toBe(" da");
    expect(new URLSearchParams(window.location.search).get("q")).toBe(" da");
    expect(search()).toBe("?ticket=ACP-7&q=+da");
  });

  it("shows each keystroke at once and writes it to the URL", async () => {
    await mount("/", true);
    await act(async () => {
      type("d");
      type("da");
      type("dab");
    });
    expect(box().value).toBe("dab");
    expect(search()).toBe("?q=dab");
  });

  it("follows the URL when it changes from outside", async () => {
    await mount("/?q=start", true);
    expect(box().value).toBe("start");
    await act(async () => navigate("/?q=pasted&status=todo"));
    expect(box().value).toBe("pasted");
    await act(async () => navigate("/?status=todo"));
    expect(box().value).toBe("");
  });

  it("follows back and forward", async () => {
    await mount("/", true);
    await act(async () => navigate("/?q=one"));
    await act(async () => navigate("/?q=two"));
    expect(box().value).toBe("two");
    await act(async () => {
      const popped = new Promise((resolve) => window.addEventListener("popstate", resolve, { once: true }));
      window.history.back();
      await popped;
    });
    expect(box().value).toBe("one");
    await act(async () => {
      const popped = new Promise((resolve) => window.addEventListener("popstate", resolve, { once: true }));
      window.history.forward();
      await popped;
    });
    expect(box().value).toBe("two");
  });

  it("ignores an older value of its own that renders after a newer one was written", async () => {
    await mount("/", true);
    await act(async () => type("abc"));
    // The router renders q=ab while the URL already says q=abc, as when an
    // earlier keystroke's transition commits after a later one was written.
    await act(async () => {
      navigate("/?q=ab", { replace: true });
      window.history.replaceState(window.history.state, "", "/?q=abc");
    });
    expect(state.filters.q).toBe("ab");
    expect(box().value).toBe("abc");
  });

  it("empties the box and every filter on Clear, keeping other parameters", async () => {
    await mount("/table?ticket=ACP-7&label=web&q=dash", true);
    await act(async () => type("dash "));
    const clear = [...container.querySelectorAll("button")].find((b) => b.textContent?.includes("Clear filters"))!;
    await act(async () => clear.click());
    expect(box().value).toBe("");
    expect(search()).toBe("?ticket=ACP-7");
    expect(container.textContent).not.toContain("Clear filters");
  });
});

describe("filter panel dropdowns", () => {
  it("selects the matching option for a URL value in another case, without a duplicate", async () => {
    await mount("/?label=WEB&project=alp", true);
    expect(select("Label").value).toBe("web");
    expect([...select("Label").options].map((o) => o.value)).toEqual(["", "web"]);
    expect(select("Project").value).toBe("ALP");
    expect([...select("Project").options].map((o) => o.value)).toEqual(["", "ALP"]);
  });

  it("still shows a value that matches no option", async () => {
    await mount("/?label=gone&repo=a%2Fb", true);
    expect(select("Label").value).toBe("gone");
    expect(select("Repo").value).toBe("a/b");
  });
});

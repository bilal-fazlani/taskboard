// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, useEffect } from "react";
import { createRoot, type Root } from "react-dom/client";
import { BrowserRouter, useNavigate, type NavigateFunction } from "react-router-dom";
import FilterPanel from "./FilterPanel";
import { useFilters, type FilterState } from "../hooks/useFilters";
import { LAST_PROJECT_KEY } from "../lib/defaultProject";
import { DEBOUNCE_MS } from "../lib/liveRefresh";
import { memoryStorage } from "../test/memoryStorage";

// Web tests never reach a server: the bar loads the projects and labels it
// offers, and nothing else.
const { labelList, projectList } = vi.hoisted(() => ({ labelList: vi.fn(), projectList: vi.fn() }));
vi.mock("../api/client", () => ({
  api: { labels: { list: labelList }, projects: { list: projectList } },
}));

// jsdom has no EventSource. This stand-in lets one test deliver the `changed`
// event the server would send, so the panel's live reload runs for real.
class FakeEvents {
  static opened: FakeEvents[] = [];
  private readonly listeners = new Map<string, Set<() => void>>();

  constructor() {
    FakeEvents.opened.push(this);
  }

  addEventListener(type: string, listener: () => void) {
    let set = this.listeners.get(type);
    if (!set) this.listeners.set(type, (set = new Set()));
    set.add(listener);
  }

  removeEventListener(type: string, listener: () => void) {
    this.listeners.get(type)?.delete(listener);
  }

  close() {}

  emit(type: string) {
    for (const listener of [...(this.listeners.get(type) ?? [])]) listener();
  }
}

const label = (name: string) => ({ id: name, name, color: "#fff", ticketCount: 0 });
const project = (prefix: string) => ({
  id: prefix,
  name: "Alpha",
  prefix,
  description: "",
  icon: "",
  color: "",
  status: "",
  createdAt: "",
  updatedAt: "",
});

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
  return panel ? <FilterPanel state={state} repos={[]} /> : null;
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
  vi.stubGlobal("localStorage", memoryStorage());
  labelList.mockResolvedValue([label("web")]);
  projectList.mockResolvedValue([project("ALP")]);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  vi.unstubAllGlobals();
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

  it("clears only the filter parameters, keeping the project, even right after a change", async () => {
    await mount("/?project=ALP&ticket=ACP-7&status=todo&zoom=2");
    const before = state;
    await act(async () => {
      before.setFilter("label", "web");
      before.clearFilters();
    });
    expect(search()).toBe("?project=ALP&ticket=ACP-7&zoom=2");
    expect(state.active).toBe(false);
  });

  it("counts only the filters other than the project as active", async () => {
    // Every view always has a project, so that alone filters nothing away.
    await mount("/?project=ALP");
    expect(state.active).toBe(false);
    await act(async () => state.setFilter("q", "hook"));
    expect(state.active).toBe(true);
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
    await mount("/kanban?project=ALP&ticket=ACP-7", true);
    await act(async () => type(" "));
    expect(box().value).toBe(" ");
    expect(search()).toBe("?project=ALP&ticket=ACP-7");
    await act(async () => type(" da"));
    expect(box().value).toBe(" da");
    expect(new URLSearchParams(window.location.search).get("q")).toBe(" da");
    expect(search()).toBe("?project=ALP&ticket=ACP-7&q=+da");
  });

  it("shows each keystroke at once and writes it to the URL", async () => {
    await mount("/?project=ALP", true);
    await act(async () => {
      type("d");
      type("da");
      type("dab");
    });
    expect(box().value).toBe("dab");
    expect(search()).toBe("?project=ALP&q=dab");
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
    await mount("/?project=ALP", true);
    await act(async () => type("abc"));
    // The router renders q=ab while the URL already says q=abc, as when an
    // earlier keystroke's transition commits after a later one was written.
    await act(async () => {
      navigate("/?project=ALP&q=ab", { replace: true });
      window.history.replaceState(window.history.state, "", "/?project=ALP&q=abc");
    });
    expect(state.filters.q).toBe("ab");
    expect(box().value).toBe("abc");
  });

  it("empties the box and every filter but the project on Clear, keeping other parameters", async () => {
    await mount("/table?project=ALP&ticket=ACP-7&label=web&q=dash", true);
    await act(async () => type("dash "));
    const clear = [...container.querySelectorAll("button")].find((b) => b.textContent?.includes("Clear filters"))!;
    await act(async () => clear.click());
    expect(box().value).toBe("");
    expect(search()).toBe("?project=ALP&ticket=ACP-7");
    expect(select("Project").value).toBe("ALP");
    expect(container.textContent).not.toContain("Clear filters");
  });
});

describe("filter panel dropdowns", () => {
  it("selects the matching option for a URL value in another case, without a duplicate", async () => {
    await mount("/?label=WEB&project=alp", true);
    expect(select("Label").value).toBe("web");
    expect([...select("Label").options].map((o) => o.value)).toEqual(["", "web"]);
    expect(select("Project").value).toBe("ALP");
    expect([...select("Project").options].map((o) => o.value)).toEqual(["ALP"]);
  });

  it("still shows a repo that matches no option", async () => {
    // A repo is a string the tickets carry rather than a record that can be
    // deleted, so it is never dropped and has to show whatever the URL says.
    await mount("/?project=ALP&repo=a%2Fb", true);
    expect(select("Repo").value).toBe("a/b");
  });

  it("offers every status, agent_review included, in board column order", async () => {
    await mount("/?project=ALP&status=agent_review", true);
    expect([...select("Status").options].map((o) => o.value)).toEqual([
      "",
      "todo",
      "in_progress",
      "agent_review",
      "done",
    ]);
    expect([...select("Status").options].map((o) => o.textContent)).toContain("Agent Review");
    expect(select("Status").value).toBe("agent_review");
  });
});

// A filter naming a project or label that no longer exists would leave the
// view empty for a reason the user cannot see, so it leaves the URL. Status
// and priority are fixed sets and always stay.
describe("filters that no longer name anything", () => {
  it("drops a label that no longer exists, keeping every other filter", async () => {
    await mount("/table?project=ALP&status=todo&priority=high&label=gone&repo=a%2Fb&q=hook&ticket=ACP-7", true);
    expect(search()).toBe("?project=ALP&status=todo&priority=high&repo=a/b&q=hook&ticket=ACP-7");
    expect(select("Label").value).toBe("");
  });

  it("replaces a project that no longer exists, keeping the label", async () => {
    await mount("/?project=GONE&label=web", true);
    expect(search()).toBe("?project=ALP&label=web");
    expect(select("Project").value).toBe("ALP");
  });

  it("keeps a project and a label that still exist, whatever their case", async () => {
    await mount("/?project=alp&label=WEB", true);
    expect(search()).toBe("?project=alp&label=WEB");
  });

  it("keeps a label until the labels have loaded, and drops it once they have", async () => {
    let load!: (labels: ReturnType<typeof label>[]) => void;
    labelList.mockReturnValue(new Promise((resolve) => (load = resolve)));
    await mount("/?project=ALP&label=web", true);
    expect(search()).toBe("?project=ALP&label=web");
    await act(async () => load([]));
    expect(search()).toBe("?project=ALP");
  });

  it("keeps a label the panel could not load, rather than claiming there are none", async () => {
    labelList.mockRejectedValue(new Error("offline"));
    await mount("/?project=ALP&label=web", true);
    await act(async () => {});
    expect(search()).toBe("?project=ALP&label=web");
  });

  it("keeps a project filter until the projects have loaded", async () => {
    let load!: (projects: ReturnType<typeof project>[]) => void;
    projectList.mockReturnValue(new Promise((resolve) => (load = resolve)));
    await mount("/?project=ALP", true);
    expect(search()).toBe("?project=ALP");
    await act(async () => load([project("ALP")]));
    expect(search()).toBe("?project=ALP");
  });

  it("drops a project filter once the last project is gone", async () => {
    // An empty list that really has loaded is not "nothing has arrived yet":
    // the project the filter names has been deleted, so the filter goes.
    projectList.mockResolvedValue([]);
    await mount("/?project=ALP&status=todo", true);
    expect(search()).toBe("?status=todo");
  });

  it("keeps a project filter the bar could not load, rather than claiming there are none", async () => {
    projectList.mockRejectedValue(new Error("offline"));
    await mount("/?project=ALP", true);
    await act(async () => {});
    expect(search()).toBe("?project=ALP");
  });

  it("drops the filter without adding a history entry", async () => {
    await mount("/?project=ALP&label=gone", true);
    const length = window.history.length;
    expect(search()).toBe("?project=ALP");
    expect(window.history.length).toBe(length);
  });

  it("drops a filter on a live change, with no reload", async () => {
    (globalThis as unknown as { EventSource?: unknown }).EventSource = FakeEvents;
    try {
      await mount("/kanban?project=ALP&label=web&status=todo&zoom=2", true);
      expect(search()).toBe("?project=ALP&label=web&status=todo&zoom=2");
      // The label is deleted elsewhere and the stream says so: the panel
      // reloads its labels and the filter naming the deleted one goes.
      labelList.mockResolvedValue([]);
      await act(async () => {
        FakeEvents.opened[FakeEvents.opened.length - 1].emit("changed");
        await new Promise((resolve) => setTimeout(resolve, DEBOUNCE_MS + 20));
      });
      expect(search()).toBe("?project=ALP&status=todo&zoom=2");
    } finally {
      delete (globalThis as unknown as { EventSource?: unknown }).EventSource;
    }
  });
});

// Every view always shows one project. A URL without one, or naming one that
// was deleted, gets the project last shown on any view, else the first.
describe("the project a URL without one gets", () => {
  const remember = (prefix: string) => globalThis.localStorage.setItem(LAST_PROJECT_KEY, prefix);

  beforeEach(() => {
    projectList.mockResolvedValue([project("ALP"), project("BET"), project("GAM")]);
  });

  it("offers no All projects option", async () => {
    await mount("/?project=BET", true);
    expect([...select("Project").options].map((o) => o.value)).toEqual(["ALP", "BET", "GAM"]);
    expect(container.textContent).not.toMatch(/All projects/i);
  });

  it("is the one last shown, replacing the history entry", async () => {
    remember("GAM");
    window.history.replaceState(null, "", "/kanban?status=todo&ticket=ACP-7");
    const length = window.history.length;
    await mount("/kanban?status=todo&ticket=ACP-7", true);
    expect(search()).toBe("?status=todo&ticket=ACP-7&project=GAM");
    expect(window.history.length).toBe(length);
    expect(select("Project").value).toBe("GAM");
  });

  it("is the first project when none was shown before", async () => {
    await mount("/table", true);
    expect(search()).toBe("?project=ALP");
  });

  it("is the first project when the one last shown was deleted", async () => {
    remember("GONE");
    await mount("/", true);
    expect(search()).toBe("?project=ALP");
  });

  it("is the first project when storage can't be used, and nothing breaks", async () => {
    vi.stubGlobal("localStorage", {
      getItem: () => {
        throw new Error("denied");
      },
      setItem: () => {
        throw new Error("denied");
      },
    });
    await mount("/", true);
    expect(search()).toBe("?project=ALP");
    expect(select("Project").value).toBe("ALP");
  });

  it("replaces a deleted project in the URL with the one last shown", async () => {
    remember("BET");
    await mount("/?project=GONE&status=todo", true);
    expect(search()).toBe("?project=BET&status=todo");
  });

  it("is nothing while the projects load, and then picked", async () => {
    let load!: (projects: ReturnType<typeof project>[]) => void;
    projectList.mockReturnValue(new Promise((resolve) => (load = resolve)));
    await mount("/?status=todo", true);
    expect(search()).toBe("?status=todo");
    await act(async () => load([project("BET")]));
    expect(search()).toBe("?status=todo&project=BET");
  });

  it("is nothing when there are no projects, and the bar says so", async () => {
    projectList.mockResolvedValue([]);
    await mount("/?status=todo", true);
    expect(search()).toBe("?status=todo");
    expect([...select("Project").options].map((o) => o.textContent)).toEqual(["No projects"]);
    expect(select("Project").value).toBe("");
  });

  it("remembers the project shown, as the list spells it, for every view", async () => {
    await mount("/kanban?project=bet", true);
    expect(globalThis.localStorage.getItem(LAST_PROJECT_KEY)).toBe("BET");
    await act(async () => {
      select("Project").value = "GAM";
      select("Project").dispatchEvent(new Event("change", { bubbles: true }));
    });
    expect(search()).toBe("?project=GAM");
    expect(globalThis.localStorage.getItem(LAST_PROJECT_KEY)).toBe("GAM");
  });

  it("stays when Clear removes every other filter", async () => {
    await mount("/?project=BET&status=todo&q=hook", true);
    const clear = [...container.querySelectorAll("button")].find((b) => b.textContent?.includes("Clear filters"))!;
    await act(async () => clear.click());
    expect(search()).toBe("?project=BET");
    expect(select("Project").value).toBe("BET");
    // Only the project is set, which filters nothing away: nothing to clear.
    expect(container.textContent).not.toContain("Clear filters");
  });
});

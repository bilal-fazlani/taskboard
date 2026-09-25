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

// Web tests never reach a server: the bar loads the projects, epics and
// labels it offers, and nothing else.
const { labelList, projectList, epicList } = vi.hoisted(() => ({
  labelList: vi.fn(),
  projectList: vi.fn(),
  epicList: vi.fn(),
}));
vi.mock("../api/client", () => ({
  api: { labels: { list: labelList }, projects: { list: projectList }, epics: { list: epicList } },
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
const project = (prefix: string, name = "Alpha", status = "active") => ({
  id: prefix,
  name,
  prefix,
  description: "",
  icon: "",
  color: "",
  status,
  createdAt: "",
  updatedAt: "",
});
const NO_PROGRESS = { counts: {}, total: 0, complete: false, lastActivityAt: null };
const epicListOf = (names: string[], projectId = "ALP") => ({
  epics: names.map((name) => ({ ...NO_PROGRESS, id: `e-${name}`, projectId, name, createdAt: "", updatedAt: "" })),
  noEpic: NO_PROGRESS,
});
type ActivityTicket = { projectPrefix: string; updatedAt: string };
const touched = (projectPrefix: string, updatedAt: string): ActivityTicket => ({ projectPrefix, updatedAt });

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// These run under the real BrowserRouter, which renders each navigation in a
// transition, so they cover the path the app takes.
let root: Root;
let container: HTMLDivElement;
let state: FilterState;
let navigate: NavigateFunction;

// Hands the hook's state and the router's navigate to the test after each
// commit.
function Harness({
  panel,
  tickets,
  onCommit,
}: {
  panel: boolean;
  tickets: readonly ActivityTicket[] | null;
  onCommit: (s: FilterState, n: NavigateFunction) => void;
}) {
  const state = useFilters();
  const navigate = useNavigate();
  useEffect(() => onCommit(state, navigate));
  return panel ? <FilterPanel state={state} tickets={tickets} repos={[]} /> : null;
}

// The view's tickets, as it hands them to the bar: null while they load.
async function renderPanel(panel: boolean, tickets: readonly ActivityTicket[] | null) {
  await act(async () => {
    root.render(
      <BrowserRouter>
        <Harness
          panel={panel}
          tickets={tickets}
          onCommit={(s, n) => {
            state = s;
            navigate = n;
          }}
        />
      </BrowserRouter>,
    );
  });
}

async function mount(url: string, panel = false, tickets: readonly ActivityTicket[] | null = []) {
  window.history.replaceState(null, "", url);
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  await renderPanel(panel, tickets);
}

beforeEach(() => {
  window.history.replaceState(null, "", "/");
  vi.stubGlobal("localStorage", memoryStorage());
  labelList.mockResolvedValue([label("web")]);
  projectList.mockResolvedValue([project("ALP")]);
  epicList.mockResolvedValue(epicListOf([]));
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  vi.unstubAllGlobals();
});

const search = () => decodeURIComponent(window.location.search);
const box = () => container.querySelector<HTMLInputElement>('input[type="search"]')!;
const select = (name: string) => container.querySelector<HTMLSelectElement>(`select[aria-label="${name}"]`)!;

// A multi-value filter control (FilterMultiSelect): its button, what the
// button reads, and its list's rows once open.
const control = (name: string) => container.querySelector<HTMLElement>(`[role="group"][aria-label="${name}"]`)!;
const trigger = (name: string) => control(name).querySelector<HTMLButtonElement>("button[aria-haspopup]")!;
const reads = (name: string) => trigger(name).querySelector("span")!.textContent;
const rows = (name: string) => [...control(name).querySelectorAll<HTMLElement>('[role="option"]')];
const isOpen = (name: string) => trigger(name).getAttribute("aria-expanded") === "true";
async function openList(name: string) {
  if (!isOpen(name)) await act(async () => trigger(name).click());
}
/** The list's rows, opening it first. */
async function rowLabels(name: string) {
  await openList(name);
  return rows(name).map((r) => r.textContent);
}
/** The ticked rows, opening the list first. */
async function ticked(name: string) {
  await openList(name);
  return rows(name)
    .filter((r) => r.getAttribute("aria-selected") === "true")
    .map((r) => r.textContent);
}
/** Ticks or unticks the row with the given text, opening the list first. */
async function choose(name: string, label: string) {
  await openList(name);
  const row = rows(name).find((r) => r.textContent === label);
  if (!row) throw new Error(`no ${label} in ${name}: ${rows(name).map((r) => r.textContent)}`);
  await act(async () => row.click());
}

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
      before.setFilter("status", ["todo"]);
      before.setFilter("label", ["web", "api"]);
    });
    expect(search()).toBe("?ticket=ACP-7&status=todo&label=web&label=api");
    expect(state.filters).toMatchObject({ status: ["todo"], label: ["web", "api"] });
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
      before.setFilter("label", ["web"]);
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
    await act(async () => state.setFilter("status", ["done"]));
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
  it("ticks the matching option for a URL value in another case, without a duplicate", async () => {
    await mount("/?label=WEB&project=alp", true);
    expect(reads("Label")).toBe("web");
    expect(await rowLabels("Label")).toEqual(["web"]);
    expect(await ticked("Label")).toEqual(["web"]);
    expect(select("Project").value).toBe("ALP");
    expect([...select("Project").options].map((o) => o.value)).toEqual(["ALP"]);
  });

  it("still shows a repo that matches no option", async () => {
    // A repo is a string the tickets carry rather than a record that can be
    // deleted, so it is never dropped and has to show whatever the URL says.
    await mount("/?project=ALP&repo=a%2Fb", true);
    expect(reads("Repo")).toBe("a/b");
    expect(await ticked("Repo")).toEqual(["a/b"]);
  });

  it("offers every status, agent_review included, in board column order", async () => {
    await mount("/?project=ALP&status=agent_review", true);
    expect(await rowLabels("Status")).toEqual(["Todo", "In Progress", "Agent Review", "Done"]);
    expect(await ticked("Status")).toEqual(["Agent Review"]);
    expect(reads("Status")).toBe("Agent Review");
  });
});

describe("multi-value filters", () => {
  const priorities = [["Urgent"], ["Urgent", "High"], ["Urgent", "High", "Low"]];

  it("reads the all label with nothing chosen, as a field that is not set", async () => {
    await mount("/?project=ALP", true);
    for (const [name, all] of [
      ["Epic", "All epics"],
      ["Status", "All statuses"],
      ["Priority", "All priorities"],
      ["Label", "All labels"],
      ["Repo", "All repos"],
    ]) {
      expect(reads(name), name).toBe(all);
      expect(trigger(name).className, name).toContain("border-slate-700");
      expect(trigger(name).title, name).toBe("");
      expect(isOpen(name), name).toBe(false);
    }
  });

  it("writes each choice at once as repeated parameters in option order, keeping the list open", async () => {
    await mount("/kanban?project=ALP&ticket=ACP-7", true);
    await choose("Priority", "Low");
    expect(search()).toBe("?project=ALP&ticket=ACP-7&priority=low");
    await choose("Priority", "Urgent");
    await choose("Priority", "High");
    expect(search()).toBe("?project=ALP&ticket=ACP-7&priority=urgent&priority=high&priority=low");
    expect(isOpen("Priority")).toBe(true);
    expect(state.filters.priority).toEqual(["urgent", "high", "low"]);
    expect(state.active).toBe(true);
    await choose("Priority", "Urgent");
    expect(search()).toBe("?project=ALP&ticket=ACP-7&priority=high&priority=low");
  });

  it.each(priorities)("reads one value alone and several comma-joined with a count: %s", async (...chosen) => {
    const qs = chosen.map((p) => `priority=${p.toLowerCase()}`).join("&");
    await mount(`/?project=ALP&${qs}`, true);
    expect(reads("Priority")).toBe(chosen.join(", "));
    const badge = trigger("Priority").querySelectorAll("span")[1];
    if (chosen.length === 1) expect(badge).toBeUndefined();
    else expect(badge.textContent).toBe(String(chosen.length));
    expect(trigger("Priority").title).toBe(`Priority: ${chosen.join(", ")}`);
    expect(trigger("Priority").className).toContain("border-blue-500/60");
  });

  it("shows the values of a URL in any order in option order", async () => {
    await mount("/?project=ALP&status=done&status=todo", true);
    expect(reads("Status")).toBe("Todo, Done");
  });

  it("clears only its own filter from the footer, which shows how many are chosen", async () => {
    await mount("/?project=ALP&status=todo&status=done&label=web", true);
    await openList("Status");
    const footer = control("Status").querySelector<HTMLElement>(".border-t")!;
    expect(footer.textContent).toContain("2 selected");
    const clear = [...footer.querySelectorAll("button")].find((b) => b.textContent === "Clear")!;
    await act(async () => clear.click());
    expect(search()).toBe("?project=ALP&label=web");
    expect(isOpen("Status")).toBe(true);
    // Nothing left to clear.
    expect(control("Status").querySelector(".border-t")).toBeNull();
  });

  it("is cleared with every other filter by Clear filters", async () => {
    await mount("/?project=ALP&status=todo&status=done&label=web&priority=low", true);
    const clear = [...container.querySelectorAll("button")].find((b) => b.textContent?.includes("Clear filters"))!;
    await act(async () => clear.click());
    expect(search()).toBe("?project=ALP");
    expect(reads("Status")).toBe("All statuses");
  });

  it("closes on Escape, and only the list, giving the button its focus back", async () => {
    await mount("/?project=ALP", true);
    const below = vi.fn();
    const { pushEscape } = await import("../lib/escapeStack");
    const pop = pushEscape(below);
    await openList("Status");
    expect(document.activeElement?.getAttribute("role")).toBe("listbox");
    await act(async () => {
      document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }));
    });
    expect(isOpen("Status")).toBe(false);
    expect(document.activeElement).toBe(trigger("Status"));
    expect(below).not.toHaveBeenCalled();
    // With the list gone, Escape reaches the layer below.
    await act(async () => {
      document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }));
    });
    expect(below).toHaveBeenCalledTimes(1);
    pop();
  });

  it("moves with the arrow keys and ticks with Space or Enter", async () => {
    await mount("/?project=ALP", true);
    await act(async () => {
      trigger("Status").focus();
      trigger("Status").dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true }));
    });
    expect(isOpen("Status")).toBe(true);
    const list = control("Status").querySelector<HTMLElement>('[role="listbox"]')!;
    const key = (k: string) =>
      act(async () => {
        list.dispatchEvent(new KeyboardEvent("keydown", { key: k, bubbles: true }));
      });
    const activeRow = () => document.getElementById(list.getAttribute("aria-activedescendant")!)!.textContent;
    expect(activeRow()).toBe("Todo");
    await key("ArrowDown");
    await key("ArrowDown");
    expect(activeRow()).toBe("Agent Review");
    await key(" ");
    expect(search()).toBe("?project=ALP&status=agent_review");
    await key("End");
    await key("Enter");
    expect(search()).toBe("?project=ALP&status=agent_review&status=done");
    await key("Home");
    await key("ArrowUp");
    expect(activeRow()).toBe("Todo");
  });

  it("closes on a press outside, and when the focus leaves it", async () => {
    await mount("/?project=ALP", true);
    await openList("Label");
    await act(async () => {
      document.body.dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
    });
    expect(isOpen("Label")).toBe(false);
    await openList("Label");
    await act(async () => box().focus());
    expect(isOpen("Label")).toBe(false);
    // A press inside the list keeps it open.
    await openList("Label");
    await act(async () => {
      rows("Label")[0].dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
    });
    expect(isOpen("Label")).toBe(true);
  });

  it("toggles the list from its button", async () => {
    await mount("/?project=ALP", true);
    await act(async () => trigger("Repo").click());
    expect(isOpen("Repo")).toBe(true);
    await act(async () => trigger("Repo").click());
    expect(isOpen("Repo")).toBe(false);
  });
});

// A filter naming a project or label that no longer exists would leave the
// view empty for a reason the user cannot see, so it leaves the URL. Status
// and priority are fixed sets and always stay.
describe("filters that no longer name anything", () => {
  it("drops a label that no longer exists, keeping every other filter", async () => {
    await mount("/table?project=ALP&status=todo&priority=high&label=gone&repo=a%2Fb&q=hook&ticket=ACP-7", true);
    expect(search()).toBe("?project=ALP&status=todo&priority=high&repo=a/b&q=hook&ticket=ACP-7");
    expect(reads("Label")).toBe("All labels");
  });

  it("drops only the labels that no longer exist, keeping the filter's other values", async () => {
    labelList.mockResolvedValue([label("web"), label("api")]);
    await mount("/table?project=ALP&label=gone&label=WEB&status=todo&label=old&label=api&ticket=ACP-7", true);
    expect(search()).toBe("?project=ALP&label=WEB&status=todo&label=api&ticket=ACP-7");
    // In the order the labels are offered.
    expect(reads("Label")).toBe("web, api");
  });

  it("drops a label deleted elsewhere on a live change, keeping the others", async () => {
    (globalThis as unknown as { EventSource?: unknown }).EventSource = FakeEvents;
    try {
      labelList.mockResolvedValue([label("web"), label("api")]);
      await mount("/?project=ALP&label=web&label=api", true);
      expect(search()).toBe("?project=ALP&label=web&label=api");
      labelList.mockResolvedValue([label("web")]);
      await act(async () => {
        FakeEvents.opened[FakeEvents.opened.length - 1].emit("changed");
        await new Promise((resolve) => setTimeout(resolve, DEBOUNCE_MS + 20));
      });
      expect(search()).toBe("?project=ALP&label=web");
    } finally {
      delete (globalThis as unknown as { EventSource?: unknown }).EventSource;
    }
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
    // A value naming no project the bar knows still shows, as written.
    expect([...select("Project").options].map((o) => o.textContent)).toEqual(["ALP"]);
    expect(select("Project").value).toBe("ALP");
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
// was deleted, gets the project last shown on any view if it is still active,
// else the active project whose tickets changed last, else the first active
// one by name.
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

  it("is the first by name when none was shown before and no project has tickets", async () => {
    await mount("/table", true);
    expect(search()).toBe("?project=ALP");
  });

  it("is the first by name when the one last shown was deleted", async () => {
    remember("GONE");
    await mount("/", true);
    expect(search()).toBe("?project=ALP");
  });

  it("is the first by name when storage can't be used, and nothing breaks", async () => {
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

// Archived projects stay out of the way: never picked, and not offered in the
// dropdown, which lists the active ones by name. A link to an archived one
// still opens it.
describe("archived projects and the project order", () => {
  const remember = (prefix: string) => globalThis.localStorage.setItem(LAST_PROJECT_KEY, prefix);
  const options = () => [...select("Project").options].map((o) => o.value);

  // Newest first, as the API lists them.
  beforeEach(() => {
    projectList.mockResolvedValue([
      project("ZED", "zed tools"),
      project("OLD", "Archive me", "archived"),
      project("BET", "Beta"),
      project("ALP", "alpha"),
    ]);
  });

  it("offers only the active projects, by name ignoring case", async () => {
    await mount("/?project=BET", true);
    expect(options()).toEqual(["ALP", "BET", "ZED"]);
    expect([...select("Project").options].map((o) => o.textContent)).toEqual(["alpha", "Beta", "zed tools"]);
  });

  it("keeps an archived project a URL names, as an extra entry, without replacing it", async () => {
    await mount("/kanban?project=OLD&status=todo&ticket=OLD-3", true, [touched("BET", "2026-09-23T10:00:00Z")]);
    expect(search()).toBe("?project=OLD&status=todo&ticket=OLD-3");
    expect(select("Project").value).toBe("OLD");
    expect(options()).toEqual(["ALP", "BET", "ZED", "OLD"]);
  });

  it("labels a project without an icon by its name alone, archived or not", async () => {
    // The API leaves an empty icon out of the JSON altogether.
    const withoutIcon = (p: ReturnType<typeof project>) => {
      const json: Partial<typeof p> = { ...p };
      delete json.icon;
      return json;
    };
    projectList.mockResolvedValue([
      withoutIcon(project("ALP", "alpha")),
      withoutIcon(project("OLD", "Archive me", "archived")),
    ]);
    await mount("/?project=OLD", true);
    expect([...select("Project").options].map((o) => o.textContent)).toEqual(["alpha", "Archive me (archived)"]);
  });

  it("labels the archived project's entry with its icon and name, marked archived, whatever the URL's case", async () => {
    projectList.mockResolvedValue([
      { ...project("OLD", "Archive me", "archived"), icon: "📦" },
      project("ALP", "alpha"),
    ]);
    await mount("/?project=old", true);
    expect(search()).toBe("?project=old");
    expect(select("Project").value).toBe("OLD");
    const labels = [...select("Project").options].map((o) => o.textContent);
    expect(labels).toEqual(["alpha", "📦 Archive me (archived)"]);
    expect(select("Project").selectedOptions[0].textContent).toBe("📦 Archive me (archived)");
  });

  it("drops the archived project from the list again once another is chosen", async () => {
    await mount("/?project=OLD", true);
    await act(async () => {
      select("Project").value = "BET";
      select("Project").dispatchEvent(new Event("change", { bubbles: true }));
    });
    expect(search()).toBe("?project=BET");
    expect(options()).toEqual(["ALP", "BET", "ZED"]);
  });

  it("is the active project whose tickets changed last, with nothing remembered", async () => {
    await mount("/table", true, [
      touched("ALP", "2026-09-20T10:00:00Z"),
      touched("ZED", "2026-09-22T10:00:00Z"),
      touched("OLD", "2026-09-23T10:00:00Z"),
    ]);
    expect(search()).toBe("?project=ZED");
  });

  it("passes over a remembered project that has since been archived", async () => {
    remember("OLD");
    await mount("/", true, [touched("BET", "2026-09-22T10:00:00Z")]);
    expect(search()).toBe("?project=BET");
  });

  it("is the first active project by name when none has tickets", async () => {
    // Not ZED, the newest, and not OLD, first by name but archived.
    await mount("/", true);
    expect(search()).toBe("?project=ALP");
  });

  it("waits for the tickets before picking, then picks from them", async () => {
    await mount("/?status=todo", true, null);
    await act(async () => {});
    expect(search()).toBe("?status=todo");
    await renderPanel(true, [touched("ZED", "2026-09-22T10:00:00Z")]);
    expect(search()).toBe("?status=todo&project=ZED");
  });

  it("waits for the projects too, when the tickets come first", async () => {
    let load!: (projects: ReturnType<typeof project>[]) => void;
    projectList.mockReturnValue(new Promise((resolve) => (load = resolve)));
    await mount("/", true, [touched("BET", "2026-09-22T10:00:00Z")]);
    expect(search()).toBe("");
    await act(async () => load([project("ALP", "alpha"), project("BET", "Beta")]));
    expect(search()).toBe("?project=BET");
  });

  it("picks nothing when every project is archived, and the bar says so", async () => {
    projectList.mockResolvedValue([project("OLD", "Archive me", "archived")]);
    remember("OLD");
    await mount("/?status=todo", true, [touched("OLD", "2026-09-23T10:00:00Z")]);
    expect(search()).toBe("?status=todo");
    expect([...select("Project").options].map((o) => o.textContent)).toEqual(["No active projects"]);
  });

  it("drops a deleted project from the URL when every project left is archived", async () => {
    projectList.mockResolvedValue([project("OLD", "Archive me", "archived")]);
    await mount("/?project=GONE&status=todo", true);
    expect(search()).toBe("?status=todo");
  });

  it("still keeps an archived project a URL names when every project is archived", async () => {
    projectList.mockResolvedValue([project("OLD", "Archive me", "archived")]);
    await mount("/?project=OLD", true);
    expect(search()).toBe("?project=OLD");
    expect(select("Project").value).toBe("OLD");
  });
});

// The epic filter sits right after Project, since epics belong to it, and
// offers the shown project's epics. Like a deleted label, an epic the shown
// project doesn't have leaves the URL, which is what drops it on a switch to
// another project.
describe("the epic filter", () => {
  const change = async (name: string, value: string) =>
    act(async () => {
      select(name).value = value;
      select(name).dispatchEvent(new Event("change", { bubbles: true }));
    });
  const labels = () => rowLabels("Epic");

  beforeEach(() => {
    projectList.mockResolvedValue([project("ALP"), project("BET", "Beta")]);
    epicList.mockImplementation(async (prefix: string) =>
      prefix === "ALP" ? epicListOf(["Views", "agents", "Realtime"]) : epicListOf(["Billing"], "BET"),
    );
  });

  it("comes right after Project", async () => {
    await mount("/?project=ALP", true);
    const names = [...container.querySelectorAll('select, [role="group"]')].map((el) => el.getAttribute("aria-label"));
    expect(names).toEqual(["Project", "Epic", "Status", "Priority", "Label", "Repo"]);
  });

  it("offers All epics, No epic, then the shown project's epics by name", async () => {
    await mount("/?project=ALP", true);
    expect(epicList).toHaveBeenCalledWith("ALP");
    expect(await labels()).toEqual(["No epic", "agents", "Realtime", "Views"]);
    expect(reads("Epic")).toBe("All epics");
  });

  it("writes the chosen epics, none among them, to the URL in option order, and unticking removes them", async () => {
    await mount("/?project=ALP", true);
    await choose("Epic", "Views");
    expect(search()).toBe("?project=ALP&epic=Views");
    expect(state.active).toBe(true);
    await choose("Epic", "No epic");
    expect(search()).toBe("?project=ALP&epic=none&epic=Views");
    expect(reads("Epic")).toBe("No epic, Views");
    await choose("Epic", "Views");
    await choose("Epic", "No epic");
    expect(search()).toBe("?project=ALP");
  });

  it("draws a divider under No epic only", async () => {
    await mount("/?project=ALP", true);
    await openList("Epic");
    const list = control("Epic").querySelector('[role="listbox"]')!;
    const divider = list.querySelector('[aria-hidden="true"].h-px')!;
    expect(divider.previousElementSibling!.textContent).toBe("No epic");
    expect(list.querySelectorAll('[aria-hidden="true"].h-px')).toHaveLength(1);
  });

  it("selects the epic a URL names in another case, without a duplicate", async () => {
    await mount("/?project=ALP&epic=VIEWS", true);
    expect(reads("Epic")).toBe("Views");
    expect(await labels()).toEqual(["No epic", "agents", "Realtime", "Views"]);
    expect(search()).toBe("?project=ALP&epic=VIEWS");
  });

  it("is cleared by Clear filters, which keeps the project", async () => {
    await mount("/?project=ALP&epic=Views", true);
    const clear = [...container.querySelectorAll("button")].find((b) => b.textContent?.includes("Clear filters"))!;
    await act(async () => clear.click());
    expect(search()).toBe("?project=ALP");
    expect(reads("Epic")).toBe("All epics");
  });

  it("drops an epic the shown project doesn't have, keeping the other filters", async () => {
    await mount("/?project=ALP&epic=Fleet&status=todo", true);
    expect(search()).toBe("?project=ALP&status=todo");
  });

  it("drops only the epics the shown project doesn't have, keeping No epic and the others", async () => {
    await mount("/?project=ALP&epic=Fleet&epic=views&epic=none&epic=Gone&status=todo", true);
    expect(search()).toBe("?project=ALP&epic=views&epic=none&status=todo");
    expect(reads("Epic")).toBe("No epic, Views");
  });

  it("keeps No epic whatever the project's epics are", async () => {
    epicList.mockResolvedValue(epicListOf([]));
    await mount("/?project=ALP&epic=none", true);
    expect(search()).toBe("?project=ALP&epic=none");
    expect(reads("Epic")).toBe("No epic");
  });

  it("is dropped by a switch to a project without that epic, once its epics have loaded", async () => {
    await mount("/?project=ALP&epic=Views&status=todo", true);
    expect(search()).toBe("?project=ALP&epic=Views&status=todo");
    let load!: (list: ReturnType<typeof epicListOf>) => void;
    epicList.mockImplementation(() => new Promise((resolve) => (load = resolve)));
    await change("Project", "BET");
    expect(epicList).toHaveBeenLastCalledWith("BET");
    // The last project's epics say nothing about this one's.
    expect(search()).toBe("?project=BET&epic=Views&status=todo");
    await act(async () => load(epicListOf(["Billing"], "BET")));
    expect(search()).toBe("?project=BET&status=todo");
    expect(await labels()).toEqual(["No epic", "Billing"]);
  });

  it("keeps No epic across a switch of project", async () => {
    await mount("/?project=ALP&epic=none", true);
    await change("Project", "BET");
    expect(search()).toBe("?project=BET&epic=none");
  });

  it("ignores the epics of a project no longer shown when they arrive late", async () => {
    let loadAlp!: (list: ReturnType<typeof epicListOf>) => void;
    epicList.mockImplementation((prefix: string) =>
      prefix === "ALP"
        ? new Promise((resolve) => (loadAlp = resolve))
        : Promise.resolve(epicListOf(["Billing", "Invoices"], "BET")),
    );
    await mount("/?project=ALP&epic=Billing", true);
    await change("Project", "BET");
    expect(search()).toBe("?project=BET&epic=Billing");
    expect(await labels()).toEqual(["No epic", "Billing", "Invoices"]);
    await act(async () => loadAlp(epicListOf(["Views"])));
    // BET's epics are still the ones offered: Invoices, which the URL doesn't
    // name, would be gone had ALP's late answer replaced them.
    expect(search()).toBe("?project=BET&epic=Billing");
    expect(await labels()).toEqual(["No epic", "Billing", "Invoices"]);
  });

  it("keeps an epic a URL names for another project while that project's epics load", async () => {
    await mount("/?project=ALP", true);
    expect(await labels()).toEqual(["No epic", "agents", "Realtime", "Views"]);
    let loadBet!: (list: ReturnType<typeof epicListOf>) => void;
    epicList.mockImplementation(() => new Promise((resolve) => (loadBet = resolve)));
    await act(async () => navigate("/?project=BET&epic=Billing"));
    // ALP's epics, still the last loaded, don't have Billing, and must not
    // count against BET's.
    expect(search()).toBe("?project=BET&epic=Billing");
    await act(async () => loadBet(epicListOf(["Billing"], "BET")));
    expect(search()).toBe("?project=BET&epic=Billing");
    expect(reads("Epic")).toBe("Billing");
  });

  it("keeps an epic until the epics have loaded, and one they could not load", async () => {
    epicList.mockRejectedValue(new Error("offline"));
    await mount("/?project=ALP&epic=Views", true);
    await act(async () => {});
    expect(search()).toBe("?project=ALP&epic=Views");
    // A value naming no epic the bar knows still shows, as written.
    expect(reads("Epic")).toBe("Views");
  });

  it("drops an epic deleted elsewhere on a live change", async () => {
    (globalThis as unknown as { EventSource?: unknown }).EventSource = FakeEvents;
    try {
      await mount("/?project=ALP&epic=Views", true);
      expect(search()).toBe("?project=ALP&epic=Views");
      epicList.mockResolvedValue(epicListOf(["agents"]));
      await act(async () => {
        FakeEvents.opened[FakeEvents.opened.length - 1].emit("changed");
        await new Promise((resolve) => setTimeout(resolve, DEBOUNCE_MS + 20));
      });
      expect(search()).toBe("?project=ALP");
    } finally {
      delete (globalThis as unknown as { EventSource?: unknown }).EventSource;
    }
  });
});

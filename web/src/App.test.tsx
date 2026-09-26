import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter } from "react-router-dom";
import { AppRoutes } from "./App";
import { VIEWS_GROUP_LABEL, otherItems, viewItems } from "./lib/navigation";

// Server rendering runs no effects, so the pages never call the API. The mock
// makes sure of it: any call fails the test.
vi.mock("./api/client", () => ({
  api: new Proxy(
    {},
    {
      get: (_target, name) => {
        throw new Error(`unexpected API call: api.${String(name)}`);
      },
    },
  ),
}));

function render(path: string): string {
  return renderToStaticMarkup(
    <MemoryRouter initialEntries={[path]}>
      <AppRoutes />
    </MemoryRouter>,
  );
}

function heading(html: string): string | undefined {
  return /<h1[^>]*>([^<]*)<\/h1>/.exec(html)?.[1];
}

// The labels of the links inside the Views group, in order.
function viewsGroupLinks(html: string): string[] {
  const group = /<div role="group" aria-labelledby="nav-views">(.*?)<\/div><\/div>/.exec(html);
  if (!group) return [];
  return [...group[1].matchAll(/<a [^>]*href="([^"]*)"[^>]*>.*?([^<>]+)<\/a>/g)].map(
    (m) => `${m[2]} ${m[1]}`,
  );
}

// The label of the link the router marks as the current page.
function activeLink(html: string): string | undefined {
  return /<a [^>]*aria-current="page"[^>]*>.*?([^<>]+)<\/a>/.exec(html)?.[1];
}

describe("views navigation", () => {
  it("groups exactly Dependencies, Kanban and Table under Views, with Projects, Epics and Labels outside", () => {
    expect(VIEWS_GROUP_LABEL).toBe("Views");
    expect(viewItems.map((i) => `${i.label} ${i.to}`)).toEqual([
      "Dependencies /",
      "Kanban /kanban",
      "Table /table",
    ]);
    expect(otherItems.map((i) => `${i.label} ${i.to}`)).toEqual([
      "Projects /projects",
      "Epics /epics",
      "Labels /labels",
    ]);
  });

  it("renders the Views group in the sidebar with Projects, Epics and Labels outside it", () => {
    const html = render("/");
    expect(html).toContain('<p id="nav-views"');
    expect(html).toMatch(/id="nav-views"[^>]*>Views<\/p>/);
    expect(viewsGroupLinks(html)).toEqual(["Dependencies /", "Kanban /kanban", "Table /table"]);
    expect(html).toMatch(/href="\/projects"[^>]*>.*?Projects<\/a>/);
    expect(html).toMatch(/href="\/epics"[^>]*>.*?Epics<\/a>/);
    expect(html).toMatch(/href="\/labels"[^>]*>.*?Labels<\/a>/);
  });

  it.each([
    ["/", "Dependencies"],
    ["/kanban", "Kanban"],
    ["/table", "Table"],
    ["/epics", "Epics"],
  ])("serves %s with the heading and active nav entry %s", (path, name) => {
    const html = render(path);
    expect(heading(html)).toBe(name);
    expect(activeLink(html)).toBe(name);
  });

  it.each([
    ["/table/", "Table"],
    ["/kanban/", "Kanban"],
    ["/projects/", "Projects"],
    ["/KANBAN", "Kanban"],
  ])("marks the entry current on %s, as on its own path", (path, name) => {
    const html = render(path);
    expect(heading(html)).toBe(name);
    expect(activeLink(html)).toBe(name);
  });

  it.each(["/board", "/tickets", "/graph", "/no/such/page", "/kanban/extra"])(
    "serves a not-found page inside the layout for %s, with no redirect",
    (path) => {
      const html = render(path);
      expect(heading(html)).toBe("Page not found");
      expect(html).toContain(`>${path}</code>`);
      // The sidebar is there, with no entry marked as the current page.
      expect(viewsGroupLinks(html)).toEqual(["Dependencies /", "Kanban /kanban", "Table /table"]);
      expect(html).toMatch(/href="\/projects"/);
      expect(activeLink(html)).toBeUndefined();
      // A way back to the default view.
      expect(html).toMatch(/<a [^>]*href="\/"[^>]*>Go to Dependencies<\/a>/);
    },
  );
});

// The select with the given accessible name, and the value it shows selected.
function selectedOption(html: string, name: string): string | undefined {
  const select = new RegExp(`<select aria-label="${name}"[^>]*>(.*?)</select>`).exec(html);
  if (!select) return undefined;
  return /<option value="([^"]*)" selected="">/.exec(select[1])?.[1];
}

// What the multi-value filter control with the given name reads, or
// undefined when there is none.
function filterReads(html: string, name: string): string | undefined {
  const control = new RegExp(`<div role="group" aria-label="${name}"[^>]*><button[^>]*><span class="truncate">([^<]*)</span>`);
  return control.exec(html)?.[1];
}

describe("filter panel", () => {
  const views = viewItems.map((i) => [i.label, i.to]);

  it.each(views)("renders on %s with every filter and no other project selector", (_name, path) => {
    const html = render(path);
    expect(html).toContain('role="search"');
    for (const [name, all] of [
      ["Epic", "All epics"],
      ["Status", "All statuses"],
      ["Priority", "All priorities"],
      ["Label", "All labels"],
      ["Repo", "All repos"],
    ]) {
      expect(filterReads(html, name), name).toBe(all);
    }
    // Every view always has a project: there is no "All projects", only a
    // placeholder that can't be picked until the bar has picked one.
    expect(html).toMatch(/<select aria-label="Project"[^>]*><option value="" disabled="" selected="">Project<\/option><\/select>/);
    expect(html).toContain('<input type="search" aria-label="Search"');
    // The project is the one select; every other filter takes several values.
    expect(html.match(/<select/g)).toHaveLength(1);
    // The epic comes right after the project, since epics belong to it.
    expect(html.match(/<(?:select|div role="group") aria-label="(\w+)"/g)!.slice(0, 6)).toEqual([
      '<select aria-label="Project"',
      '<div role="group" aria-label="Epic"',
      '<div role="group" aria-label="Status"',
      '<div role="group" aria-label="Priority"',
      '<div role="group" aria-label="Label"',
      '<div role="group" aria-label="Repo"',
    ]);
    expect(html).not.toMatch(/All projects/i);
    // Nothing to clear.
    expect(html).not.toContain("Clear filters");
  });

  it.each(views)("reads its state from the URL on %s", (_name, path) => {
    const html = render(`${path}?project=ACP&epic=none&status=in_progress&priority=high&label=web&repo=a%2Fb&q=live+%26+hook&ticket=ACP-7`);
    expect(selectedOption(html, "Project")).toBe("ACP");
    expect(filterReads(html, "Epic")).toBe("No epic");
    expect(filterReads(html, "Status")).toBe("In Progress");
    expect(filterReads(html, "Priority")).toBe("High");
    expect(filterReads(html, "Label")).toBe("web");
    expect(filterReads(html, "Repo")).toBe("a/b");
    expect(html).toMatch(/aria-label="Search"[^>]*value="live &amp; hook"/);
    expect(html).toContain("Clear filters");
  });

  it.each(views)("reads several values of a filter from repeated parameters on %s", (_name, path) => {
    const html = render(`${path}?project=ACP&status=in_progress&status=todo&label=web&label=api&epic=none&epic=Views`);
    expect(filterReads(html, "Status")).toBe("Todo, In Progress");
    expect(filterReads(html, "Label")).toBe("web, api");
    expect(filterReads(html, "Epic")).toBe("No epic, Views");
    expect(html).toContain('title="Status: Todo, In Progress"');
    expect(html).toContain("Clear filters");
  });

  // The radios' values, the checked one marked with a star, or null when the
  // page has no dim-or-hide choice.
  function unmatchedChoice(html: string): string[] | null {
    const group = /<div role="radiogroup" aria-label="Cards the filters don&#x27;t match"[^>]*>(.*?)<\/div>/.exec(html);
    if (!group) return null;
    return [...group[1].matchAll(/<input type="radio"([^>]*)\/>([^<]*)/g)].map(
      (m) => `${m[2]}${/checked=""/.test(m[1]) ? "*" : ""}`,
    );
  }

  it("offers Dim or Hide on Dependencies only, after the search and before Clear filters, with Dim the default", () => {
    expect(unmatchedChoice(render("/?project=ACP&label=web"))).toEqual(["Dim*", "Hide"]);
    // Shown with no filter to dim or hide by, too, so the bar doesn't jump.
    expect(unmatchedChoice(render("/?project=ACP"))).toEqual(["Dim*", "Hide"]);
    const html = render("/?project=ACP&label=web");
    const at = (text: string) => html.indexOf(text);
    expect(at('aria-label="Search"')).toBeLessThan(at('role="radiogroup"'));
    expect(at('role="radiogroup"')).toBeLessThan(at("Clear filters"));
    // A tooltip says what it's for; there is no visible label.
    expect(html).toContain('title="Cards the filters don&#x27;t match: dim them or hide them"');
    expect(unmatchedChoice(render("/kanban?project=ACP&label=web"))).toBeNull();
    expect(unmatchedChoice(render("/table?project=ACP&label=web"))).toBeNull();
    expect(unmatchedChoice(render("/kanban?project=ACP&unmatched=hide"))).toBeNull();
  });

  it("reads hide mode from the URL on Dependencies, and dim for a value that means nothing", () => {
    expect(unmatchedChoice(render("/?project=ACP&label=web&unmatched=hide"))).toEqual(["Dim", "Hide*"]);
    expect(unmatchedChoice(render("/?project=ACP&unmatched=bogus"))).toEqual(["Dim*", "Hide"]);
  });

  it("carries hide mode to the other views with the filters", () => {
    const html = render("/?ticket=ACP-7&unmatched=hide&label=web&project=ACP");
    expect(viewsGroupLinks(html)).toEqual([
      "Dependencies /?project=ACP&amp;label=web&amp;unmatched=hide",
      "Kanban /kanban?project=ACP&amp;label=web&amp;unmatched=hide",
      "Table /table?project=ACP&amp;label=web&amp;unmatched=hide",
    ]);
    // Kanban ignores it but keeps it in its links, for the way back.
    expect(viewsGroupLinks(render("/kanban?project=ACP&unmatched=hide"))[0]).toBe("Dependencies /?project=ACP&amp;unmatched=hide");
    expect(viewsGroupLinks(render("/?project=ACP&unmatched=bogus"))[1]).toBe("Kanban /kanban?project=ACP");
    expect(render("/?project=ACP&unmatched=hide")).toMatch(/href="\/epics"/);
  });

  it("carries the filters, and only the filters, to the other views, not to Projects, Epics or Labels", () => {
    const html = render("/kanban?ticket=ACP-7&label=web&project=ACP");
    expect(viewsGroupLinks(html)).toEqual([
      "Dependencies /?project=ACP&amp;label=web",
      "Kanban /kanban?project=ACP&amp;label=web",
      "Table /table?project=ACP&amp;label=web",
    ]);
    expect(activeLink(html)).toBe("Kanban");
    // Projects, Epics and Labels sit outside the group and have no filters to
    // carry: each link is bare, with no query string at all.
    expect(html).toMatch(/href="\/projects"/);
    expect(html).toMatch(/href="\/epics"/);
    expect(html).toMatch(/href="\/labels"/);
  });
});

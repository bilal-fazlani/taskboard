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
  it("groups exactly Dependencies, Kanban, Table and Epics under Views", () => {
    expect(VIEWS_GROUP_LABEL).toBe("Views");
    expect(viewItems.map((i) => `${i.label} ${i.to}`)).toEqual([
      "Dependencies /",
      "Kanban /kanban",
      "Table /table",
      "Epics /epics",
    ]);
    expect(otherItems.map((i) => `${i.label} ${i.to}`)).toEqual([
      "Projects /projects",
      "Labels /labels",
    ]);
  });

  it("renders the Views group in the sidebar with Projects and Labels outside it", () => {
    const html = render("/");
    expect(html).toContain('<p id="nav-views"');
    expect(html).toMatch(/id="nav-views"[^>]*>Views<\/p>/);
    expect(viewsGroupLinks(html)).toEqual(["Dependencies /", "Kanban /kanban", "Table /table", "Epics /epics"]);
    expect(html).toMatch(/href="\/projects"[^>]*>.*?Projects<\/a>/);
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

  it.each(["/board", "/tickets", "/graph"])("no longer serves %s, not even as a redirect", (path) => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    try {
      const html = render(path);
      // The layout route does not match either, so nothing renders at all.
      expect(html).toBe("");
      expect(warn).toHaveBeenCalledWith(expect.stringContaining(`No routes matched location "${path}"`));
    } finally {
      warn.mockRestore();
    }
  });
});

// The select with the given accessible name, and the value it shows selected.
function selectedOption(html: string, name: string): string | undefined {
  const select = new RegExp(`<select aria-label="${name}"[^>]*>(.*?)</select>`).exec(html);
  if (!select) return undefined;
  return /<option value="([^"]*)" selected="">/.exec(select[1])?.[1];
}

describe("filter panel", () => {
  // Epics has only the project selector (see Epics.dom.test.tsx).
  const views = viewItems.filter((i) => i.to !== "/epics").map((i) => [i.label, i.to]);

  it.each(views)("renders on %s with every filter and no other project selector", (_name, path) => {
    const html = render(path);
    expect(html).toContain('role="search"');
    for (const name of ["Status", "Priority", "Label", "Repo"]) {
      expect(selectedOption(html, name), name).toBe("");
    }
    // Every view always has a project: there is no "All projects", only a
    // placeholder that can't be picked until the bar has picked one.
    expect(html).toMatch(/<select aria-label="Project"[^>]*><option value="" disabled="" selected="">Project<\/option><\/select>/);
    expect(html).toContain('<input type="search" aria-label="Search"');
    expect(html.match(/<select/g)).toHaveLength(5);
    expect(html).not.toMatch(/All projects/i);
    // Nothing to clear.
    expect(html).not.toContain("Clear filters");
  });

  it.each(views)("reads its state from the URL on %s", (_name, path) => {
    const html = render(`${path}?project=ACP&status=in_progress&priority=high&label=web&repo=a%2Fb&q=live+%26+hook&ticket=ACP-7`);
    expect(selectedOption(html, "Project")).toBe("ACP");
    expect(selectedOption(html, "Status")).toBe("in_progress");
    expect(selectedOption(html, "Priority")).toBe("high");
    expect(selectedOption(html, "Label")).toBe("web");
    expect(selectedOption(html, "Repo")).toBe("a/b");
    expect(html).toMatch(/aria-label="Search"[^>]*value="live &amp; hook"/);
    expect(html).toContain("Clear filters");
  });

  it("carries the filters, and only the filters, to the other views", () => {
    const html = render("/kanban?ticket=ACP-7&label=web&project=ACP");
    expect(viewsGroupLinks(html)).toEqual([
      "Dependencies /?project=ACP&amp;label=web",
      "Kanban /kanban?project=ACP&amp;label=web",
      "Table /table?project=ACP&amp;label=web",
      "Epics /epics?project=ACP&amp;label=web",
    ]);
    expect(activeLink(html)).toBe("Kanban");
    // Projects and Labels have no filters to carry.
    expect(html).toMatch(/href="\/projects"/);
    expect(html).toMatch(/href="\/labels"/);
  });
});

import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter } from "react-router-dom";
import { AppRoutes } from "./App";
import { VIEWS_GROUP_LABEL, otherItems, viewItems } from "./lib/navigation";

// The terminal needs a DOM, and it stays closed here anyway.
vi.mock("./components/TerminalPanel", () => ({ default: () => null }));

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
  it("groups exactly Dependencies, Kanban and Table under Views", () => {
    expect(VIEWS_GROUP_LABEL).toBe("Views");
    expect(viewItems.map((i) => `${i.label} ${i.to}`)).toEqual([
      "Dependencies /",
      "Kanban /kanban",
      "Table /table",
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
    expect(viewsGroupLinks(html)).toEqual(["Dependencies /", "Kanban /kanban", "Table /table"]);
    expect(html).toMatch(/href="\/projects"[^>]*>.*?Projects<\/a>/);
    expect(html).toMatch(/href="\/labels"[^>]*>.*?Labels<\/a>/);
  });

  it.each([
    ["/", "Dependencies"],
    ["/kanban", "Kanban"],
    ["/table", "Table"],
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

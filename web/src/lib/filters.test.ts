import { describe, expect, it } from "vitest";
import {
  EMPTY_FILTERS,
  FILTER_KEYS,
  NARROWING_KEYS,
  filterSearch,
  hasFilters,
  inProject,
  matchesFilters,
  parseFilters,
  repoOptions,
  selectOptions,
  urlValue,
  withFilter,
  withoutFilters,
  type FilterableTicket,
  type Filters,
} from "./filters";

function ticket(overrides: Partial<FilterableTicket> = {}): FilterableTicket {
  return {
    number: 7,
    title: "Live refresh hook",
    description: "Wire the **SSE** stream into every page",
    status: "todo",
    priority: "high",
    projectPrefix: "ACP",
    repos: ["bilal-fazlani/taskboard"],
    labels: [{ name: "web" }, { name: "M2:Realtime" }],
    ...overrides,
  };
}

const f = (filters: Partial<Filters>): Filters => ({ ...EMPTY_FILTERS, ...filters });

describe("matchesFilters", () => {
  it("matches everything with no filters", () => {
    expect(hasFilters(EMPTY_FILTERS)).toBe(false);
    expect(matchesFilters(ticket(), EMPTY_FILTERS)).toBe(true);
    expect(matchesFilters(ticket({ repos: undefined, labels: [], description: "" }), EMPTY_FILTERS)).toBe(true);
  });

  it("filters by project prefix, case-insensitively", () => {
    expect(matchesFilters(ticket(), f({ project: "ACP" }))).toBe(true);
    expect(matchesFilters(ticket(), f({ project: "acp" }))).toBe(true);
    expect(matchesFilters(ticket(), f({ project: "LDR" }))).toBe(false);
    // A prefix, not a prefix match: ACP does not match ACPX.
    expect(matchesFilters(ticket({ projectPrefix: "ACPX" }), f({ project: "ACP" }))).toBe(false);
  });

  it("filters by status", () => {
    expect(matchesFilters(ticket(), f({ status: "todo" }))).toBe(true);
    expect(matchesFilters(ticket(), f({ status: "in_progress" }))).toBe(false);
  });

  it("filters by priority", () => {
    expect(matchesFilters(ticket(), f({ priority: "high" }))).toBe(true);
    expect(matchesFilters(ticket(), f({ priority: "low" }))).toBe(false);
  });

  it("filters by label name, case-insensitively", () => {
    expect(matchesFilters(ticket(), f({ label: "web" }))).toBe(true);
    expect(matchesFilters(ticket(), f({ label: "m2:realtime" }))).toBe(true);
    expect(matchesFilters(ticket(), f({ label: "api" }))).toBe(false);
    expect(matchesFilters(ticket({ labels: [] }), f({ label: "web" }))).toBe(false);
    // The API leaves the field out when a ticket has no labels.
    expect(matchesFilters(ticket({ labels: undefined }), f({ label: "web" }))).toBe(false);
    expect(matchesFilters(ticket({ labels: null }), f({ label: "web" }))).toBe(false);
  });

  it("filters by repo, exactly", () => {
    expect(matchesFilters(ticket(), f({ repo: "bilal-fazlani/taskboard" }))).toBe(true);
    expect(matchesFilters(ticket(), f({ repo: "Bilal-Fazlani/Taskboard" }))).toBe(false);
    expect(matchesFilters(ticket(), f({ repo: "bilal-fazlani" }))).toBe(false);
    expect(matchesFilters(ticket({ repos: undefined }), f({ repo: "bilal-fazlani/taskboard" }))).toBe(false);
  });

  it.each([
    ["the key", "acp-7"],
    ["the key, upper case", "ACP-7"],
    ["part of the key", "p-7"],
    ["the title", "REFRESH"],
    ["the description", "EVERY page"],
  ])("finds text in %s, case-insensitively", (_where, q) => {
    expect(matchesFilters(ticket(), f({ q }))).toBe(true);
  });

  it("rejects text found nowhere, and ignores surrounding spaces", () => {
    expect(matchesFilters(ticket(), f({ q: "graph" }))).toBe(false);
    // Only key, title and description are searched, not labels or repos.
    expect(matchesFilters(ticket(), f({ q: "realtime" }))).toBe(false);
    expect(matchesFilters(ticket(), f({ q: "taskboard" }))).toBe(false);
    expect(matchesFilters(ticket(), f({ q: "  hook  " }))).toBe(true);
    expect(matchesFilters(ticket(), f({ q: "   " }))).toBe(true);
  });

  it("tolerates a ticket without a description", () => {
    const t = ticket({ description: undefined });
    expect(matchesFilters(t, f({ q: "hook" }))).toBe(true);
    expect(matchesFilters(t, f({ q: "sse" }))).toBe(false);
  });

  it("requires every set filter to match", () => {
    const all = f({ project: "ACP", status: "todo", priority: "high", label: "web", repo: "bilal-fazlani/taskboard", q: "hook" });
    expect(matchesFilters(ticket(), all)).toBe(true);
    for (const key of FILTER_KEYS) {
      expect(matchesFilters(ticket(), { ...all, [key]: "nope" }), key).toBe(false);
    }
  });
});

describe("URL state", () => {
  it("reads every filter, and empty strings for missing ones", () => {
    expect(parseFilters(new URLSearchParams(""))).toEqual(EMPTY_FILTERS);
    expect(
      parseFilters(new URLSearchParams("project=ACP&status=todo&priority=high&label=web&repo=a%2Fb&q=live+hook&ticket=ACP-7")),
    ).toEqual({ project: "ACP", status: "todo", priority: "high", label: "web", repo: "a/b", q: "live hook" });
  });

  it("round-trips filters through the query string", () => {
    const filters = f({ project: "ACP", status: "in_progress", label: "M3:Views", repo: "bilal-fazlani/taskboard", q: "a & b = c?" });
    let params = new URLSearchParams();
    for (const key of FILTER_KEYS) params = withFilter(params, key, filters[key]);
    const url = `/kanban?${params.toString()}`;
    expect(parseFilters(new URL(url, "http://localhost").searchParams)).toEqual(filters);
    expect(hasFilters(filters)).toBe(true);
  });

  it("omits empty filters from the URL", () => {
    let params = new URLSearchParams("project=ACP&q=x");
    params = withFilter(params, "q", "");
    params = withFilter(params, "status", "");
    params = withFilter(params, "label", "   ");
    expect(params.toString()).toBe("project=ACP");
  });

  it("writes readable URLs", () => {
    let params = new URLSearchParams();
    params = withFilter(params, "project", "ACP");
    params = withFilter(params, "status", "todo");
    params = withFilter(params, "label", "web");
    expect(params.toString()).toBe("project=ACP&status=todo&label=web");
  });

  it("keeps unknown parameters when setting and clearing filters", () => {
    const start = new URLSearchParams("ticket=ACP-7&project=ACP&zoom=2");
    const set = withFilter(start, "status", "todo");
    expect(set.toString()).toBe("ticket=ACP-7&project=ACP&zoom=2&status=todo");
    expect(withFilter(set, "project", "").toString()).toBe("ticket=ACP-7&zoom=2&status=todo");
    expect(withoutFilters(set).toString()).toBe("ticket=ACP-7&zoom=2");
    // The input is never modified.
    expect(start.toString()).toBe("ticket=ACP-7&project=ACP&zoom=2");
  });

  it("carries only the filters to another view", () => {
    expect(filterSearch("")).toBe("");
    expect(filterSearch("?ticket=ACP-7")).toBe("");
    expect(filterSearch("?ticket=ACP-7&label=web&project=ACP&q=")).toBe("?project=ACP&label=web");
    expect(filterSearch("?q=a%26b")).toBe("?q=a%26b");
  });
});

describe("repoOptions", () => {
  it("lists each repo once, sorted, keeping a selected one no ticket has", () => {
    const tickets = [{ repos: ["z/z", "a/a"] }, { repos: ["a/a"] }, {}];
    expect(repoOptions(tickets)).toEqual(["a/a", "z/z"]);
    expect(repoOptions(tickets, "a/a")).toEqual(["a/a", "z/z"]);
    expect(repoOptions(tickets, "m/m")).toEqual(["a/a", "m/m", "z/z"]);
  });
});

describe("urlValue", () => {
  it("stores blank text as nothing and anything else as typed", () => {
    expect(urlValue("")).toBe("");
    expect(urlValue("   ")).toBe("");
    expect(urlValue(" dash ")).toBe(" dash ");
  });
});

describe("selectOptions", () => {
  const options = [
    { value: "web", label: "web" },
    { value: "ALP", label: "Alpha" },
  ];

  it("leaves the options alone with nothing selected", () => {
    expect(selectOptions(options, "")).toEqual({ options, value: "" });
  });

  it("selects an exact match", () => {
    expect(selectOptions(options, "web")).toEqual({ options, value: "web" });
  });

  it("matches another case only when told to", () => {
    expect(selectOptions(options, "WEB", true)).toEqual({ options, value: "web" });
    expect(selectOptions(options, "alp", true)).toEqual({ options, value: "ALP" });
    expect(selectOptions(options, "WEB")).toEqual({
      options: [...options, { value: "WEB", label: "WEB" }],
      value: "WEB",
    });
  });

  it("adds a value that matches no option", () => {
    expect(selectOptions(options, "gone", true)).toEqual({
      options: [...options, { value: "gone", label: "gone" }],
      value: "gone",
    });
  });
});

describe("filters other than the project", () => {
  it("are every filter but the project", () => {
    expect(NARROWING_KEYS).toEqual(["status", "priority", "label", "repo", "q"]);
  });

  it("make a view filtered, where the project alone does not", () => {
    expect(hasFilters(f({ project: "ACP" }), NARROWING_KEYS)).toBe(false);
    expect(hasFilters(f({ project: "ACP" }))).toBe(true);
    expect(hasFilters(f({ project: "ACP", q: "x" }), NARROWING_KEYS)).toBe(true);
  });
});

describe("inProject", () => {
  const tickets = [
    { id: "1", projectPrefix: "ACP" },
    { id: "2", projectPrefix: "LDR" },
    { id: "3", projectPrefix: "ACP" },
  ];

  it("keeps the project's tickets, ignoring case", () => {
    expect(inProject(tickets, "acp").map((t) => t.id)).toEqual(["1", "3"]);
    expect(inProject(tickets, "GONE")).toEqual([]);
  });

  it("keeps every ticket when no project is given", () => {
    expect(inProject(tickets, "").map((t) => t.id)).toEqual(["1", "2", "3"]);
  });
});

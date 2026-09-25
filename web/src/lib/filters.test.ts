import { describe, expect, it } from "vitest";
import {
  EMPTY_FILTERS,
  FILTER_KEYS,
  NARROWING_KEYS,
  NO_EPIC,
  UNMATCHED_PARAM,
  filterSearch,
  hasFilters,
  hasInvalidUnmatched,
  inProject,
  matchesFilters,
  multiSelectOptions,
  parseFilters,
  parseUnmatched,
  repoOptions,
  selectOptions,
  toggleValue,
  urlValue,
  withFilter,
  withUnmatched,
  withoutFilters,
  withoutValues,
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
    epic: { name: "Realtime" },
    ...overrides,
  };
}

const f = (filters: Partial<Filters>): Filters => ({ ...EMPTY_FILTERS, ...filters });

describe("matchesFilters", () => {
  it("matches the search through a ticket's documents too", () => {
    const t = ticket({ id: "t1", title: "Plain", description: "" });
    expect(matchesFilters(t, f({ q: "storage" }))).toBe(false);
    expect(matchesFilters(t, f({ q: "storage" }), null)).toBe(false);
    expect(matchesFilters(t, f({ q: "storage" }), new Set(["t1"]))).toBe(true);
    expect(matchesFilters(t, f({ q: "storage" }), new Set(["other"]))).toBe(false);
    // A ticket without an id can't be matched through documents.
    expect(matchesFilters(ticket({ title: "Plain", description: "" }), f({ q: "storage" }), new Set(["t1"]))).toBe(false);
    // Document matches never widen the other filters.
    expect(matchesFilters(t, f({ q: "storage", status: ["done"] }), new Set(["t1"]))).toBe(false);
    expect(matchesFilters(t, f({ q: "storage", project: "LDR" }), new Set(["t1"]))).toBe(false);
    // With no search they change nothing.
    expect(matchesFilters(t, f({ status: ["done"] }), new Set(["t1"]))).toBe(false);
  });

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
    expect(matchesFilters(ticket(), f({ status: ["todo"] }))).toBe(true);
    expect(matchesFilters(ticket(), f({ status: ["in_progress"] }))).toBe(false);
  });

  it("filters by priority", () => {
    expect(matchesFilters(ticket(), f({ priority: ["high"] }))).toBe(true);
    expect(matchesFilters(ticket(), f({ priority: ["low"] }))).toBe(false);
  });

  it("filters by label name, case-insensitively", () => {
    expect(matchesFilters(ticket(), f({ label: ["web"] }))).toBe(true);
    expect(matchesFilters(ticket(), f({ label: ["m2:realtime"] }))).toBe(true);
    expect(matchesFilters(ticket(), f({ label: ["api"] }))).toBe(false);
    expect(matchesFilters(ticket({ labels: [] }), f({ label: ["web"] }))).toBe(false);
    // The API leaves the field out when a ticket has no labels.
    expect(matchesFilters(ticket({ labels: undefined }), f({ label: ["web"] }))).toBe(false);
    expect(matchesFilters(ticket({ labels: null }), f({ label: ["web"] }))).toBe(false);
  });

  it("filters by epic name, case-insensitively", () => {
    expect(matchesFilters(ticket(), f({ epic: ["Realtime"] }))).toBe(true);
    expect(matchesFilters(ticket(), f({ epic: ["REALTIME"] }))).toBe(true);
    expect(matchesFilters(ticket(), f({ epic: ["Views"] }))).toBe(false);
    // A name, not a prefix of one.
    expect(matchesFilters(ticket(), f({ epic: ["Real"] }))).toBe(false);
    // The API leaves the field out when a ticket has no epic.
    expect(matchesFilters(ticket({ epic: undefined }), f({ epic: ["Realtime"] }))).toBe(false);
    expect(matchesFilters(ticket({ epic: null }), f({ epic: ["Realtime"] }))).toBe(false);
  });

  it("matches only the tickets without an epic for none, in any case", () => {
    expect(NO_EPIC).toBe("none");
    for (const epic of ["none", "None", "NONE"]) {
      expect(matchesFilters(ticket({ epic: undefined }), f({ epic: [epic] })), epic).toBe(true);
      expect(matchesFilters(ticket({ epic: null }), f({ epic: [epic] })), epic).toBe(true);
      expect(matchesFilters(ticket(), f({ epic: [epic] })), epic).toBe(false);
    }
  });

  it("filters by repo, exactly", () => {
    expect(matchesFilters(ticket(), f({ repo: ["bilal-fazlani/taskboard"] }))).toBe(true);
    expect(matchesFilters(ticket(), f({ repo: ["Bilal-Fazlani/Taskboard"] }))).toBe(false);
    expect(matchesFilters(ticket(), f({ repo: ["bilal-fazlani"] }))).toBe(false);
    expect(matchesFilters(ticket({ repos: undefined }), f({ repo: ["bilal-fazlani/taskboard"] }))).toBe(false);
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
    const all = f({ project: "ACP", epic: ["realtime"], status: ["todo"], priority: ["high"], label: ["web"], repo: ["bilal-fazlani/taskboard"], q: "hook" });
    expect(matchesFilters(ticket(), all)).toBe(true);
    for (const key of FILTER_KEYS) {
      const nope = key === "project" || key === "q" ? "nope" : ["nope"];
      expect(matchesFilters(ticket(), { ...all, [key]: nope }), key).toBe(false);
    }
  });

  it("matches a filter with several values when the ticket has any of them", () => {
    expect(matchesFilters(ticket(), f({ status: ["done", "todo"] }))).toBe(true);
    expect(matchesFilters(ticket(), f({ status: ["done", "in_progress"] }))).toBe(false);
    expect(matchesFilters(ticket(), f({ priority: ["low", "high"] }))).toBe(true);
    expect(matchesFilters(ticket(), f({ priority: ["low", "urgent"] }))).toBe(false);
    expect(matchesFilters(ticket(), f({ label: ["api", "WEB"] }))).toBe(true);
    expect(matchesFilters(ticket(), f({ label: ["api", "infra"] }))).toBe(false);
    expect(matchesFilters(ticket(), f({ repo: ["x/y", "bilal-fazlani/taskboard"] }))).toBe(true);
    expect(matchesFilters(ticket(), f({ repo: ["x/y", "z/z"] }))).toBe(false);
    expect(matchesFilters(ticket(), f({ epic: ["Views", "realtime"] }))).toBe(true);
    expect(matchesFilters(ticket(), f({ epic: ["Views", "Agents"] }))).toBe(false);
  });

  it("takes none as one epic value among others", () => {
    const either = f({ epic: ["none", "Views"] });
    expect(matchesFilters(ticket({ epic: undefined }), either)).toBe(true);
    expect(matchesFilters(ticket({ epic: { name: "views" } }), either)).toBe(true);
    expect(matchesFilters(ticket(), either)).toBe(false);
  });

  it("requires every set filter, each matched by any of its values", () => {
    const set = f({ status: ["todo", "done"], label: ["api", "web"], priority: ["high"] });
    expect(matchesFilters(ticket(), set)).toBe(true);
    // Any value of one filter never makes up for another filter missing.
    expect(matchesFilters(ticket({ status: "in_progress" }), set)).toBe(false);
    expect(matchesFilters(ticket({ labels: [{ name: "infra" }] }), set)).toBe(false);
    expect(matchesFilters(ticket({ priority: "low" }), set)).toBe(false);
    // An empty list of values is no filter.
    expect(matchesFilters(ticket(), f({ status: [], label: [] }))).toBe(true);
  });
});

describe("URL state", () => {
  it("reads every filter, and nothing for missing ones", () => {
    expect(parseFilters(new URLSearchParams(""))).toEqual(EMPTY_FILTERS);
    expect(
      parseFilters(new URLSearchParams("project=ACP&epic=Views&status=todo&priority=high&label=web&repo=a%2Fb&q=live+hook&ticket=ACP-7")),
    ).toEqual({ project: "ACP", epic: ["Views"], status: ["todo"], priority: ["high"], label: ["web"], repo: ["a/b"], q: "live hook" });
  });

  it("reads repeated parameters as several values, in URL order, without empty ones or repeats", () => {
    const filters = parseFilters(
      new URLSearchParams("status=todo&label=web&status=in_progress&label=&label=api&label=web&epic=none&epic=Views"),
    );
    expect(filters.status).toEqual(["todo", "in_progress"]);
    expect(filters.label).toEqual(["web", "api"]);
    expect(filters.epic).toEqual(["none", "Views"]);
    expect(parseFilters(new URLSearchParams("status=&priority=")).status).toEqual([]);
  });

  it("reads only the first project and search, which take one value", () => {
    expect(parseFilters(new URLSearchParams("project=ACP&project=LDR&q=a&q=b"))).toMatchObject({ project: "ACP", q: "a" });
  });

  it("writes several values as repeated parameters where the filter was, keeping the others", () => {
    const start = new URLSearchParams("ticket=ACP-7&project=ACP&status=done&zoom=2&label=web");
    const set = withFilter(start, "status", ["todo", "in_progress"]);
    expect(set.toString()).toBe("ticket=ACP-7&project=ACP&status=todo&status=in_progress&zoom=2&label=web");
    const added = withFilter(set, "repo", ["a/b", "c/d"]);
    expect(added.toString()).toBe(
      "ticket=ACP-7&project=ACP&status=todo&status=in_progress&zoom=2&label=web&repo=a%2Fb&repo=c%2Fd",
    );
    // Fewer values replace them all; none removes the filter.
    expect(withFilter(added, "status", ["done"]).toString()).toBe(
      "ticket=ACP-7&project=ACP&status=done&zoom=2&label=web&repo=a%2Fb&repo=c%2Fd",
    );
    expect(withFilter(added, "status", []).toString()).toBe("ticket=ACP-7&project=ACP&zoom=2&label=web&repo=a%2Fb&repo=c%2Fd");
    // Empty values and repeats are never written.
    expect(withFilter(start, "label", ["", "api", "api"]).toString()).toBe("ticket=ACP-7&project=ACP&status=done&zoom=2&label=api");
    // The input is never modified.
    expect(start.toString()).toBe("ticket=ACP-7&project=ACP&status=done&zoom=2&label=web");
  });

  it("round-trips several values per filter through the query string", () => {
    const filters = f({
      project: "ACP",
      epic: ["none", "Agent review & status"],
      status: ["todo", "in_progress"],
      priority: ["urgent", "high"],
      label: ["M3:Views", "web"],
      repo: ["a/b", "bilal-fazlani/taskboard"],
      q: "a & b = c?",
    });
    let params = new URLSearchParams("ticket=ACP-7");
    for (const key of FILTER_KEYS) params = withFilter(params, key, filters[key]);
    const url = new URL(`/kanban?${params.toString()}`, "http://localhost");
    expect(parseFilters(url.searchParams)).toEqual(filters);
    expect(url.searchParams.get("ticket")).toBe("ACP-7");
    // Carried to another view as they are, without the ticket.
    const carried = filterSearch(url.search);
    expect(parseFilters(new URLSearchParams(carried))).toEqual(filters);
    expect(carried).not.toContain("ticket");
  });

  it("removes only the given values, each from its own filter", () => {
    const params = new URLSearchParams("project=ACP&label=web&label=gone&status=todo&epic=Old&epic=none&ticket=ACP-7");
    const dropped = withoutValues(params, [
      { key: "label", value: "gone" },
      { key: "epic", value: "Old" },
      // A value of another filter, or not in the URL, removes nothing.
      { key: "status", value: "web" },
      { key: "label", value: "missing" },
    ]);
    expect(dropped.toString()).toBe("project=ACP&label=web&status=todo&epic=none&ticket=ACP-7");
    expect(withoutValues(params, []).toString()).toBe(params.toString());
  });

  it.each([
    ["an epic's name", "Agent review & status"],
    ["none", "none"],
  ])("round-trips the epic filter set to %s", (_what, epic) => {
    const params = withFilter(new URLSearchParams("project=ACP"), "epic", [epic]);
    expect(params.get("epic")).toBe(epic);
    expect(parseFilters(new URL(`/?${params.toString()}`, "http://localhost").searchParams).epic).toEqual([epic]);
    expect(filterSearch(`?${params.toString()}&ticket=ACP-7`)).toBe(`?${params.toString()}`);
  });

  it("writes the epic right after the project, and drops it when emptied", () => {
    let params = new URLSearchParams();
    for (const key of FILTER_KEYS) params = withFilter(params, key, f({ project: "ACP", epic: ["none"], status: ["todo"] })[key]);
    expect(params.toString()).toBe("project=ACP&epic=none&status=todo");
    expect(withFilter(params, "epic", []).toString()).toBe("project=ACP&status=todo");
  });

  it("clears the epic with the other narrowing filters, keeping the project", () => {
    const params = new URLSearchParams("project=ACP&epic=Views&label=web&ticket=ACP-7");
    expect(withoutFilters(params, NARROWING_KEYS).toString()).toBe("project=ACP&ticket=ACP-7");
  });

  it("round-trips filters through the query string", () => {
    const filters = f({ project: "ACP", epic: ["Views"], status: ["in_progress"], label: ["M3:Views"], repo: ["bilal-fazlani/taskboard"], q: "a & b = c?" });
    let params = new URLSearchParams();
    for (const key of FILTER_KEYS) params = withFilter(params, key, filters[key]);
    const url = `/kanban?${params.toString()}`;
    expect(parseFilters(new URL(url, "http://localhost").searchParams)).toEqual(filters);
    expect(hasFilters(filters)).toBe(true);
  });

  it("omits empty filters from the URL", () => {
    let params = new URLSearchParams("project=ACP&q=x");
    params = withFilter(params, "q", "");
    params = withFilter(params, "status", []);
    params = withFilter(params, "label", [""]);
    expect(params.toString()).toBe("project=ACP");
  });

  it("writes readable URLs", () => {
    let params = new URLSearchParams();
    params = withFilter(params, "project", "ACP");
    params = withFilter(params, "status", ["todo"]);
    params = withFilter(params, "label", ["web", "api"]);
    expect(params.toString()).toBe("project=ACP&status=todo&label=web&label=api");
  });

  it("keeps unknown parameters when setting and clearing filters", () => {
    const start = new URLSearchParams("ticket=ACP-7&project=ACP&zoom=2");
    const set = withFilter(start, "status", ["todo"]);
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
    expect(filterSearch("?status=todo&ticket=ACP-7&status=done&label=&status=todo")).toBe("?status=todo&status=done");
  });

  it("carries hide mode to another view, and nothing for dim or a value that means nothing", () => {
    expect(filterSearch("?unmatched=hide&ticket=ACP-7&label=web&project=ACP")).toBe(
      "?project=ACP&label=web&unmatched=hide",
    );
    expect(filterSearch("?unmatched=hide")).toBe("?unmatched=hide");
    expect(filterSearch("?project=ACP&unmatched=dim")).toBe("?project=ACP");
    expect(filterSearch("?project=ACP&unmatched=HIDE")).toBe("?project=ACP");
  });
});

describe("unmatched mode", () => {
  const params = (qs: string) => new URLSearchParams(qs);

  it("is hide only for unmatched=hide, and dim otherwise", () => {
    expect(UNMATCHED_PARAM).toBe("unmatched");
    expect(parseUnmatched(params("unmatched=hide"))).toBe("hide");
    expect(parseUnmatched(params(""))).toBe("dim");
    expect(parseUnmatched(params("unmatched=dim"))).toBe("dim");
    expect(parseUnmatched(params("unmatched=Hide"))).toBe("dim");
    expect(parseUnmatched(params("unmatched="))).toBe("dim");
  });

  it("calls any value but hide invalid, and no value valid", () => {
    expect(hasInvalidUnmatched(params(""))).toBe(false);
    expect(hasInvalidUnmatched(params("project=ACP&unmatched=hide"))).toBe(false);
    expect(hasInvalidUnmatched(params("unmatched=dim"))).toBe(true);
    expect(hasInvalidUnmatched(params("unmatched=bogus"))).toBe(true);
    expect(hasInvalidUnmatched(params("unmatched="))).toBe(true);
  });

  it("is written only in hide mode, keeping every other parameter", () => {
    const start = params("project=ACP&ticket=ACP-7");
    const hidden = withUnmatched(start, "hide");
    expect(hidden.toString()).toBe("project=ACP&ticket=ACP-7&unmatched=hide");
    expect(withUnmatched(hidden, "dim").toString()).toBe("project=ACP&ticket=ACP-7");
    expect(withUnmatched(params("unmatched=bogus&q=x"), "dim").toString()).toBe("q=x");
    // The input is never modified.
    expect(start.toString()).toBe("project=ACP&ticket=ACP-7");
  });

  it("is no filter: not parsed as one, not counted, and not removed by clearing the filters", () => {
    const withHide = params("project=ACP&unmatched=hide");
    expect(parseFilters(withHide)).toEqual({ ...EMPTY_FILTERS, project: "ACP" });
    expect(hasFilters(parseFilters(withHide), NARROWING_KEYS)).toBe(false);
    expect((FILTER_KEYS as readonly string[]).includes(UNMATCHED_PARAM)).toBe(false);
    expect(withoutFilters(params("project=ACP&label=web&unmatched=hide"), NARROWING_KEYS).toString()).toBe(
      "project=ACP&unmatched=hide",
    );
  });
});

describe("repoOptions", () => {
  it("lists each repo once, sorted, keeping selected ones no ticket has", () => {
    const tickets = [{ repos: ["z/z", "a/a"] }, { repos: ["a/a"] }, {}];
    expect(repoOptions(tickets)).toEqual(["a/a", "z/z"]);
    expect(repoOptions(tickets, ["a/a"])).toEqual(["a/a", "z/z"]);
    expect(repoOptions(tickets, ["m/m", "b/b", "a/a"])).toEqual(["a/a", "b/b", "m/m", "z/z"]);
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

describe("multiSelectOptions", () => {
  const options = [
    { value: "none", label: "No epic" },
    { value: "agents", label: "agents" },
    { value: "Views", label: "Views" },
  ];

  it("chooses nothing with no values", () => {
    expect(multiSelectOptions(options, [])).toEqual({ options, chosen: [] });
  });

  it("chooses the matching options in option order, ignoring case only when told to", () => {
    expect(multiSelectOptions(options, ["VIEWS", "NONE"], true)).toEqual({ options, chosen: ["none", "Views"] });
    expect(multiSelectOptions(options, ["VIEWS"])).toEqual({
      options: [...options, { value: "VIEWS", label: "VIEWS" }],
      chosen: ["VIEWS"],
    });
  });

  it("adds a value that matches no option, once, so it still shows", () => {
    expect(multiSelectOptions(options, ["gone", "Views", "gone"], true)).toEqual({
      options: [...options, { value: "gone", label: "gone" }],
      chosen: ["Views", "gone"],
    });
  });
});

describe("toggleValue", () => {
  const options = ["todo", "in_progress", "agent_review", "done"].map((value) => ({ value, label: value }));

  it("adds or removes one value, writing the result in option order", () => {
    expect(toggleValue(options, [], "done")).toEqual(["done"]);
    expect(toggleValue(options, ["done"], "todo")).toEqual(["todo", "done"]);
    expect(toggleValue(options, ["todo", "done"], "in_progress")).toEqual(["todo", "in_progress", "done"]);
    expect(toggleValue(options, ["todo", "in_progress", "done"], "todo")).toEqual(["in_progress", "done"]);
    expect(toggleValue(options, ["done"], "done")).toEqual([]);
  });
});

describe("filters other than the project", () => {
  it("are every filter but the project", () => {
    expect(NARROWING_KEYS).toEqual(["epic", "status", "priority", "label", "repo", "q"]);
  });

  it("make a view filtered, where the project alone does not", () => {
    expect(hasFilters(f({ project: "ACP" }), NARROWING_KEYS)).toBe(false);
    expect(hasFilters(f({ project: "ACP" }))).toBe(true);
    expect(hasFilters(f({ project: "ACP", q: "x" }), NARROWING_KEYS)).toBe(true);
    expect(hasFilters(f({ project: "ACP", epic: ["none"] }), NARROWING_KEYS)).toBe(true);
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

  it("keeps no ticket when no project is given, so a view without one shows none", () => {
    expect(inProject(tickets, "")).toEqual([]);
  });
});

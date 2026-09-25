import { describe, expect, it } from "vitest";
import { EMPTY_FILTERS, type Filters } from "./filters";
import { CHECKED_FILTERS, staleFilters } from "./staleFilters";

const filters = (overrides: Partial<Filters>): Filters => ({ ...EMPTY_FILTERS, ...overrides });
const known = { projects: ["ACP", "IAGML"], labels: ["web", "M3:Views"] };

describe("staleFilters", () => {
  it("drops a project that no longer exists", () => {
    expect(staleFilters(filters({ project: "GONE" }), known)).toEqual([{ key: "project", value: "GONE" }]);
  });

  it("drops a label that no longer exists", () => {
    expect(staleFilters(filters({ label: ["gone"] }), known)).toEqual([{ key: "label", value: "gone" }]);
  });

  it("keeps a project and a label that still exist, whatever their case", () => {
    expect(staleFilters(filters({ project: "acp", label: ["WEB"] }), known)).toEqual([]);
  });

  it("keeps status, priority, repo and the search whatever they say", () => {
    // Fixed sets and free text: nothing here can name a deleted record.
    const set = filters({ status: ["todo"], priority: ["high"], repo: ["nobody/nothing"], q: "gone" });
    expect(staleFilters(set, known)).toEqual([]);
    expect(CHECKED_FILTERS).toEqual(["project", "epic", "label"]);
  });

  it("drops an epic the shown project doesn't have, ignoring case otherwise", () => {
    const withEpics = { ...known, epics: ["Views", "Agents"] };
    expect(staleFilters(filters({ project: "ACP", epic: ["Fleet"] }), withEpics)).toEqual([{ key: "epic", value: "Fleet" }]);
    expect(staleFilters(filters({ project: "ACP", epic: ["views"] }), withEpics)).toEqual([]);
    // Another project's epics: what a switch to a project without it leaves.
    expect(staleFilters(filters({ project: "IAGML", epic: ["Views"] }), { ...known, epics: ["Billing"] })).toEqual([
      { key: "epic", value: "Views" },
    ]);
    expect(staleFilters(filters({ project: "IAGML", epic: ["Views"] }), { ...known, epics: [] })).toEqual([{ key: "epic", value: "Views" }]);
  });

  it("keeps none, which names no epic, whatever the epics are", () => {
    for (const epic of ["none", "NONE"]) {
      expect(staleFilters(filters({ project: "ACP", epic: [epic] }), { ...known, epics: [] })).toEqual([]);
    }
  });

  it("keeps an epic until the shown project's epics are known", () => {
    expect(staleFilters(filters({ project: "ACP", epic: ["Fleet"] }), { ...known, epics: null })).toEqual([]);
    expect(staleFilters(filters({ project: "ACP", epic: ["Fleet"] }), known)).toEqual([]);
  });

  it("drops only what is stale, leaving the other filters alone", () => {
    const set = filters({ project: "ACP", label: ["gone"], status: ["todo"], q: "hook" });
    expect(staleFilters(set, known)).toEqual([{ key: "label", value: "gone" }]);
  });

  it("drops both when both are gone", () => {
    expect(staleFilters(filters({ project: "X", label: ["y"] }), known)).toEqual([
      { key: "project", value: "X" },
      { key: "label", value: "y" },
    ]);
  });

  it("drops nothing against a list that has not loaded", () => {
    const set = filters({ project: "ACP", label: ["web"] });
    expect(staleFilters(set, { projects: null, labels: null })).toEqual([]);
    expect(staleFilters(filters({ project: "GONE", label: ["gone"] }), { projects: null, labels: null })).toEqual([]);
  });

  it("drops against a loaded but empty list, which is what deleting the last one leaves", () => {
    expect(staleFilters(filters({ label: ["web"] }), { projects: null, labels: [] })).toEqual([{ key: "label", value: "web" }]);
    expect(staleFilters(filters({ project: "ACP" }), { projects: [], labels: null })).toEqual([{ key: "project", value: "ACP" }]);
    expect(staleFilters(filters({ project: "ACP", label: ["web"] }), { projects: [], labels: [] })).toEqual([
      { key: "project", value: "ACP" },
      { key: "label", value: "web" },
    ]);
  });

  it("drops only the values that name nothing, keeping a filter's other values", () => {
    const set = filters({ project: "ACP", label: ["web", "gone", "M3:VIEWS", "old"], epic: ["none", "Views", "Fleet"] });
    expect(staleFilters(set, { ...known, epics: ["Views"] })).toEqual([
      { key: "epic", value: "Fleet" },
      { key: "label", value: "gone" },
      { key: "label", value: "old" },
    ]);
  });

  it("drops nothing when no filter is set", () => {
    expect(staleFilters(EMPTY_FILTERS, { projects: [], labels: [] })).toEqual([]);
  });
});

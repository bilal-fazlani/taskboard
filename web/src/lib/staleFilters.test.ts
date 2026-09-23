import { describe, expect, it } from "vitest";
import { EMPTY_FILTERS, type Filters } from "./filters";
import { CHECKED_FILTERS, staleFilters } from "./staleFilters";

const filters = (overrides: Partial<Filters>): Filters => ({ ...EMPTY_FILTERS, ...overrides });
const known = { projects: ["ACP", "IAGML"], labels: ["web", "M3:Views"] };

describe("staleFilters", () => {
  it("drops a project that no longer exists", () => {
    expect(staleFilters(filters({ project: "GONE" }), known)).toEqual(["project"]);
  });

  it("drops a label that no longer exists", () => {
    expect(staleFilters(filters({ label: "gone" }), known)).toEqual(["label"]);
  });

  it("keeps a project and a label that still exist, whatever their case", () => {
    expect(staleFilters(filters({ project: "acp", label: "WEB" }), known)).toEqual([]);
  });

  it("keeps status, priority, repo and the search whatever they say", () => {
    // Fixed sets and free text: nothing here can name a deleted record.
    const set = filters({ status: "todo", priority: "high", repo: "nobody/nothing", q: "gone" });
    expect(staleFilters(set, known)).toEqual([]);
    expect(CHECKED_FILTERS).toEqual(["project", "epic", "label"]);
  });

  it("drops an epic the shown project doesn't have, ignoring case otherwise", () => {
    const withEpics = { ...known, epics: ["Views", "Agents"] };
    expect(staleFilters(filters({ project: "ACP", epic: "Fleet" }), withEpics)).toEqual(["epic"]);
    expect(staleFilters(filters({ project: "ACP", epic: "views" }), withEpics)).toEqual([]);
    // Another project's epics: what a switch to a project without it leaves.
    expect(staleFilters(filters({ project: "IAGML", epic: "Views" }), { ...known, epics: ["Billing"] })).toEqual(["epic"]);
    expect(staleFilters(filters({ project: "IAGML", epic: "Views" }), { ...known, epics: [] })).toEqual(["epic"]);
  });

  it("keeps none, which names no epic, whatever the epics are", () => {
    for (const epic of ["none", "NONE"]) {
      expect(staleFilters(filters({ project: "ACP", epic }), { ...known, epics: [] })).toEqual([]);
    }
  });

  it("keeps an epic until the shown project's epics are known", () => {
    expect(staleFilters(filters({ project: "ACP", epic: "Fleet" }), { ...known, epics: null })).toEqual([]);
    expect(staleFilters(filters({ project: "ACP", epic: "Fleet" }), known)).toEqual([]);
  });

  it("drops only what is stale, leaving the other filters alone", () => {
    const set = filters({ project: "ACP", label: "gone", status: "todo", q: "hook" });
    expect(staleFilters(set, known)).toEqual(["label"]);
  });

  it("drops both when both are gone", () => {
    expect(staleFilters(filters({ project: "X", label: "y" }), known)).toEqual(["project", "label"]);
  });

  it("drops nothing against a list that has not loaded", () => {
    const set = filters({ project: "ACP", label: "web" });
    expect(staleFilters(set, { projects: null, labels: null })).toEqual([]);
    expect(staleFilters(filters({ project: "GONE", label: "gone" }), { projects: null, labels: null })).toEqual([]);
  });

  it("drops against a loaded but empty list, which is what deleting the last one leaves", () => {
    expect(staleFilters(filters({ label: "web" }), { projects: null, labels: [] })).toEqual(["label"]);
    expect(staleFilters(filters({ project: "ACP" }), { projects: [], labels: null })).toEqual(["project"]);
    expect(staleFilters(filters({ project: "ACP", label: "web" }), { projects: [], labels: [] })).toEqual([
      "project",
      "label",
    ]);
  });

  it("drops nothing when no filter is set", () => {
    expect(staleFilters(EMPTY_FILTERS, { projects: [], labels: [] })).toEqual([]);
  });
});

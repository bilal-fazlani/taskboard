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
    expect(CHECKED_FILTERS).toEqual(["project", "label"]);
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

// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook } from "@testing-library/react";

const mockApi = vi.hoisted(() => ({ documents: { search: vi.fn() } }));
vi.mock("../api/client", () => ({ api: mockApi }));
// The live change feed, driven by the tests: fireLiveChange() is a change.
const live = vi.hoisted(() => ({ listener: null as null | (() => void) }));
vi.mock("./useLiveRefresh", () => ({
  useLiveRefresh: (onChange: () => void) => {
    live.listener = onChange;
  },
}));

import { DOCUMENT_SEARCH_DEBOUNCE_MS, useDocumentMatches } from "./useDocumentMatches";

let hook: { result: { current: ReadonlySet<string> | null }; rerender: (props: Props) => void };
interface Props {
  q: string;
  project?: string;
}

function mount(props: Props) {
  hook = renderHook(({ q, project = "ACP" }: Props) => useDocumentMatches(q, project), { initialProps: props });
  return hook;
}

const ids = () => (hook.result.current === null ? null : [...hook.result.current]);

beforeEach(() => {
  vi.useFakeTimers();
  live.listener = null;
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.resetAllMocks();
});

async function flush(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

describe("useDocumentMatches", () => {
  it("asks once for the settled query, and keeps the last answer while the next loads", async () => {
    mockApi.documents.search.mockResolvedValueOnce({ ticketIds: ["t1"] });
    const view = mount({ q: "sto" });
    await flush(DOCUMENT_SEARCH_DEBOUNCE_MS - 10);
    view.rerender({ q: " stor " });
    await flush(DOCUMENT_SEARCH_DEBOUNCE_MS - 10);
    expect(mockApi.documents.search).not.toHaveBeenCalled();
    expect(ids()).toBeNull();
    await flush(10);
    expect(mockApi.documents.search).toHaveBeenCalledTimes(1);
    expect(mockApi.documents.search).toHaveBeenCalledWith("stor", "ACP");
    expect(ids()).toEqual(["t1"]);

    mockApi.documents.search.mockResolvedValueOnce({ ticketIds: ["t2"] });
    view.rerender({ q: "storage" });
    expect(ids()).toEqual(["t1"]);
    await flush(DOCUMENT_SEARCH_DEBOUNCE_MS);
    expect(ids()).toEqual(["t2"]);
  });

  it("asks again on a live change, keeping the answer meanwhile", async () => {
    mockApi.documents.search.mockResolvedValueOnce({ ticketIds: ["t1"] });
    mount({ q: "rollout" });
    await flush(DOCUMENT_SEARCH_DEBOUNCE_MS);
    expect(ids()).toEqual(["t1"]);

    let answer: (value: { ticketIds: string[] }) => void = () => {};
    mockApi.documents.search.mockReturnValueOnce(new Promise((resolve) => (answer = resolve)));
    await act(async () => live.listener?.());
    expect(mockApi.documents.search).toHaveBeenCalledTimes(2);
    expect(ids()).toEqual(["t1"]);
    await act(async () => answer({ ticketIds: [] }));
    expect(ids()).toEqual([]);
  });

  it("drops an answer a newer query has overtaken", async () => {
    let first: (value: { ticketIds: string[] }) => void = () => {};
    mockApi.documents.search.mockReturnValueOnce(new Promise((resolve) => (first = resolve)));
    mockApi.documents.search.mockResolvedValueOnce({ ticketIds: ["new"] });
    const view = mount({ q: "old" });
    await flush(DOCUMENT_SEARCH_DEBOUNCE_MS);
    view.rerender({ q: "newer" });
    await flush(DOCUMENT_SEARCH_DEBOUNCE_MS);
    expect(ids()).toEqual(["new"]);
    await act(async () => first({ ticketIds: ["old"] }));
    expect(ids()).toEqual(["new"]);
  });

  it("asks again for another project", async () => {
    mockApi.documents.search.mockResolvedValue({ ticketIds: [] });
    const view = mount({ q: "plan" });
    await flush(DOCUMENT_SEARCH_DEBOUNCE_MS);
    view.rerender({ q: "plan", project: "LDR" });
    await flush(DOCUMENT_SEARCH_DEBOUNCE_MS);
    expect(mockApi.documents.search.mock.calls).toEqual([
      ["plan", "ACP"],
      ["plan", "LDR"],
    ]);
  });

  it("never asks for an empty search, and forgets the answer when the search is cleared", async () => {
    const view = mount({ q: "   " });
    await flush(DOCUMENT_SEARCH_DEBOUNCE_MS * 2);
    await act(async () => live.listener?.());
    expect(mockApi.documents.search).not.toHaveBeenCalled();
    expect(ids()).toBeNull();

    mockApi.documents.search.mockResolvedValueOnce({ ticketIds: ["t1"] });
    view.rerender({ q: "plan" });
    await flush(DOCUMENT_SEARCH_DEBOUNCE_MS);
    expect(ids()).toEqual(["t1"]);
    view.rerender({ q: "" });
    expect(ids()).toBeNull();
    view.rerender({ q: "other" });
    expect(ids()).toBeNull();
  });

  it("keeps matching on the text alone when the server fails", async () => {
    mockApi.documents.search.mockRejectedValue(new Error("API error 500: x"));
    mount({ q: "storage" });
    await flush(DOCUMENT_SEARCH_DEBOUNCE_MS);
    expect(ids()).toBeNull();
  });
});

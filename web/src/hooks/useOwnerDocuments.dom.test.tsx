// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, useEffect } from "react";
import { createRoot, type Root } from "react-dom/client";
import type { DocumentMeta, DocumentOwnerRef } from "../api/client";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const mockApi = vi.hoisted(() => ({ documents: { list: vi.fn() } }));
vi.mock("../api/client", () => ({ api: mockApi }));

// The live change feed, captured so a test can fire it.
const live = vi.hoisted(() => ({ fire: () => {} }));
vi.mock("./useLiveRefresh", () => ({
  useLiveRefresh: (onChange: () => void) => {
    live.fire = onChange;
  },
}));

import { useOwnerDocuments } from "./useOwnerDocuments";

type State = ReturnType<typeof useOwnerDocuments>;
let state: State;
let root: Root;
let container: HTMLDivElement;

const spec: DocumentMeta = { id: "d1", name: "Design spec", format: "markdown", size: 1, revision: 1, createdAt: "", updatedAt: "" };

function Harness({ owner }: { owner: DocumentOwnerRef }) {
  const s = useOwnerDocuments(owner);
  useEffect(() => {
    state = s;
  });
  return null;
}

async function mount(ticketId: string | DocumentOwnerRef) {
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => root.render(<Harness owner={typeof ticketId === "string" ? { ticketId } : ticketId} />));
}

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  vi.clearAllMocks();
});

describe("useOwnerDocuments", () => {
  it("loads the ticket's documents and loads them again on a live change", async () => {
    mockApi.documents.list.mockResolvedValueOnce([]);
    await mount("t1");
    expect(mockApi.documents.list).toHaveBeenCalledWith({ ticketId: "t1" });
    expect(state.documents).toEqual([]);

    mockApi.documents.list.mockResolvedValueOnce([spec]);
    await act(async () => live.fire());
    expect(state.documents).toEqual([spec]);
    expect(state.failed).toBe(false);
  });

  it("says a first load failed, and keeps what it has when a reload fails", async () => {
    mockApi.documents.list.mockRejectedValueOnce(new Error("API error 500: boom"));
    await mount("t1");
    expect(state.documents).toBeNull();
    expect(state.failed).toBe(true);

    mockApi.documents.list.mockResolvedValueOnce([spec]);
    await act(async () => state.reload());
    expect(state.documents).toEqual([spec]);

    mockApi.documents.list.mockRejectedValueOnce(new Error("API error 500: boom"));
    await act(async () => live.fire());
    expect(state.documents).toEqual([spec]);
    expect(state.failed).toBe(false);
  });

  it("keeps only the newest reply", async () => {
    let answerFirst: (docs: DocumentMeta[]) => void = () => {};
    mockApi.documents.list.mockReturnValueOnce(new Promise((resolve) => (answerFirst = resolve)));
    await mount("t1");
    mockApi.documents.list.mockResolvedValueOnce([spec]);
    await act(async () => state.reload());
    await act(async () => answerFirst([]));
    expect(state.documents).toEqual([spec]);
  });

  it("loads an epic's documents, and a different owner's list never shows for another", async () => {
    const plan: DocumentMeta = { ...spec, id: "d2", ticketId: undefined, epicId: "e1", name: "Rollout" };
    mockApi.documents.list.mockResolvedValueOnce([plan]);
    await mount({ epicId: "e1" });
    expect(mockApi.documents.list).toHaveBeenCalledWith({ epicId: "e1" });
    expect(state.documents).toEqual([plan]);

    let answer: (docs: DocumentMeta[]) => void = () => {};
    mockApi.documents.list.mockReturnValueOnce(new Promise((resolve) => (answer = resolve)));
    await act(async () => root.render(<Harness owner={{ ticketId: "e1" }} />));
    expect(mockApi.documents.list).toHaveBeenLastCalledWith({ ticketId: "e1" });
    expect(state.documents).toBeNull();
    await act(async () => answer([spec]));
    expect(state.documents).toEqual([spec]);
  });
});

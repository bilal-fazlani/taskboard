// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { act, useEffect } from "react";
import { createRoot, type Root } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { useEpicParam, type EpicParamState } from "./useEpicParam";
import { useDocParam, type DocParamState } from "./useDocParam";
import type { DocumentMeta } from "../api/client";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

type E = { id: string; name: string };
let root: Root;
let container: HTMLDivElement;
let state: EpicParamState<E>;
let doc: DocParamState;
const launch: E = { id: "e1", name: "Launch" };
const store: E = { id: "e2", name: "Store" };
const plan: DocumentMeta = { id: "d1", epicId: "e1", name: "Plan", format: "markdown", size: 1, revision: 1, createdAt: "", updatedAt: "" };

// The epic's document parameter too, as the epic modal holds it.
function Harness({ epics }: { epics: E[] | null }) {
  const s = useEpicParam(epics);
  const d = useDocParam(s.selected ? [plan] : null, "epic");
  useEffect(() => {
    state = s;
    doc = d;
  });
  return null;
}
async function settle() {
  for (let i = 0; i < 30; i++) await new Promise((resolve) => setTimeout(resolve, 5));
}
async function render(epics: E[] | null) {
  await act(async () => {
    root.render(
      <BrowserRouter>
        <Harness epics={epics} />
      </BrowserRouter>,
    );
  });
}
async function mount(url: string, epics: E[] | null) {
  window.history.replaceState(null, "", url);
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  await render(epics);
}
const url = () => window.location.pathname + decodeURIComponent(window.location.search.replace(/\+/g, " "));
const back = () =>
  act(async () => {
    window.history.back();
    await settle();
  });

beforeEach(() => window.history.replaceState(null, "", "/"));
afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

describe("useEpicParam", () => {
  it("opens an epic as a new entry, Back closes it, Forward reopens it, and close pops it", async () => {
    await mount("/epics?project=ACP", [launch]);
    await act(async () => state.open(launch));
    expect(url()).toBe("/epics?project=ACP&epic=Launch");
    expect(state.selected?.id).toBe("e1");
    await back();
    expect(state.selected).toBeNull();

    await act(async () => {
      window.history.forward();
      await settle();
    });
    expect(state.selected?.id).toBe("e1");
    await act(async () => {
      state.close();
      await settle();
    });
    expect(url()).toBe("/epics?project=ACP");
    expect(state.selected).toBeNull();
  });

  it("closes past a document opened over the epic in one go", async () => {
    await mount("/epics?project=ACP", [launch]);
    await act(async () => state.open(launch));
    await act(async () => doc.open(plan));
    expect(url()).toBe("/epics?project=ACP&epic=Launch&doc=Plan.md");
    await act(async () => {
      state.close();
      await settle();
    });
    expect(url()).toBe("/epics?project=ACP");
  });

  it("opens the epic a pasted link names, by name ignoring case, and closing replaces the entry", async () => {
    await mount("/epics?project=ACP&epic=launch&doc=plan.md", [launch]);
    expect(state.selected?.id).toBe("e1");
    expect(doc.selected?.id).toBe("d1");
    await act(async () => {
      state.close();
      await settle();
    });
    expect(url()).toBe("/epics?project=ACP");
  });

  it("follows a rename once the list has the new name", async () => {
    await mount("/epics?project=ACP&epic=launch", [launch]);
    const renamed = { ...launch, name: "Go live" };
    await render([renamed]);
    await act(async () => state.renamed(renamed));
    await act(settle);
    expect(url()).toBe("/epics?project=ACP&epic=Go live");
    expect(state.selected?.name).toBe("Go live");
  });

  it("keeps a rename made here while the list still has the old name", async () => {
    await mount("/epics?project=ACP&epic=Launch&doc=Plan.md", [launch]);
    const renamed = { ...launch, name: "Go live" };
    await act(async () => state.renamed(renamed));
    await act(settle);
    expect(url()).toBe("/epics?project=ACP&epic=Go live&doc=Plan.md");
    expect(state.selected?.name).toBe("Go live");
    await render([renamed]);
    await act(settle);
    expect(url()).toBe("/epics?project=ACP&epic=Go live&doc=Plan.md");
  });

  it("follows a rename made elsewhere, keeping the open document", async () => {
    await mount("/epics?project=ACP&epic=Launch&doc=Plan.md", [launch]);
    expect(state.selected?.id).toBe("e1");
    await render([{ ...launch, name: "Go live" }]);
    await act(settle);
    expect(url()).toBe("/epics?project=ACP&epic=Go live&doc=Plan.md");
    expect(state.selected?.name).toBe("Go live");
  });

  it("drops an epic the list does not have, once it has loaded", async () => {
    await mount("/epics?project=ACP&epic=Nope&doc=x.md", null);
    expect(url()).toBe("/epics?project=ACP&epic=Nope&doc=x.md");
    await render([launch]);
    await act(settle);
    expect(url()).toBe("/epics?project=ACP");
    expect(state.selected).toBeNull();
  });

  it("closes an open epic that was deleted", async () => {
    await mount("/epics?project=ACP", [launch, store]);
    await act(async () => state.open(launch));
    await render([store]);
    await act(settle);
    expect(state.selected).toBeNull();
    expect(url()).toBe("/epics?project=ACP");
  });

  it("holds the epic when Back drops it with unsaved document text, and Keep editing puts both back", async () => {
    await mount("/epics?project=ACP", [launch]);
    await act(async () => state.open(launch));
    await act(async () => doc.open(plan));
    await act(async () => {
      doc.onDirtyChange(true);
      state.onDirtyChange(true);
    });
    await back();
    expect(url()).toBe("/epics?project=ACP&epic=Launch");
    expect(doc.closeRequested).toBe(true);
    expect(state.closeRequested).toBe(false);
    await back();
    expect(url()).toBe("/epics?project=ACP");
    expect(state.selected?.id).toBe("e1");
    expect(state.closeRequested).toBe(true);
    expect(doc.selected?.id).toBe("d1");

    // Keep editing: the epic's entry first, then the document's on top.
    await act(async () => {
      state.cancelClose();
      doc.cancelClose();
      await settle();
    });
    expect(url()).toBe("/epics?project=ACP&epic=Launch&doc=Plan.md");
    expect(state.closeRequested).toBe(false);
    expect(doc.closeRequested).toBe(false);

    // Back once more asks again; Discard then leaves nothing open.
    await back();
    await back();
    expect(state.closeRequested).toBe(true);
    await act(async () => {
      doc.close();
      state.close();
      doc.onDirtyChange(false);
      state.onDirtyChange(false);
      await settle();
    });
    expect(url()).toBe("/epics?project=ACP");
    expect(state.selected).toBeNull();
    expect(doc.selected).toBeNull();
  });
});

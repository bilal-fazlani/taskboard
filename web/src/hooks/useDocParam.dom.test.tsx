// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { act, useEffect } from "react";
import { createRoot, type Root } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import type { DocumentMeta } from "../api/client";
import { useDocParam, type DocParamState } from "./useDocParam";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let container: HTMLDivElement;
let state: DocParamState;

const spec: DocumentMeta = { id: "d1", name: "Design spec", format: "markdown", size: 1, revision: 1, createdAt: "", updatedAt: "" };
const notes: DocumentMeta = { ...spec, id: "d2", name: "Notes" };

function Harness({ docs }: { docs: DocumentMeta[] | null }) {
  const s = useDocParam(docs);
  useEffect(() => {
    state = s;
  });
  return null;
}

async function settle() {
  for (let i = 0; i < 30; i++) await new Promise((resolve) => setTimeout(resolve, 5));
}

async function render(docs: DocumentMeta[] | null) {
  await act(async () => {
    root.render(
      <BrowserRouter>
        <Harness docs={docs} />
      </BrowserRouter>,
    );
  });
}

async function mount(url: string, docs: DocumentMeta[] | null) {
  window.history.replaceState(null, "", url);
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  await render(docs);
}

const url = () => window.location.pathname + decodeURIComponent(window.location.search.replace(/\+/g, " "));

beforeEach(() => window.history.replaceState(null, "", "/"));
afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

describe("useDocParam", () => {
  it("opens a document as a new entry named by its display name, and Back closes it", async () => {
    await mount("/?ticket=ACP-7", [spec, notes]);
    await act(async () => state.open(spec));
    expect(url()).toBe("/?ticket=ACP-7&doc=Design spec.md");
    expect(state.selected?.id).toBe("d1");

    await act(async () => {
      window.history.back();
      await settle();
    });
    expect(url()).toBe("/?ticket=ACP-7");
    expect(state.selected).toBeNull();
  });

  it("closes by going back one entry", async () => {
    await mount("/?ticket=ACP-7", [spec]);
    await act(async () => state.open(spec));
    await act(async () => {
      state.close();
      await settle();
    });
    expect(url()).toBe("/?ticket=ACP-7");
  });

  it("opens the document a link names, and closes it in place", async () => {
    await mount("/?ticket=ACP-7&doc=design%20SPEC.md", [spec]);
    expect(state.selected?.id).toBe("d1");
    await act(async () => {
      state.close();
      await settle();
    });
    expect(url()).toBe("/?ticket=ACP-7");
  });

  it("waits for the documents before deciding a link names nothing", async () => {
    await mount("/?ticket=ACP-7&doc=Design%20spec.md", null);
    expect(state.selected).toBeNull();
    expect(state.notice).toBeNull();
    expect(url()).toBe("/?ticket=ACP-7&doc=Design spec.md");
  });

  it("says a link's document is not there, for a wrong extension too, and drops the parameter", async () => {
    await mount("/?ticket=ACP-7&doc=Design%20spec.html", [spec]);
    await act(settle);
    expect(state.selected).toBeNull();
    expect(state.notice).toBe("Couldn't find Design spec.html on this ticket.");
    expect(url()).toBe("/?ticket=ACP-7");
    await act(async () => state.dismissNotice());
    expect(state.notice).toBeNull();
  });

  it("follows a rename made elsewhere, keeping the document open", async () => {
    await mount("/?ticket=ACP-7", [spec]);
    await act(async () => state.open(spec));
    await render([{ ...spec, name: "Plan" }]);
    await act(settle);
    expect(state.selected?.name).toBe("Plan");
    expect(url()).toBe("/?ticket=ACP-7&doc=Plan.md");
  });

  it("keeps a rename made here while the list still has the old name", async () => {
    await mount("/?ticket=ACP-7", [spec]);
    await act(async () => state.open(spec));
    await act(async () => {
      state.renamed({ ...spec, name: "Plan" });
      await settle();
    });
    // The list has not reloaded yet: the URL and the title keep the new name.
    expect(url()).toBe("/?ticket=ACP-7&doc=Plan.md");
    expect(state.selected?.name).toBe("Plan");

    await render([{ ...spec, name: "Plan" }]);
    await act(settle);
    expect(url()).toBe("/?ticket=ACP-7&doc=Plan.md");
    expect(state.selected?.name).toBe("Plan");
  });

  it("says the open document was deleted and closes it", async () => {
    await mount("/?ticket=ACP-7", [spec, notes]);
    await act(async () => state.open(spec));
    await render([notes]);
    await act(settle);
    expect(state.selected).toBeNull();
    expect(state.notice).toBe("Design spec.md was deleted.");
    expect(url()).toBe("/?ticket=ACP-7");
  });
});

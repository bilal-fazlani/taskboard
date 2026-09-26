// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import type { JournalEntry, JournalPage } from "../api/client";
import { JOURNAL_AUTHOR_KEY } from "../lib/journalAuthor";
import { memoryStorage } from "../test/memoryStorage";

// The journal on the Edit Project form, with the API mocked: loading the
// newest page, showing older pages, appending, and remembering the author.

const mockApi = vi.hoisted(() => ({
  projects: { journal: vi.fn(), appendJournal: vi.fn() },
}));
vi.mock("../api/client", () => ({ api: mockApi }));
const liveRefresh = vi.hoisted(() => ({ listener: null as null | (() => void) }));
vi.mock("../hooks/useLiveRefresh", () => ({
  useLiveRefresh: (listener: () => void) => {
    liveRefresh.listener = listener;
  },
}));

import ProjectJournal from "./ProjectJournal";

const entry = (id: string, text: string, author = "orchestrator", createdAt = "2026-09-20T10:00:00Z"): JournalEntry => ({
  id,
  projectId: "p1",
  author,
  text,
  createdAt,
});

const page = (entries: JournalEntry[], total: number, nextBefore?: string): JournalPage => ({
  entries,
  total,
  hasMore: !!nextBefore,
  ...(nextBefore ? { nextBefore } : {}),
});

async function renderJournal() {
  await act(async () => {
    render(<ProjectJournal projectId="p1" />);
  });
}

const texts = () => screen.queryAllByTestId("journal-entry").map((li) => li.querySelector(".prose-card")!.textContent);
const total = () => screen.getByTestId("journal-total").textContent;
const textBox = () => screen.getByRole("textbox", { name: "New journal entry" }) as HTMLTextAreaElement;
const authorBox = () => screen.getByRole("textbox", { name: "Author" }) as HTMLInputElement;
const appendButton = () => screen.getByRole("button", { name: "Append" }) as HTMLButtonElement;

let storage: Storage;

beforeEach(() => {
  storage = memoryStorage();
  vi.stubGlobal("localStorage", storage);
  liveRefresh.listener = null;
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.resetAllMocks();
});

describe("the project journal", () => {
  it("says so when a project has no entries", async () => {
    mockApi.projects.journal.mockResolvedValue(page([], 0));
    await renderJournal();
    expect(mockApi.projects.journal).toHaveBeenCalledWith("p1");
    expect(screen.getByText("No journal entries yet.")).toBeTruthy();
    expect(total()).toBe("0");
    expect(screen.queryByRole("button", { name: /Show older entries/ })).toBeNull();
  });

  it("shows the newest page newest first, with each entry's author, time and Markdown", async () => {
    mockApi.projects.journal.mockResolvedValue(
      page(
        [
          entry("e3", "Run **finished**.", "orchestrator", "2026-09-22T10:00:00Z"),
          entry("e2", "Keep the text for now.", "Bilal", "2026-09-21T10:00:00Z"),
          entry("e1", "Run started.", "orchestrator", "2026-09-20T10:00:00Z"),
        ],
        3,
      ),
    );
    await renderJournal();
    expect(texts()).toEqual(["Run finished.", "Keep the text for now.", "Run started."]);
    const [first, second] = screen.getAllByTestId("journal-entry");
    expect(within(first).getByText("finished").tagName).toBe("STRONG");
    expect(within(second).getByText("Bilal")).toBeTruthy();
    const time = first.querySelector("time")!;
    expect(time.getAttribute("dateTime")).toBe("2026-09-22T10:00:00Z");
    expect(time.textContent).not.toBe("");
    expect(total()).toBe("3");
  });

  it("shows older entries a page at a time, below the ones shown", async () => {
    mockApi.projects.journal.mockImplementation(async (_id: string, params?: { before?: string }) => {
      if (!params?.before) return page([entry("e5", "five"), entry("e4", "four")], 5, "e4");
      if (params.before === "e4") return page([entry("e3", "three"), entry("e2", "two")], 5, "e2");
      return page([entry("e1", "one")], 5);
    });
    await renderJournal();
    expect(texts()).toEqual(["five", "four"]);

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /Show older entries/ }));
    });
    expect(mockApi.projects.journal).toHaveBeenLastCalledWith("p1", { before: "e4" });
    expect(texts()).toEqual(["five", "four", "three", "two"]);

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /Show older entries/ }));
    });
    expect(mockApi.projects.journal).toHaveBeenLastCalledWith("p1", { before: "e2" });
    expect(texts()).toEqual(["five", "four", "three", "two", "one"]);
    expect(screen.queryByRole("button", { name: /Show older entries/ })).toBeNull();
  });

  it("appends an entry at the top and clears the draft", async () => {
    mockApi.projects.journal.mockResolvedValue(page([entry("e1", "Run started.")], 1));
    mockApi.projects.appendJournal.mockResolvedValue(entry("e2", "Approved the move.", "Bilal", "2026-09-26T12:00:00Z"));
    await renderJournal();

    // Both a text and a name are needed.
    expect(appendButton().disabled).toBe(true);
    fireEvent.change(textBox(), { target: { value: "Approved the move." } });
    expect(appendButton().disabled).toBe(true);
    fireEvent.change(authorBox(), { target: { value: "  " } });
    expect(appendButton().disabled).toBe(true);
    fireEvent.change(authorBox(), { target: { value: " Bilal " } });
    expect(appendButton().disabled).toBe(false);

    await act(async () => {
      fireEvent.click(appendButton());
    });
    expect(mockApi.projects.appendJournal).toHaveBeenCalledWith("p1", { author: "Bilal", text: "Approved the move." });
    expect(texts()).toEqual(["Approved the move.", "Run started."]);
    expect(total()).toBe("2");
    expect(textBox().value).toBe("");
    expect(authorBox().value).toBe(" Bilal ");
  });

  it("appends with Cmd/Ctrl+Enter in the text, or Enter in the name", async () => {
    mockApi.projects.journal.mockResolvedValue(page([], 0));
    mockApi.projects.appendJournal.mockImplementation(async (_id: string, e: { author: string; text: string }) =>
      entry(`n-${e.text}`, e.text, e.author),
    );
    storage.setItem(JOURNAL_AUTHOR_KEY, "Bilal");
    await renderJournal();

    fireEvent.change(textBox(), { target: { value: "first" } });
    fireEvent.keyDown(textBox(), { key: "Enter" });
    expect(mockApi.projects.appendJournal).not.toHaveBeenCalled();
    await act(async () => {
      fireEvent.keyDown(textBox(), { key: "Enter", metaKey: true });
    });
    fireEvent.change(textBox(), { target: { value: "second" } });
    await act(async () => {
      fireEvent.keyDown(authorBox(), { key: "Enter" });
    });
    expect(texts()).toEqual(["second", "first"]);
  });

  it("keeps the draft and says why when an append fails", async () => {
    mockApi.projects.journal.mockResolvedValue(page([], 0));
    mockApi.projects.appendJournal.mockRejectedValue(new Error("API error 400: text is required"));
    storage.setItem(JOURNAL_AUTHOR_KEY, "Bilal");
    await renderJournal();
    fireEvent.change(textBox(), { target: { value: "Something" } });
    await act(async () => {
      fireEvent.click(appendButton());
    });
    expect(screen.getByRole("alert").textContent).toContain("text is required");
    expect(textBox().value).toBe("Something");
    expect(texts()).toEqual([]);
    expect(storage.getItem(JOURNAL_AUTHOR_KEY)).toBe("Bilal");
  });

  it("remembers the author in this browser for the next entry", async () => {
    mockApi.projects.journal.mockResolvedValue(page([], 0));
    mockApi.projects.appendJournal.mockResolvedValue(entry("e1", "Hello", "Bilal"));
    await renderJournal();
    expect(authorBox().value).toBe("");
    expect(authorBox().placeholder).toBe("Your name");

    fireEvent.change(textBox(), { target: { value: "Hello" } });
    fireEvent.change(authorBox(), { target: { value: " Bilal " } });
    await act(async () => {
      fireEvent.click(appendButton());
    });
    expect(storage.getItem(JOURNAL_AUTHOR_KEY)).toBe("Bilal");

    cleanup();
    await renderJournal();
    expect(authorBox().value).toBe("Bilal");
  });

  it("works without storage, just without remembering the author", async () => {
    const throwing = {
      getItem: () => {
        throw new Error("blocked");
      },
      setItem: () => {
        throw new Error("blocked");
      },
    };
    vi.stubGlobal("localStorage", throwing);
    mockApi.projects.journal.mockResolvedValue(page([], 0));
    mockApi.projects.appendJournal.mockResolvedValue(entry("e1", "Hello", "Bilal"));
    await renderJournal();
    expect(authorBox().value).toBe("");
    fireEvent.change(textBox(), { target: { value: "Hello" } });
    fireEvent.change(authorBox(), { target: { value: "Bilal" } });
    await act(async () => {
      fireEvent.click(appendButton());
    });
    expect(texts()).toEqual(["Hello"]);
  });

  it("adds entries appended elsewhere on a live refresh, keeping older pages shown", async () => {
    let newest = page([entry("e3", "three"), entry("e2", "two")], 3, "e2");
    mockApi.projects.journal.mockImplementation(async (_id: string, params?: { before?: string }) =>
      params?.before ? page([entry("e1", "one")], 3) : newest,
    );
    await renderJournal();
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /Show older entries/ }));
    });
    expect(texts()).toEqual(["three", "two", "one"]);

    newest = page([entry("e4", "four"), entry("e3", "three")], 4, "e3");
    await act(async () => {
      liveRefresh.listener!();
    });
    expect(texts()).toEqual(["four", "three", "two", "one"]);
    expect(total()).toBe("4");
    expect(screen.queryByRole("button", { name: /Show older entries/ })).toBeNull();
  });

  it("never skips or repeats an entry when a live refresh lands while an older page loads", async () => {
    let resolveOlder: (p: JournalPage) => void = () => {};
    let newest = page([entry("e5", "five"), entry("e4", "four")], 5, "e4");
    mockApi.projects.journal.mockImplementation((_id: string, params?: { before?: string }) => {
      if (!params?.before) return Promise.resolve(newest);
      if (params.before === "e4") return new Promise<JournalPage>((r) => (resolveOlder = r));
      if (params.before === "e2") return Promise.resolve(page([entry("e1", "one")], 6));
      throw new Error(`unexpected before ${params.before}`);
    });
    await renderJournal();
    expect(texts()).toEqual(["five", "four"]);

    // Show older entries is asked for, and while that page loads, an entry
    // appended elsewhere arrives with a live refresh.
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /Show older entries/ }));
    });
    newest = page([entry("e6", "six"), entry("e5", "five")], 6, "e5");
    await act(async () => {
      liveRefresh.listener!();
    });
    expect(texts()).toEqual(["six", "five", "four"]);

    await act(async () => {
      resolveOlder(page([entry("e3", "three"), entry("e2", "two")], 6, "e2"));
    });
    expect(texts()).toEqual(["six", "five", "four", "three", "two"]);

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /Show older entries/ }));
    });
    expect(mockApi.projects.journal).toHaveBeenLastCalledWith("p1", { before: "e2" });
    expect(texts()).toEqual(["six", "five", "four", "three", "two", "one"]);
    expect(total()).toBe("6");
    expect(screen.queryByRole("button", { name: /Show older entries/ })).toBeNull();
  });

  it("counts an appended entry once when a live refresh already brought it", async () => {
    let newest = page([entry("e1", "one")], 1);
    mockApi.projects.journal.mockImplementation(async () => newest);
    let resolveAppend: (e: JournalEntry) => void = () => {};
    mockApi.projects.appendJournal.mockImplementation(() => new Promise<JournalEntry>((r) => (resolveAppend = r)));
    storage.setItem(JOURNAL_AUTHOR_KEY, "Bilal");
    await renderJournal();

    fireEvent.change(textBox(), { target: { value: "two" } });
    await act(async () => {
      fireEvent.click(appendButton());
    });
    // The refresh that follows the write lands before the append's answer.
    newest = page([entry("e2", "two", "Bilal"), entry("e1", "one")], 2);
    await act(async () => {
      liveRefresh.listener!();
    });
    await act(async () => {
      resolveAppend(entry("e2", "two", "Bilal"));
    });
    expect(texts()).toEqual(["two", "one"]);
    expect(total()).toBe("2");
  });

  it("keeps the focus on the text after appending", async () => {
    mockApi.projects.journal.mockResolvedValue(page([], 0));
    mockApi.projects.appendJournal.mockResolvedValue(entry("e1", "Hello", "Bilal"));
    storage.setItem(JOURNAL_AUTHOR_KEY, "Bilal");
    await renderJournal();
    fireEvent.change(textBox(), { target: { value: "Hello" } });
    appendButton().focus();
    await act(async () => {
      fireEvent.click(appendButton());
    });
    expect(document.activeElement).toBe(textBox());
  });

  it("says so when the journal cannot be loaded", async () => {
    mockApi.projects.journal.mockRejectedValue(new Error("offline"));
    await renderJournal();
    expect(screen.getByText("The journal could not be loaded.")).toBeTruthy();
  });
});

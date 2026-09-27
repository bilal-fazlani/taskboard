// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { BrowserRouter } from "react-router-dom";
import type { Entry, EntryAgent, EntryPage, Project, Ticket } from "../api/client";

// The ticket page's entries, through the editor, with the API mocked: no test
// reaches a real server.
const mockApi = vi.hoisted(() => ({
  tickets: { get: vi.fn(), list: vi.fn(), history: vi.fn(), addSubtask: vi.fn() },
  subtasks: { toggle: vi.fn(), delete: vi.fn() },
  labels: { list: vi.fn() },
  epics: { list: vi.fn() },
  entries: { list: vi.fn(), createNote: vi.fn() },
  documents: {
    list: vi.fn(),
    create: vi.fn(),
    get: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
    createImage: vi.fn(),
    usage: vi.fn(),
    downloadUrl: (id: string) => `/api/documents/${id}/download`,
    imageUrl: (id: string, revision: number) => `/api/documents/${id}/image?rev=${revision}`,
    thumbnailUrl: (id: string, revision: number) => `/api/documents/${id}/thumbnail?rev=${revision}`,
  },
}));
vi.mock("../api/client", () => ({ api: mockApi }));

const live = vi.hoisted(() => ({ listeners: new Set<() => void>() }));
vi.mock("../hooks/useLiveRefresh", async () => {
  const { useEffect, useRef } = await import("react");
  return {
    useLiveRefresh: (onChange: () => void) => {
      const ref = useRef(onChange);
      useEffect(() => {
        ref.current = onChange;
      });
      useEffect(() => {
        const listener = () => ref.current();
        live.listeners.add(listener);
        return () => {
          live.listeners.delete(listener);
        };
      }, []);
    },
  };
});
const fireLive = () => act(async () => live.listeners.forEach((l) => l()));

import TicketEditor from "./TicketEditor";

const project: Project = {
  id: "p1",
  name: "Agent Control Plane",
  prefix: "ACP",
  description: "",
  icon: "",
  color: "#3b82f6",
  status: "active",
  createdAt: "",
  updatedAt: "",
};

function makeTicket(overrides: Partial<Ticket> = {}): Ticket {
  return {
    id: "t1",
    projectId: "p1",
    number: 9,
    title: "Store: agent identify and touch",
    description: "Store methods.",
    status: "in_progress",
    priority: "high",
    position: 0,
    createdAt: "",
    updatedAt: "",
    projectPrefix: "ACP",
    repos: [],
    labels: [],
    subtasks: [{ id: "s1", ticketId: "t1", title: "IdentifyAgent", completed: true, position: 0 }],
    dependsOn: [],
    blocks: [],
    epic: { id: "e1", name: "Agents" },
    ...overrides,
  };
}

const FULL_SESSION = "claude --resume 3da2c294-37eb-4757-be78-0513c53efa33";

function agent(id: string, role: string, model: string, provider: string): EntryAgent {
  return {
    id,
    sessionId: "s1",
    role,
    model,
    provider,
    createdAt: "",
    lastSeenAt: "",
    session: {
      id: "s1",
      vendor: "claude_code",
      vendorSessionId: "3da2c294",
      machine: "bilal-mbp",
      resumeCommand: FULL_SESSION,
      createdAt: "",
    },
  };
}

const AGENTS: Record<string, EntryAgent> = {
  a0: agent("a0", "orchestrator", "opus-5.5", "anthropic"),
  a1: agent("a1", "implementer", "sonnet-5", "anthropic"),
  a2: agent("a2", "reviewer", "gpt-5", "openai"),
};

const at = (minute: number) => `2026-09-27T10:${String(minute).padStart(2, "0")}:00Z`;

// ACP-9 midway through, as the mock shows it: an open challenge on a
// decision, a hand-off, a person's decision that replaced an older one, a
// review asking for changes, a learning and a handled note.
const ENTRIES: Entry[] = [
  { id: "n1", ticketId: "t1", type: "note", text: "250 ms will make the inbox feel slow.", authorName: "bilal", about: "d2", createdAt: at(10) },
  { id: "h1", ticketId: "t1", type: "hand_off", text: "Stopped: review 1 fixes done. Next: the await test.", agentId: "a1", createdAt: at(9) },
  { id: "d3", ticketId: "t1", type: "decision", source: "person", text: "Stale threshold and lease live in store config.", agentId: "a0", replaces: "d1", createdAt: at(8) },
  { id: "d2", ticketId: "t1", type: "decision", source: "agent", text: "Poll every 250 ms with a deadline. Rejected: update_hook.", agentId: "a1", createdAt: at(7) },
  {
    id: "r1",
    ticketId: "t1",
    type: "review",
    text: "AwaitAnswer returned on any request.",
    agentId: "a2",
    verdict: "changes",
    findings: { blocker: 0, major: 1, minor: 2, nit: 0 },
    reportDocument: "Review 1",
    createdAt: at(6),
  },
  { id: "l1", ticketId: "t1", type: "learning", text: "A second connection doesn't see uncommitted writes.", agentId: "a1", createdAt: at(5) },
  { id: "d1", ticketId: "t1", type: "decision", source: "agent", text: "The stale threshold is a server flag.", agentId: "a0", replacedBy: "d3", createdAt: at(2) },
  { id: "n0", ticketId: "t1", type: "note", text: "Use a temp DB.", authorName: "bilal", handledBy: "a1", handledAt: at(4), createdAt: at(1) },
];

function page(entries: Entry[], extra: Partial<EntryPage> = {}): EntryPage {
  return { entries, total: entries.length, hasMore: false, agents: AGENTS, ...extra };
}

// What the entry reads answer: the ticket's entries, and for the side
// column's counts, a total per type on the epic and the project.
let ticketEntries: Entry[];
const COUNTS: Record<string, number> = { "epic:decision": 15, "epic:learning": 0, "project:decision": 42, "project:learning": 6 };
function entriesList(owner: Record<string, string>, params: { types?: string[] } = {}) {
  if ("ticketId" in owner) return Promise.resolve(page(ticketEntries));
  const level = "epicId" in owner ? "epic" : "project";
  return Promise.resolve(page([], { total: COUNTS[`${level}:${params.types?.[0]}`] ?? 0, agents: {} }));
}

function renderEditor(ticket = makeTicket(), extra: { deleted?: boolean } = {}) {
  return render(
    <TicketEditor
      ticket={ticket}
      projects={[project]}
      onClose={vi.fn()}
      onUpdate={vi.fn()}
      onDelete={vi.fn()}
      {...extra}
    />,
    { wrapper: BrowserRouter },
  );
}

const content = () => screen.getByRole("region", { name: "Ticket content" });
const section = (name: string) => within(content()).getByRole("region", { name: new RegExp(`^${name}`) });
const texts = (root: HTMLElement) =>
  within(root)
    .queryAllByTestId("entry")
    .map((el) => el.querySelector("[data-testid=entry-text]")?.textContent);
// Throws until the card is on screen, so waitFor retries it.
const entryCard = (id: string) => {
  const card = content().querySelector<HTMLElement>(`[data-entry-id="${id}"]`);
  if (!card) throw new Error(`no entry ${id} on screen`);
  return card;
};

beforeEach(() => {
  window.history.replaceState(null, "", "/");
  ticketEntries = ENTRIES;
  mockApi.documents.list.mockResolvedValue([]);
  mockApi.epics.list.mockResolvedValue({ epics: [], noEpic: { total: 0, done: 0 } });
  mockApi.tickets.get.mockImplementation((id: string) =>
    Promise.resolve(makeTicket({ id, epicOpenNotes: 2, projectOpenNotes: 1 })),
  );
  mockApi.tickets.list.mockResolvedValue([]);
  mockApi.tickets.history.mockResolvedValue([]);
  mockApi.labels.list.mockResolvedValue([]);
  mockApi.entries.list.mockImplementation(entriesList);
});

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

describe("the ticket page's entries", () => {
  it("groups entries by type in reading order, status history last", async () => {
    renderEditor();
    await screen.findByText("Stopped: review 1 fixes done. Next: the await test.");
    const headings = within(content())
      .getAllByRole("heading", { level: 3 })
      .map((h) => h.childNodes[0]?.textContent);
    expect(headings).toEqual([
      "Notes",
      "Where it stands",
      "Decisions",
      "Reviews",
      "Learnings",
      "Proof",
      "Subtasks",
      "Documents",
      "Activity",
    ]);
    expect(texts(section("Notes"))).toEqual(["250 ms will make the inbox feel slow."]);
    expect(within(section("Notes")).getByText("1 open")).toBeTruthy();
    expect(texts(section("Where it stands"))).toEqual(["Stopped: review 1 fixes done. Next: the await test."]);
    expect(texts(section("Decisions"))).toEqual([
      "Stale threshold and lease live in store config.",
      "Poll every 250 ms with a deadline.",
    ]);
    expect(within(section("Decisions")).getByText("2 current · 1 replaced")).toBeTruthy();
    expect(within(section("Decisions")).getByText("Rejected: update_hook.")).toBeTruthy();
    expect(texts(section("Reviews"))).toEqual(["AwaitAnswer returned on any request."]);
    expect(texts(section("Learnings"))).toEqual(["A second connection doesn't see uncommitted writes."]);
    expect(within(section("Proof")).getByText(/Written when the ticket is finished/)).toBeTruthy();

    // A handled note is folded away under the open ones, and names who handled it.
    const handled = within(section("Notes")).getByRole("button", { name: /1 handled/ });
    expect(screen.queryByText("Use a temp DB.")).toBeNull();
    fireEvent.click(handled);
    expect(within(entryCard("n0")).getByText(/handled by/)).toBeTruthy();
  });

  it("tucks a replaced entry under the one that replaced it, struck through when opened", async () => {
    renderEditor();
    const card = await waitFor(() => entryCard("d3"));
    expect(screen.queryByText("The stale threshold is a server flag.")).toBeNull();
    const toggle = within(card).getByRole("button", { name: /Replaces 1 earlier entry/ });
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(toggle);
    const old = within(card).getByTestId("replaced-entry");
    const text = within(old).getByText("The stale threshold is a server flag.");
    expect(text.className).toContain("line-through");
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
  });

  it("names each author: type, role, model and vendor, or You; the person's decisions say so", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    renderEditor();
    const decision = await waitFor(() => entryCard("d3"));
    expect(decision.textContent).toContain("from you, recorded by");
    expect(decision.textContent).toContain("orchestrator · opus-5.5");
    expect(within(decision).getByRole("img", { name: "Anthropic" })).toBeTruthy();

    const chip = within(decision).getByTestId("session-chip");
    expect(chip.textContent).toContain("claude --resume 3da2c294");
    expect(chip.textContent).not.toContain("37eb");
    expect(chip.getAttribute("title")).toContain("bilal-mbp");
    fireEvent.click(chip);
    expect(writeText).toHaveBeenCalledWith(FULL_SESSION);
    await within(decision).findByRole("button", { name: "Resume command copied" });

    const agentDecision = entryCard("d2");
    expect(agentDecision.textContent).not.toContain("from you");
    expect(agentDecision.textContent).toContain("implementer · sonnet-5");

    const review = entryCard("r1");
    expect(within(review).getByTestId("entry-type").textContent).toBe("Review 1");
    expect(review.textContent).toContain("0 blocker · 1 major · 2 minor");
    expect(within(review).getByRole("img", { name: "OpenAI" })).toBeTruthy();

    const note = entryCard("n1");
    expect(within(note).getByText("You")).toBeTruthy();
    expect(within(note).queryByTestId("session-chip")).toBeNull();
  });

  it("marks a challenged entry, and the note says what it challenges", async () => {
    renderEditor();
    const challenged = await waitFor(() => entryCard("d2"));
    expect(challenged.dataset.challenged).toBe("true");
    expect(within(challenged).getByText("challenged by you")).toBeTruthy();
    expect(entryCard("d3").dataset.challenged).toBeUndefined();
    expect(within(entryCard("n1")).getByText(/Challenges: “Poll every 250 ms/)).toBeTruthy();
  });

  it("leaves a note from the box under open notes", async () => {
    const note: Entry = { id: "n2", ticketId: "t1", type: "note", text: "Aim for 100 ms.", authorName: "bilal", createdAt: at(11) };
    mockApi.entries.createNote.mockImplementation(() => {
      ticketEntries = [note, ...ENTRIES];
      return Promise.resolve(note);
    });
    renderEditor();
    await waitFor(() => entryCard("n1"));
    const box = within(section("Notes")).getByRole("textbox", { name: "Note" });
    const leave = within(section("Notes")).getByRole("button", { name: "Leave note" }) as HTMLButtonElement;
    expect(leave.disabled).toBe(true);
    fireEvent.change(box, { target: { value: "  Aim for 100 ms.  " } });
    fireEvent.click(leave);
    await waitFor(() => expect(texts(section("Notes"))).toContain("Aim for 100 ms."));
    expect(mockApi.entries.createNote).toHaveBeenCalledWith({ ticketId: "t1" }, { type: "note", text: "Aim for 100 ms." });
    expect((box as HTMLTextAreaElement).value).toBe("");
    expect(within(section("Notes")).getByText("2 open")).toBeTruthy();
  });

  it("challenges an entry: the box points at it, and the note carries it", async () => {
    mockApi.entries.createNote.mockResolvedValue({ id: "n3", type: "note", text: "x", createdAt: at(12) });
    renderEditor();
    const learning = await waitFor(() => entryCard("l1"));
    // Only decisions, learnings and proof can be challenged.
    expect(within(entryCard("h1")).queryByRole("button", { name: /^Challenge/ })).toBeNull();
    expect(within(entryCard("r1")).queryByRole("button", { name: /^Challenge/ })).toBeNull();
    fireEvent.click(within(learning).getByRole("button", { name: /^Challenge/ }));
    const box = within(section("Notes")).getByRole("textbox", { name: "Note" });
    expect(document.activeElement).toBe(box);
    expect(screen.getByTestId("note-about").textContent).toContain("A second connection doesn't see uncommitted writes.");
    fireEvent.change(box, { target: { value: "It does with WAL." } });
    fireEvent.submit(box.closest("form")!);
    await waitFor(() =>
      expect(mockApi.entries.createNote).toHaveBeenCalledWith(
        { ticketId: "t1" },
        { type: "note", text: "It does with WAL.", about: "l1" },
      ),
    );
    await waitFor(() => expect(screen.queryByTestId("note-about")).toBeNull());
  });

  it("keeps the note and says why when leaving it fails", async () => {
    mockApi.entries.createNote.mockRejectedValue(new Error("API error 500: boom"));
    renderEditor();
    await waitFor(() => entryCard("n1"));
    const box = within(section("Notes")).getByRole("textbox", { name: "Note" }) as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: "Keep me." } });
    fireEvent.click(within(section("Notes")).getByRole("button", { name: "Leave note" }));
    await within(section("Notes")).findByRole("alert");
    expect(box.value).toBe("Keep me.");
  });

  it("updates live: a new entry shows up on a change event", async () => {
    renderEditor();
    await waitFor(() => entryCard("l1"));
    ticketEntries = [
      { id: "p1", ticketId: "t1", type: "proof", text: "go test ./... passed.", agentId: "a1", createdAt: at(20) },
      ...ENTRIES,
    ];
    await fireLive();
    await waitFor(() => expect(texts(section("Proof"))).toEqual(["go test ./... passed."]));
  });

  it("counts the epic's and project's entries in the side column, linking to them", async () => {
    renderEditor();
    const epic = await screen.findByTestId("entry-context-epic");
    await waitFor(() => expect(epic.textContent).toContain("Epic Agents: 15 decisions, 2 open notes"));
    const project = screen.getByTestId("entry-context-project");
    expect(project.textContent).toContain("Project ACP: 42 decisions, 6 learnings, 1 open note");
    // Each opens its dialog straight on the entries.
    expect(within(epic).getByRole("link", { name: "open" }).getAttribute("href")).toBe(
      "/epics?project=ACP&epic=Agents#entries",
    );
    expect(within(project).getByRole("link", { name: "open" }).getAttribute("href")).toBe(
      "/projects?project=ACP#entries",
    );
  });

  it("offers no note box or Challenge on a deleted ticket", async () => {
    renderEditor(makeTicket(), { deleted: true });
    await waitFor(() => entryCard("l1"));
    expect(screen.queryByRole("textbox", { name: "Note" })).toBeNull();
    expect(screen.queryByRole("button", { name: /^Challenge/ })).toBeNull();
  });

  it("names each Challenge after its entry, and the pointer describes the note box", async () => {
    renderEditor();
    await waitFor(() => entryCard("l1"));
    const names = screen.getAllByRole("button", { name: /^Challenge/ }).map((b) => b.getAttribute("aria-label"));
    expect(names).toEqual([
      "Challenge decision: “Stale threshold and lease live in store…”",
      "Challenge decision: “Poll every 250 ms with a deadline.…”",
      "Challenge learning: “A second connection doesn't see…”",
    ]);
    // Every name differs, and each still starts with the visible word.
    expect(new Set(names).size).toBe(names.length);

    const box = within(section("Notes")).getByRole("textbox", { name: "Note" });
    expect(box.getAttribute("aria-describedby")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /^Challenge learning/ }));
    const describedBy = box.getAttribute("aria-describedby");
    expect(describedBy).toBeTruthy();
    expect(document.getElementById(describedBy!)?.textContent).toBe(
      "Challenges: “A second connection doesn't see uncommitted writes.”",
    );
    fireEvent.click(screen.getByRole("button", { name: "Don't challenge this entry" }));
    expect(box.getAttribute("aria-describedby")).toBeNull();
  });

  it("announces a copied resume command politely", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    renderEditor();
    const card = await waitFor(() => entryCard("h1"));
    const status = within(card).getByRole("status");
    expect(status.getAttribute("aria-live")).toBe("polite");
    expect(status.textContent).toBe("");
    fireEvent.click(within(card).getByTestId("session-chip"));
    await waitFor(() => expect(status.textContent).toBe("Resume command copied"));
  });

  it("says so in Notes too when the entries can't be loaded, and keeps the box", async () => {
    mockApi.entries.list.mockRejectedValue(new Error("API error 500: boom"));
    renderEditor();
    await waitFor(() => expect(within(section("Notes")).getByText("Entries could not be loaded.")).toBeTruthy());
    expect(within(section("Notes")).getByRole("textbox", { name: "Note" })).toBeTruthy();
    expect(within(section("Decisions")).getByText("Entries could not be loaded.")).toBeTruthy();
  });
});

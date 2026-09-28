// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { BrowserRouter } from "react-router-dom";
import type { Agent, Project, Ticket, TicketRequest } from "../api/client";

// The ticket page's requests for user input, through the editor, with the API
// mocked: no test reaches a real server.
const mockApi = vi.hoisted(() => ({
  tickets: { get: vi.fn(), list: vi.fn(), history: vi.fn(), addSubtask: vi.fn() },
  subtasks: { toggle: vi.fn(), delete: vi.fn() },
  labels: { list: vi.fn() },
  epics: { list: vi.fn() },
  entries: { list: vi.fn(), createNote: vi.fn() },
  requests: { list: vi.fn(), answer: vi.fn() },
  agents: { get: vi.fn() },
  documents: { list: vi.fn() },
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

const IMPLEMENTER: Agent = {
  id: "a1",
  sessionId: "s1",
  role: "implementer",
  model: "gpt-5",
  provider: "openai",
  createdAt: "",
  lastSeenAt: "",
};

const QUESTION: TicketRequest = {
  id: "q1",
  ticketId: "t1",
  agentId: "a1",
  type: "question",
  prompt: "Should review subtasks also name the finding, e.g. `Review 2: major #1`?",
  choices: ["Round only", "Round and the finding"],
  createdAt: "2026-09-28T09:00:00Z",
};

const APPROVAL: TicketRequest = {
  id: "ap1",
  ticketId: "t1",
  agentId: "a1",
  type: "approval",
  prompt: "Delete the Agents epic's **Decisions** document?",
  choices: [],
  createdAt: "2026-09-28T09:30:00Z",
};

// Two earlier requests, oldest first here so the fold's order is the page's doing.
const EARLIER: TicketRequest[] = [
  {
    id: "old",
    ticketId: "t1",
    agentId: "a1",
    type: "question",
    prompt: "Which test DB?",
    choices: ["temp", "shared"],
    answer: "A fresh t.TempDir() per test",
    answeredBy: "bilal",
    createdAt: "2026-09-27T08:00:00Z",
    answeredAt: "2026-09-27T08:05:00Z",
  },
  {
    id: "land",
    ticketId: "t1",
    agentId: "a1",
    type: "approval",
    prompt: "Land Review 1's fixes on main?",
    choices: [],
    answer: "approved",
    answeredBy: "bilal",
    note: "after the rebase",
    createdAt: "2026-09-28T08:00:00Z",
    answeredAt: "2026-09-28T08:12:00Z",
  },
];

function makeTicket(overrides: Partial<Ticket> = {}): Ticket {
  return {
    id: "t1",
    projectId: "p1",
    number: 151,
    title: "Reviews as entries",
    description: "",
    status: "needs_user_input",
    priority: "high",
    position: 0,
    createdAt: "",
    updatedAt: "2026-09-28T09:00:00Z",
    projectPrefix: "ACP",
    repos: [],
    labels: [],
    subtasks: [],
    dependsOn: [],
    blocks: [],
    agent: IMPLEMENTER,
    ...overrides,
  };
}

// What the server holds: the ticket as get reads it, and its requests.
let serverTicket: Ticket;
let serverRequests: TicketRequest[];

function renderEditor(extra: { deleted?: boolean } = {}) {
  return render(
    <TicketEditor
      ticket={serverTicket}
      projects={[project]}
      onClose={vi.fn()}
      onUpdate={vi.fn()}
      onDelete={vi.fn()}
      {...extra}
    />,
    { wrapper: BrowserRouter },
  );
}

/** Opens a request with the page's rows behind it, as the store would. */
function waitingOn(open: TicketRequest | undefined, earlier: TicketRequest[] = []) {
  serverTicket = makeTicket(open ? { openRequest: open } : { status: "in_progress" });
  serverRequests = open ? [open, ...earlier] : earlier;
}

/** The server's side of an answer: the request answered, the ticket back in progress. */
function answersLikeTheStore() {
  mockApi.requests.answer.mockImplementation((id: string, data: { answer: string; note?: string }) => {
    const r = serverRequests.find((x) => x.id === id)!;
    const done: TicketRequest = {
      ...r,
      answer: data.answer,
      note: data.note,
      answeredBy: "bilal",
      answeredAt: "2026-09-28T09:40:00Z",
    };
    serverRequests = serverRequests.map((x) => (x.id === id ? done : x));
    serverTicket = makeTicket({ status: "in_progress", updatedAt: "2026-09-28T09:40:00Z" });
    return Promise.resolve(done);
  });
}

const content = () => screen.getByRole("region", { name: "Ticket content" });
const openRequest = () => screen.findByTestId("open-request");
const status = () => screen.getByTestId("ticket-editor-status");

beforeEach(() => {
  window.history.replaceState(null, "", "/");
  mockApi.documents.list.mockResolvedValue([]);
  mockApi.epics.list.mockResolvedValue({ epics: [], noEpic: { total: 0, done: 0 } });
  mockApi.tickets.get.mockImplementation(() => Promise.resolve(serverTicket));
  mockApi.tickets.list.mockResolvedValue([]);
  mockApi.tickets.history.mockResolvedValue([]);
  mockApi.labels.list.mockResolvedValue([]);
  mockApi.entries.list.mockResolvedValue({ entries: [], total: 0, hasMore: false, agents: {} });
  mockApi.requests.list.mockImplementation(() => Promise.resolve(serverRequests));
  answersLikeTheStore();
});

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

describe("an open question", () => {
  it("shows first on the page, its prompt as markdown, its choices, and a way to answer in your own words", async () => {
    waitingOn(QUESTION);
    renderEditor();
    const block = await openRequest();
    // First in the content column, above the title.
    const title = within(content()).getByRole("textbox", { name: "Title" });
    expect(block.compareDocumentPosition(title) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(within(block).getByText("Question")).toBeTruthy();
    expect(within(block).getByText(/implementer · gpt-5/)).toBeTruthy();
    expect(within(block).getByText("Review 2: major #1").tagName).toBe("CODE");
    const radios = within(within(block).getByRole("radiogroup", { name: "Choices" })).getAllByRole("radio");
    expect(radios.map((r) => (r as HTMLInputElement).value)).toEqual(["Round only", "Round and the finding"]);
    expect(within(block).getByText("or answer in your own words")).toBeTruthy();
    expect(within(block).getByRole("textbox", { name: "Answer in your own words" })).toBeTruthy();
    // Not one of the ticket's fields: nothing about it sits in the side column.
    expect(within(screen.getByRole("complementary", { name: "Ticket fields" })).queryByText("Question")).toBeNull();
  });

  it("needs a choice or your own words before it can be sent", async () => {
    waitingOn(QUESTION);
    renderEditor();
    const block = await openRequest();
    const send = within(block).getByRole("button", { name: "Send answer" }) as HTMLButtonElement;
    expect(send.disabled).toBe(true);
    expect(within(block).getByText("Pick a choice or write your own answer.")).toBeTruthy();
    fireEvent.change(within(block).getByRole("textbox"), { target: { value: "   " } });
    expect(send.disabled).toBe(true);
    fireEvent.submit(send.closest("form")!);
    expect(mockApi.requests.answer).not.toHaveBeenCalled();
    fireEvent.click(within(block).getByRole("radio", { name: "Round only" }));
    expect(send.disabled).toBe(false);
    expect(within(block).getByText("The implementer (gpt-5) collects it on its next call.")).toBeTruthy();
  });

  it("sends a picked choice", async () => {
    waitingOn(QUESTION);
    renderEditor();
    const block = await openRequest();
    fireEvent.click(within(block).getByRole("radio", { name: "Round and the finding" }));
    fireEvent.click(within(block).getByRole("button", { name: "Send answer" }));
    await waitFor(() => expect(mockApi.requests.answer).toHaveBeenCalledWith("q1", { answer: "Round and the finding" }));
  });

  it("sends your own words over a picked choice", async () => {
    waitingOn(QUESTION);
    renderEditor();
    const block = await openRequest();
    fireEvent.click(within(block).getByRole("radio", { name: "Round only" }));
    fireEvent.change(within(block).getByRole("textbox", { name: "Answer in your own words" }), {
      target: { value: "  Neither: drop the marks  " },
    });
    expect(within(block).getByText("Your words replace the choice.")).toBeTruthy();
    fireEvent.click(within(block).getByRole("button", { name: "Send answer" }));
    await waitFor(() => expect(mockApi.requests.answer).toHaveBeenCalledWith("q1", { answer: "Neither: drop the marks" }));
  });

  it("goes back in progress once answered, with a toast naming the agent that collects the answer", async () => {
    waitingOn(QUESTION);
    renderEditor();
    const block = await openRequest();
    expect(status().textContent).not.toBe("In Progress");
    fireEvent.click(within(block).getByRole("radio", { name: "Round only" }));
    fireEvent.click(within(block).getByRole("button", { name: "Send answer" }));
    const toast = await screen.findByTestId("answer-toast");
    expect(toast.getAttribute("role")).toBe("status");
    expect(toast.textContent).toBe("Answer sent. The implementer (gpt-5) gets it on its next call.");
    await waitFor(() => expect(status().textContent).toBe("In Progress"));
    expect(screen.queryByTestId("open-request")).toBeNull();
    // Answering edits nothing of the ticket's own.
    expect(screen.queryByRole("button", { name: "Save Changes" })).toBeNull();
    // The answered question joins the earlier requests.
    fireEvent.click(screen.getByRole("button", { name: /Earlier requests \(1\)/ }));
    expect(screen.getByTestId("earlier-request").textContent).toContain("Answer: Round only by bilal");
  });

  it("keeps the form, and says why, when the answer is refused", async () => {
    waitingOn(QUESTION);
    mockApi.requests.answer.mockRejectedValue(new Error("API error 400: request q1 is already answered, by bilal"));
    renderEditor();
    const block = await openRequest();
    fireEvent.change(within(block).getByRole("textbox"), { target: { value: "temp" } });
    fireEvent.click(within(block).getByRole("button", { name: "Send answer" }));
    expect((await within(block).findByRole("alert")).textContent).toMatch(/already answered/);
    expect(screen.queryByTestId("answer-toast")).toBeNull();
    expect((within(block).getByRole("textbox") as HTMLTextAreaElement).value).toBe("temp");
  });

  it("offers only your own words when it has no choices", async () => {
    waitingOn({ ...QUESTION, choices: [] });
    renderEditor();
    const block = await openRequest();
    expect(within(block).queryByRole("radiogroup")).toBeNull();
    expect(within(block).queryByText("or answer in your own words")).toBeNull();
    fireEvent.change(within(block).getByRole("textbox", { name: "Your answer" }), { target: { value: "Both" } });
    fireEvent.click(within(block).getByRole("button", { name: "Send answer" }));
    await waitFor(() => expect(mockApi.requests.answer).toHaveBeenCalledWith("q1", { answer: "Both" }));
  });
});

describe("an open approval", () => {
  it("shows the prompt with Approve and Decline, and no choices", async () => {
    waitingOn(APPROVAL);
    renderEditor();
    const block = await openRequest();
    expect(block.getAttribute("aria-label")).toBe("Approval asked by the implementer");
    // The type's tag, and the button.
    expect(within(block).getAllByText("Approve")).toHaveLength(2);
    expect(within(block).getByText("Decisions").tagName).toBe("STRONG");
    expect(within(block).queryByRole("radiogroup")).toBeNull();
    expect(within(block).queryByRole("button", { name: "Send answer" })).toBeNull();
    expect(within(block).getByRole("button", { name: "Approve" })).toBeTruthy();
    expect(within(block).getByRole("button", { name: "Decline" })).toBeTruthy();
    expect(within(block).getByRole("textbox", { name: "Note to the agent (optional)" })).toBeTruthy();
  });

  it("approves without a note", async () => {
    waitingOn(APPROVAL);
    renderEditor();
    const block = await openRequest();
    fireEvent.click(within(block).getByRole("button", { name: "Approve" }));
    await waitFor(() => expect(mockApi.requests.answer).toHaveBeenCalledWith("ap1", { answer: "approved" }));
    expect((await screen.findByTestId("answer-toast")).textContent).toBe(
      "Approved. The implementer (gpt-5) gets it on its next call.",
    );
  });

  it("sends the note with Approve", async () => {
    waitingOn(APPROVAL);
    renderEditor();
    const block = await openRequest();
    fireEvent.change(within(block).getByRole("textbox"), { target: { value: " keep the history " } });
    fireEvent.click(within(block).getByRole("button", { name: "Approve" }));
    await waitFor(() =>
      expect(mockApi.requests.answer).toHaveBeenCalledWith("ap1", { answer: "approved", note: "keep the history" }),
    );
  });

  it("sends the note with Decline, and shows it in the history afterwards", async () => {
    waitingOn(APPROVAL, EARLIER);
    renderEditor();
    const block = await openRequest();
    fireEvent.change(within(block).getByRole("textbox"), { target: { value: "Keep it until ACP-209 lands" } });
    fireEvent.click(within(block).getByRole("button", { name: "Decline" }));
    await waitFor(() =>
      expect(mockApi.requests.answer).toHaveBeenCalledWith("ap1", { answer: "declined", note: "Keep it until ACP-209 lands" }),
    );
    expect((await screen.findByTestId("answer-toast")).textContent).toBe(
      "Declined. The implementer (gpt-5) gets it on its next call.",
    );
    fireEvent.click(screen.getByRole("button", { name: /Earlier requests \(3\)/ }));
    const newest = screen.getAllByTestId("earlier-request")[0];
    expect(newest.textContent).toContain("Declined by bilal");
    expect(within(newest).getByTestId("request-note").textContent).toBe("Note: Keep it until ACP-209 lands");
  });

  it("names the agent that asked when it isn't the ticket's holder", async () => {
    waitingOn({ ...APPROVAL, agentId: "orch" });
    mockApi.agents.get.mockResolvedValue({ ...IMPLEMENTER, id: "orch", role: "orchestrator", model: "opus-5.5", provider: "anthropic" });
    renderEditor();
    const block = await openRequest();
    await waitFor(() => expect(block.getAttribute("aria-label")).toBe("Approval asked by the orchestrator"));
    expect(mockApi.agents.get).toHaveBeenCalledWith("orch");
    fireEvent.click(within(block).getByRole("button", { name: "Approve" }));
    expect((await screen.findByTestId("answer-toast")).textContent).toBe(
      "Approved. The orchestrator (opus-5.5) gets it on its next call.",
    );
  });
});

describe("the block's form", () => {
  it("follows the request's type, and says so for a type it can't answer yet", async () => {
    waitingOn({ ...QUESTION, type: "pick_file" });
    renderEditor();
    const block = await openRequest();
    expect(block.getAttribute("data-type")).toBe("pick_file");
    expect(within(block).getByText(/can.t answer a .pick file. request yet/)).toBeTruthy();
    expect(within(block).queryByRole("button")).toBeNull();
  });

  it("can't be answered on a deleted ticket", async () => {
    waitingOn(APPROVAL);
    renderEditor({ deleted: true });
    const block = await openRequest();
    expect(block.closest("[inert]")).not.toBeNull();
  });
});

describe("request history", () => {
  it("folds the earlier requests under the open one, newest first, with type, prompt, answer, note and who answered", async () => {
    waitingOn(QUESTION, EARLIER);
    renderEditor();
    const block = await openRequest();
    const fold = await screen.findByRole("button", { name: /Earlier requests \(2\)/ });
    expect(fold.getAttribute("aria-expanded")).toBe("false");
    expect(block.compareDocumentPosition(fold) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(screen.queryByTestId("earlier-request")).toBeNull();
    fireEvent.click(fold);
    const rows = screen.getAllByTestId("earlier-request");
    expect(rows.map((r) => r.querySelector("p")?.textContent)).toEqual([
      "Land Review 1's fixes on main?",
      "Which test DB?",
    ]);
    expect(rows[0].textContent).toContain("Approve");
    expect(rows[0].textContent).toContain("Approved by bilal");
    expect(within(rows[0]).getByTestId("request-note").textContent).toBe("Note: after the rebase");
    expect(rows[1].textContent).toContain("Question");
    expect(rows[1].textContent).toContain("Answer: A fresh t.TempDir() per test by bilal");
    expect(within(rows[1]).queryByTestId("request-note")).toBeNull();
  });

  it("sits at the top of the page when no request is open", async () => {
    waitingOn(undefined, EARLIER);
    renderEditor();
    const fold = await screen.findByRole("button", { name: /Earlier requests \(2\)/ });
    expect(screen.queryByTestId("open-request")).toBeNull();
    const title = within(content()).getByRole("textbox", { name: "Title" });
    expect(fold.compareDocumentPosition(title) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("shows nothing for a ticket that never asked", async () => {
    waitingOn(undefined, []);
    renderEditor();
    await waitFor(() => expect(mockApi.requests.list).toHaveBeenCalled());
    expect(screen.queryByTestId("ticket-requests")).toBeNull();
  });

  it("picks up a request answered elsewhere on the next live change", async () => {
    waitingOn(undefined, [EARLIER[0]]);
    renderEditor();
    await screen.findByRole("button", { name: /Earlier requests \(1\)/ });
    serverRequests = EARLIER;
    await fireLive();
    await screen.findByRole("button", { name: /Earlier requests \(2\)/ });
  });
});

// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { Board as BoardData, Project, Ticket, TicketRequest } from "../api/client";
import { memoryStorage } from "../test/memoryStorage";

// Kanban's Waiting on You column: its own red-tinted column between In
// Progress and Agent Review, the longest wait first, which takes no drops and
// no new tickets. The API is mocked.

const mockApi = vi.hoisted(() => ({
  tickets: { list: vi.fn(), get: vi.fn(), move: vi.fn() },
  projects: { list: vi.fn() },
  labels: { list: vi.fn() },
  epics: { list: vi.fn() },
  board: { get: vi.fn() },
}));
vi.mock("../api/client", () => ({ api: mockApi }));

vi.mock("../hooks/useLiveRefresh", () => ({ useLiveRefresh: () => {} }));

// jsdom lays nothing out, so a pointer drag can't find a drop target. The
// board's DndContext is wrapped to hand its drag handlers to the test, which
// calls them as dnd-kit would at the end of a drag.
type DragHandler = (event: { active: { id: string }; over: { id: string } | null }) => unknown;
const drag = vi.hoisted(() => ({ handlers: {} as Record<"onDragStart" | "onDragOver" | "onDragEnd", DragHandler> }));
vi.mock("@dnd-kit/core", async () => {
  const actual = await vi.importActual<typeof import("@dnd-kit/core")>("@dnd-kit/core");
  const { createElement } = await import("react");
  return {
    ...actual,
    DndContext: (props: Parameters<typeof actual.DndContext>[0]) => {
      Object.assign(drag.handlers, props);
      return createElement(actual.DndContext, props);
    },
  };
});

import Board from "./Board";

const ACP: Project = {
  id: "p-ACP",
  name: "ACP",
  prefix: "ACP",
  description: "",
  icon: "",
  color: "",
  status: "active",
  createdAt: "",
  updatedAt: "",
};

const request = (ticketId: string, type: TicketRequest["type"], prompt: string, createdAt: string): TicketRequest => ({
  id: `r-${ticketId}`,
  ticketId,
  agentId: "a1",
  type,
  prompt,
  choices: [],
  createdAt,
});

function ticket(number: number, status: string, over: Partial<Ticket> = {}): Ticket {
  return {
    id: `ACP-${number}`,
    projectId: "p-ACP",
    number,
    title: `Ticket ${number}`,
    description: "",
    status,
    priority: "medium",
    position: number,
    createdAt: "",
    updatedAt: "",
    projectPrefix: "ACP",
    repos: [],
    labels: [],
    subtasks: [],
    dependsOn: [],
    blocks: [],
    ...over,
  };
}

// The server's columns, in its order; ACP-5 asked before ACP-4 but sits
// later in the column the server sends.
function board(waiting: Ticket[]): BoardData {
  return {
    projectId: "",
    columns: [
      { status: "todo", tickets: [ticket(1, "todo")] },
      { status: "in_progress", tickets: [ticket(2, "in_progress")] },
      { status: "needs_user_input", tickets: waiting },
      { status: "agent_review", tickets: [ticket(3, "agent_review")] },
      { status: "done", tickets: [] },
    ],
  };
}

const WAITING = [
  ticket(4, "needs_user_input", {
    openRequest: request("ACP-4", "question", "Round only, or the finding too?", "2026-09-28T10:05:00Z"),
  }),
  ticket(5, "needs_user_input", {
    openRequest: request("ACP-5", "approval", "Delete the Decisions document?", "2026-09-28T10:00:00Z"),
  }),
];

async function mount(data: BoardData) {
  mockApi.board.get.mockResolvedValue(data);
  render(
    <MemoryRouter initialEntries={["/kanban?project=ACP"]}>
      <Board />
    </MemoryRouter>,
  );
  for (let i = 0; i < 4; i++) await act(async () => {});
}

const column = (status: string) => screen.getByTestId(`board-column-${status}`);

beforeEach(() => {
  vi.stubGlobal("localStorage", memoryStorage());
  mockApi.projects.list.mockResolvedValue([ACP]);
  mockApi.labels.list.mockResolvedValue([]);
  mockApi.epics.list.mockResolvedValue({ epics: [], noEpic: { counts: {}, total: 0, complete: false, lastActivityAt: null } });
});

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
  vi.unstubAllGlobals();
});

describe("Kanban's Waiting on You column", () => {
  it("sits between In Progress and Agent Review, with a red heading and no lane tint", async () => {
    await mount(board(WAITING));
    const headings = screen.getAllByRole("heading", { level: 3 }).map((h) => h.textContent);
    expect(headings).toEqual(["Todo", "In Progress", "Waiting on You", "Agent Review", "Done"]);
    const waiting = column("needs_user_input");
    expect(waiting.className).not.toContain("bg-red-500/5");
    expect(waiting.className).not.toContain("ring-red-500/25");
    expect(within(waiting).getByRole("heading").className).toContain("text-red-400");
    expect(within(waiting).getByText("2")).toBeTruthy();
    expect(column("in_progress").className).not.toContain("bg-red-500/5");
  });

  it("lists the longest wait first, each card red with its request", async () => {
    await mount(board(WAITING));
    const waiting = column("needs_user_input");
    const bands = within(waiting).getAllByTestId("card-request").map((b) => b.textContent);
    expect(bands).toEqual(["ApproveDelete the Decisions document?", "QuestionRound only, or the finding too?"]);
    // Kanban stays still: the red is enough there.
    expect(waiting.querySelector(".graph-attention-waiting")).toBeNull();
  });

  it("offers no new ticket in it, and its cards don't drag", async () => {
    await mount(board(WAITING));
    expect(screen.queryByRole("button", { name: "New ticket in Waiting on You" })).toBeNull();
    expect(screen.getByRole("button", { name: "New ticket in In Progress" })).toBeTruthy();
    const cards = within(column("needs_user_input")).getAllByText(/^Ticket \d$/).map(
      (title) => title.closest("[aria-roledescription='draggable']") as HTMLElement,
    );
    expect(cards).toHaveLength(2);
    for (const card of cards) expect(card.getAttribute("aria-disabled")).toBe("true");
    const todo = within(column("todo")).getByText("Ticket 1").closest("[aria-roledescription='draggable']");
    expect(todo?.getAttribute("aria-disabled")).toBe("false");
  });

  // A drag of ACP-1 from Todo that ends over `over`, start to end.
  async function dropOn(over: string) {
    const active = { id: "ACP-1" };
    await act(async () => {
      drag.handlers.onDragStart({ active, over: null });
      drag.handlers.onDragOver({ active, over: { id: over } });
    });
    await act(async () => {
      await drag.handlers.onDragEnd({ active, over: { id: over } });
    });
  }

  it("refuses a card dropped on it, or on one of its cards, and leaves the card where it was", async () => {
    await mount(board(WAITING));
    for (const over of ["needs_user_input", "ACP-5"]) {
      await dropOn(over);
      expect(mockApi.tickets.move).not.toHaveBeenCalled();
      expect(within(column("todo")).getByText("Ticket 1")).toBeTruthy();
      expect(within(column("needs_user_input")).queryByText("Ticket 1")).toBeNull();
      expect(within(column("needs_user_input")).getAllByTestId("card-request")).toHaveLength(2);
    }
  });

  it("still takes a card dropped on a writable column", async () => {
    mockApi.tickets.move.mockResolvedValue(ticket(1, "in_progress"));
    await mount(board(WAITING));
    await dropOn("in_progress");
    expect(mockApi.tickets.move).toHaveBeenCalledWith("ACP-1", "in_progress");
    expect(within(column("in_progress")).getByText("Ticket 1")).toBeTruthy();
  });

  it("says nothing waits when it's empty, rather than asking for drops", async () => {
    await mount(board([]));
    const waiting = column("needs_user_input");
    expect(within(waiting).getByText("Nothing waiting on you")).toBeTruthy();
    expect(within(waiting).queryByText("Drop tickets here")).toBeNull();
    expect(within(column("done")).getByText("Drop tickets here")).toBeTruthy();
  });
});

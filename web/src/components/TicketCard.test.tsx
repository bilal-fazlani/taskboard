// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import type { Ticket } from "../api/client";
import TicketCard, { type GraphCardInfo } from "./TicketCard";

afterEach(cleanup);

function makeTicket(overrides: Partial<Ticket> = {}): Ticket {
  return {
    id: "t1",
    projectId: "p1",
    number: 7,
    title: "Ship login page",
    description: "",
    status: "todo",
    priority: "high",
    position: 0,
    createdAt: "",
    updatedAt: "",
    projectPrefix: "AUTH",
    labels: [],
    subtasks: [],
    ...overrides,
  };
}

describe("TicketCard on the graph", () => {
  it("shows done against the total, not just the done count", () => {
    // Mirrors ACP-84: an arrow from an unfinished blocker must not read as
    // if that blocker were done.
    const graph: GraphCardInfo = { satisfiedDependencyCount: 1, externalBlockerCount: 0, dependencyTotal: 2 };
    render(<TicketCard ticket={makeTicket()} graph={graph} />);
    expect(screen.getByText("1 of 2 dependencies done")).toBeTruthy();
  });

  it("shows no done line when nothing is done, even with a total", () => {
    const graph: GraphCardInfo = { satisfiedDependencyCount: 0, externalBlockerCount: 0, dependencyTotal: 2 };
    render(<TicketCard ticket={makeTicket()} graph={graph} />);
    expect(screen.queryByText(/dependenc(y|ies) done/)).toBeNull();
  });

  it("keeps the hidden-blocker line unaffected by the total", () => {
    const graph: GraphCardInfo = { satisfiedDependencyCount: 1, externalBlockerCount: 2, dependencyTotal: 4 };
    render(<TicketCard ticket={makeTicket()} graph={graph} />);
    expect(screen.getByText("1 of 4 dependencies done")).toBeTruthy();
    expect(screen.getByText("2 hidden blockers")).toBeTruthy();
  });
});

// The card's header: the key on the left and the priority on the right, with
// the ticket's epic before the key when it has one. Kanban and Dependencies
// share the card; Dependencies adds a status dot first.

const headerTicket = (overrides: Partial<Ticket> = {}): Ticket => ({
  id: "t1",
  projectId: "p1",
  number: 67,
  title: "Open editor turns read-only when its ticket is deleted",
  description: "",
  status: "in_progress",
  priority: "medium",
  position: 0,
  createdAt: "",
  updatedAt: "",
  projectPrefix: "ACP",
  labels: [],
  subtasks: [],
  ...overrides,
});

const GRAPH: GraphCardInfo = { satisfiedDependencyCount: 0, externalBlockerCount: 0, dependencyTotal: 0 };
const epic = (name: string) => ({ id: "e1", name });

// The header row: the card's first child.
const header = (container: HTMLElement) => container.firstElementChild!.firstElementChild as HTMLElement;
// The header's left side, which holds everything but the priority.
const left = (container: HTMLElement) => header(container).firstElementChild as HTMLElement;

describe("TicketCard's header", () => {
  it("shows the epic's icon and name, a slash, then the key, with the priority on the right", () => {
    const { container } = render(<TicketCard ticket={headerTicket({ epic: epic("Views") })} />);
    expect(left(container).textContent).toBe("Views/ACP-67");
    const crumb = screen.getByTestId("card-epic");
    expect(crumb.querySelector("svg")).not.toBeNull();
    expect(header(container).lastElementChild!.textContent).toBe("medium");
  });

  it("keeps the epic grey like the key: no colour of its own", () => {
    const { container } = render(<TicketCard ticket={headerTicket({ epic: epic("Views") })} />);
    expect(left(container).className).toContain("text-slate-500");
    const crumb = screen.getByTestId("card-epic");
    expect(crumb.getAttribute("style")).toBeNull();
    expect(crumb.querySelector("[style]")).toBeNull();
  });

  it("is exactly as before without an epic", () => {
    const { container } = render(<TicketCard ticket={headerTicket()} />);
    expect(screen.queryByTestId("card-epic")).toBeNull();
    expect(left(container).outerHTML).toBe('<span class="text-[11px] font-mono text-slate-500">ACP-67</span>');
  });

  it("is exactly as before on the graph without an epic: the dot, then the key", () => {
    const { container } = render(<TicketCard ticket={headerTicket()} graph={GRAPH} />);
    expect(screen.queryByTestId("card-epic")).toBeNull();
    const side = left(container);
    expect(side.className).toBe("inline-flex items-center gap-1.5 text-[11px] font-mono text-slate-500");
    expect(side.children).toHaveLength(1);
    expect(side.firstElementChild!.getAttribute("title")).toBe("In Progress");
    expect(side.textContent).toBe("ACP-67");
  });

  it("puts the graph's status dot first, before the epic", () => {
    const { container } = render(<TicketCard ticket={headerTicket({ epic: epic("Views") })} graph={GRAPH} />);
    const [dot, crumb] = Array.from(left(container).children);
    expect(dot.getAttribute("title")).toBe("In Progress");
    expect(crumb).toBe(screen.getByTestId("card-epic"));
    expect(left(container).textContent).toBe("Views/ACP-67");
  });

  it("cuts a long epic name short at a fixed width and shows it in full on hover", () => {
    const long = "Agent review and status history for every ticket";
    render(<TicketCard ticket={headerTicket({ epic: epic(long) })} />);
    const crumb = screen.getByTestId("card-epic");
    expect(crumb.getAttribute("title")).toBe(long);
    expect(crumb.className).toMatch(/\bmax-w-\[8rem\]/);
    expect(crumb.className).toMatch(/\bmin-w-0\b/);
    const name = crumb.querySelector("span")!;
    expect(name.textContent).toBe(long);
    expect(name.className).toMatch(/\btruncate\b/);
  });

  it("never cuts the key short", () => {
    const { container } = render(<TicketCard ticket={headerTicket({ epic: epic("A very long epic name indeed") })} />);
    const key = Array.from(left(container).children).at(-1)!;
    expect(key.textContent).toBe("ACP-67");
    expect(key.className).toMatch(/\bshrink-0\b/);
    expect(key.className).toMatch(/\bwhitespace-nowrap\b/);
    expect(key.className).not.toMatch(/\btruncate\b/);
  });
});

// The graph card counts the ticket's trips to the review agent in a violet
// pill under the title; the board card never shows it.
describe("TicketCard's review rounds", () => {
  it("shows a violet review ×N pill on its own row below the title on the graph", () => {
    const { container } = render(<TicketCard ticket={makeTicket({ reviewRounds: 3 })} graph={GRAPH} />);
    const pill = screen.getByTestId("card-review-rounds");
    expect(pill.textContent).toBe("review ×3");
    expect(pill.className).toMatch(/\bbg-violet-500\/20\b/);
    expect(pill.className).toMatch(/\btext-violet-400\b/);
    expect(pill.className).toMatch(/\brounded-full\b/);
    // Its own row, straight after the title.
    const row = pill.parentElement!;
    expect(row.parentElement).toBe(container.firstElementChild);
    expect(row.previousElementSibling!.textContent).toBe("Ship login page");
  });

  it("shows it for one round too", () => {
    render(<TicketCard ticket={makeTicket({ reviewRounds: 1 })} graph={GRAPH} />);
    expect(screen.getByTestId("card-review-rounds").textContent).toBe("review ×1");
  });

  it("stays after the ticket is done", () => {
    render(<TicketCard ticket={makeTicket({ status: "done", reviewRounds: 2 })} graph={GRAPH} />);
    expect(screen.getByTestId("card-review-rounds").textContent).toBe("review ×2");
  });

  it("is left out with no rounds, or none reported", () => {
    render(<TicketCard ticket={makeTicket({ reviewRounds: 0 })} graph={GRAPH} />);
    render(<TicketCard ticket={makeTicket()} graph={GRAPH} />);
    expect(screen.queryByTestId("card-review-rounds")).toBeNull();
  });

  it("never shows on the board card", () => {
    render(<TicketCard ticket={makeTicket({ reviewRounds: 4 })} />);
    expect(screen.queryByTestId("card-review-rounds")).toBeNull();
    expect(screen.queryByText(/review ×/)).toBeNull();
  });
});

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

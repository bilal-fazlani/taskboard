import { describe, expect, it } from "vitest";
import {
  TICKET_PARAM,
  findTicket,
  ticketRef,
  ticketRefFor,
  ticketUrl,
  withTicket,
  withoutTicket,
  type TicketIdentity,
} from "./ticketParam";

function ticket(overrides: Partial<TicketIdentity> = {}): TicketIdentity {
  return { id: "01M2JHD70CF0GS3E956046R338", number: 25, projectPrefix: "ACP", ...overrides };
}

describe("ticketRef", () => {
  it("reads the ticket parameter", () => {
    expect(ticketRef(new URLSearchParams("status=todo&ticket=ACP-25"))).toBe("ACP-25");
  });

  it("is empty when no ticket is open", () => {
    expect(ticketRef(new URLSearchParams("status=todo"))).toBe("");
  });
});

describe("withTicket", () => {
  it("sets the ticket parameter and keeps the filters", () => {
    const params = withTicket(new URLSearchParams("project=ACP&q=url"), ticket());
    expect(params.get(TICKET_PARAM)).toBe("ACP-25");
    expect(params.get("project")).toBe("ACP");
    expect(params.get("q")).toBe("url");
  });

  it("replaces a ticket that was already open", () => {
    const params = withTicket(new URLSearchParams("ticket=ACP-7&status=todo"), ticket());
    expect(params.getAll(TICKET_PARAM)).toEqual(["ACP-25"]);
    expect(params.get("status")).toBe("todo");
  });

  it("does not touch the params it was given", () => {
    const params = new URLSearchParams("status=todo");
    withTicket(params, ticket());
    expect(params.toString()).toBe("status=todo");
  });

  it("falls back to the id when the ticket has no project prefix", () => {
    const params = withTicket(new URLSearchParams(), ticket({ projectPrefix: "" }));
    expect(params.get(TICKET_PARAM)).toBe("01M2JHD70CF0GS3E956046R338");
  });
});

describe("withoutTicket", () => {
  it("removes only the ticket parameter", () => {
    const params = withoutTicket(new URLSearchParams("project=ACP&ticket=ACP-25&status=todo&q=url"));
    expect(params.toString()).toBe("project=ACP&status=todo&q=url");
  });

  it("leaves params with no ticket alone", () => {
    const params = withoutTicket(new URLSearchParams("project=ACP&status=todo"));
    expect(params.toString()).toBe("project=ACP&status=todo");
  });

  it("does not touch the params it was given", () => {
    const params = new URLSearchParams("ticket=ACP-25");
    withoutTicket(params);
    expect(params.toString()).toBe("ticket=ACP-25");
  });
});

describe("findTicket", () => {
  const tickets = [ticket({ id: "a", number: 7 }), ticket({ id: "b", number: 25 })];

  it("finds a ticket by display key", () => {
    expect(findTicket(tickets, "ACP-25")?.id).toBe("b");
  });

  it("finds a ticket by display key whatever the case", () => {
    expect(findTicket(tickets, "acp-7")?.id).toBe("a");
  });

  it("finds a ticket by id", () => {
    expect(findTicket(tickets, "b")?.id).toBe("b");
  });

  it("ignores surrounding space", () => {
    expect(findTicket(tickets, " ACP-7 ")?.id).toBe("a");
  });

  it("finds nothing for a key no loaded ticket carries", () => {
    expect(findTicket(tickets, "ACP-999")).toBeUndefined();
  });

  it("finds nothing for an empty reference", () => {
    expect(findTicket(tickets, "")).toBeUndefined();
  });

  it("finds nothing among no tickets", () => {
    expect(findTicket([], "ACP-25")).toBeUndefined();
  });

  it("prefers an id match over a key match", () => {
    const shadowed = [ticket({ id: "ACP-25", number: 1 }), ticket({ id: "other", number: 25 })];
    expect(findTicket(shadowed, "ACP-25")?.id).toBe("ACP-25");
  });
});

describe("ticketUrl", () => {
  it("is the home view with the ticket parameter", () => {
    expect(ticketUrl("http://localhost:3010", ticket())).toBe("http://localhost:3010/?ticket=ACP-25");
  });

  it("escapes a reference that needs it", () => {
    expect(ticketUrl("http://localhost:3010", ticket({ projectPrefix: "A B" }))).toBe(
      "http://localhost:3010/?ticket=A%20B-25",
    );
  });
});

describe("ticketRefFor", () => {
  it("is the display key", () => {
    expect(ticketRefFor(ticket())).toBe("ACP-25");
  });
});

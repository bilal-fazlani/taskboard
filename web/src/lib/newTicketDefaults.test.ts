import { describe, expect, it } from "vitest";
import { EMPTY_FILTERS } from "./filters";
import { newTicketDefaults } from "./newTicketDefaults";

// Listed newest first, as the API does.
const PROJECTS = [
  { id: "p-ldr", prefix: "LDR" },
  { id: "p-acp", prefix: "ACP" },
];

const on = (project: string) => ({ ...EMPTY_FILTERS, project });

describe("newTicketDefaults", () => {
  it("starts in the project the view shows", () => {
    expect(newTicketDefaults(on("ACP"), PROJECTS)).toEqual({ projectId: "p-acp" });
  });

  it("matches the view's project ignoring case, as the filters do", () => {
    expect(newTicketDefaults(on("acp"), PROJECTS).projectId).toBe("p-acp");
  });

  it("falls back to the first project when the view shows none", () => {
    expect(newTicketDefaults(on(""), PROJECTS).projectId).toBe("p-ldr");
  });

  it("falls back to the first project when the view's names none that exists", () => {
    expect(newTicketDefaults(on("GONE"), PROJECTS).projectId).toBe("p-ldr");
  });

  it("has no project when there are none", () => {
    expect(newTicketDefaults(on("ACP"), [])).toEqual({ projectId: "" });
  });
});

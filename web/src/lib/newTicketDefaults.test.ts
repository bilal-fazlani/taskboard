import { describe, expect, it } from "vitest";
import { EMPTY_FILTERS } from "./filters";
import { newTicketDefaults } from "./newTicketDefaults";

// Listed newest first, as the API does.
const PROJECTS = [
  { id: "p-ldr", prefix: "LDR" },
  { id: "p-acp", prefix: "ACP" },
];

const on = (project: string, epic = "") => ({ ...EMPTY_FILTERS, project, epic });
const ACP_EPICS = {
  projectId: "p-acp",
  epics: [
    { id: "e-views", name: "Views" },
    { id: "e-agents", name: "Agents" },
  ],
};

describe("newTicketDefaults", () => {
  it("starts in the project the view shows", () => {
    expect(newTicketDefaults(on("ACP"), PROJECTS)).toEqual({ projectId: "p-acp", epicId: "" });
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
    expect(newTicketDefaults(on("ACP"), [])).toEqual({ projectId: "", epicId: "" });
  });

  it("starts on the epic the view's filter names, ignoring case", () => {
    expect(newTicketDefaults(on("ACP", "Views"), PROJECTS, ACP_EPICS)).toEqual({ projectId: "p-acp", epicId: "e-views" });
    expect(newTicketDefaults(on("ACP", "agents"), PROJECTS, ACP_EPICS).epicId).toBe("e-agents");
  });

  it("starts on no epic without an epic filter, or with the one for tickets without an epic", () => {
    expect(newTicketDefaults(on("ACP"), PROJECTS, ACP_EPICS).epicId).toBe("");
    expect(newTicketDefaults(on("ACP", "none"), PROJECTS, ACP_EPICS).epicId).toBe("");
    expect(newTicketDefaults(on("ACP", "NONE"), PROJECTS, ACP_EPICS).epicId).toBe("");
  });

  it("starts on no epic until the project's epics have loaded, or when they don't have it", () => {
    expect(newTicketDefaults(on("ACP", "Views"), PROJECTS).epicId).toBe("");
    expect(newTicketDefaults(on("ACP", "Fleet"), PROJECTS, ACP_EPICS).epicId).toBe("");
  });

  it("never takes an epic from another project's list", () => {
    expect(newTicketDefaults(on("LDR", "Views"), PROJECTS, ACP_EPICS)).toEqual({ projectId: "p-ldr", epicId: "" });
  });
});

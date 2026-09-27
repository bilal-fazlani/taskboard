import { describe, expect, it } from "vitest";
import { EMPTY_FILTERS } from "./filters";
import { newTicketBlocked, newTicketDefaults } from "./newTicketDefaults";

// Listed newest first, as the API does; names sort ACP before LDR, unlike the
// API order, so the fallback tests can tell "first by name" apart from
// "first listed".
const PROJECTS = [
  { id: "p-ldr", prefix: "LDR", name: "Leaderboard" },
  { id: "p-acp", prefix: "ACP", name: "Agent control plane" },
];

const on = (project: string, ...epic: string[]) => ({ ...EMPTY_FILTERS, project, epic });
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

  it("falls back to the first project by name, not by API order, when the view shows none", () => {
    expect(newTicketDefaults(on(""), PROJECTS).projectId).toBe("p-acp");
  });

  it("falls back to the first project by name when the view's names none that exists", () => {
    expect(newTicketDefaults(on("GONE"), PROJECTS).projectId).toBe("p-acp");
  });

  it("has no project when there are none", () => {
    expect(newTicketDefaults(on("ACP"), [])).toEqual({ projectId: "", epicId: "" });
    expect(newTicketDefaults(on(""), [])).toEqual({ projectId: "", epicId: "" });
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

  it("starts on no epic when the filter names several, which says nothing about one", () => {
    expect(newTicketDefaults(on("ACP", "Views", "agents"), PROJECTS, ACP_EPICS).epicId).toBe("");
    expect(newTicketDefaults(on("ACP", "none", "Views"), PROJECTS, ACP_EPICS).epicId).toBe("");
  });

  it("starts on no epic until the project's epics have loaded, or when they don't have it", () => {
    expect(newTicketDefaults(on("ACP", "Views"), PROJECTS).epicId).toBe("");
    expect(newTicketDefaults(on("ACP", "Fleet"), PROJECTS, ACP_EPICS).epicId).toBe("");
  });

  it("never takes an epic from another project's list", () => {
    expect(newTicketDefaults(on("LDR", "Views"), PROJECTS, ACP_EPICS)).toEqual({ projectId: "p-ldr", epicId: "" });
  });
});

describe("newTicketBlocked", () => {
  it("lets a view open the form once there is a project", () => {
    expect(newTicketBlocked(PROJECTS)).toBeNull();
  });

  it("asks for a project when there is none", () => {
    expect(newTicketBlocked([])).toBe("Create a project to add tickets.");
  });

  it("waits for the projects to load, or says the load failed", () => {
    expect(newTicketBlocked(null)).toBe("Loading projects…");
    expect(newTicketBlocked(null, true)).toBe("Couldn't load projects. Retrying…");
  });

  it("names epics in the reason when asked for New epic", () => {
    expect(newTicketBlocked(PROJECTS, false, "epics")).toBeNull();
    expect(newTicketBlocked([], false, "epics")).toBe("Create a project to add epics.");
    expect(newTicketBlocked(null, false, "epics")).toBe("Loading projects…");
  });
});

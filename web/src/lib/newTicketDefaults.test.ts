import { describe, expect, it } from "vitest";
import { EMPTY_FILTERS } from "./filters";
import { newTicketBlocked, newTicketDefaults } from "./newTicketDefaults";

// Listed newest first, as the API does; names sort ACP before LDR, unlike the
// API order, so the fallback tests can tell "first active by name" apart from
// "first listed".
const PROJECTS = [
  { id: "p-ldr", prefix: "LDR", name: "Leaderboard", status: "active" },
  { id: "p-acp", prefix: "ACP", name: "Agent control plane", status: "active" },
];
// An archived project, listed first, whose name would also sort first: never
// picked, whether by name match or by fallback.
const OLD = { id: "p-old", prefix: "OLD", name: "Aaa archived", status: "archived" };

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

  it("falls back to the first active project by name, not by API order, when the view shows none", () => {
    expect(newTicketDefaults(on(""), PROJECTS).projectId).toBe("p-acp");
  });

  it("falls back to the first active project by name when the view's names none that exists", () => {
    expect(newTicketDefaults(on("GONE"), PROJECTS).projectId).toBe("p-acp");
  });

  it("has no project when there are none", () => {
    expect(newTicketDefaults(on("ACP"), [])).toEqual({ projectId: "", epicId: "" });
  });

  it("never picks an archived project: not by name match, and not as a fallback", () => {
    const projects = [OLD, ...PROJECTS];
    // The view names the archived project directly: it is not offered, so the
    // form falls back to the first active project instead of honoring it.
    expect(newTicketDefaults(on("OLD"), projects).projectId).toBe("p-acp");
    // The view names none: same fallback, the archived project never wins
    // even though it sorts first by name.
    expect(newTicketDefaults(on(""), projects).projectId).toBe("p-acp");
  });

  it("has no project when every project is archived", () => {
    expect(newTicketDefaults(on(""), [OLD]).projectId).toBe("");
    expect(newTicketDefaults(on("OLD"), [OLD]).projectId).toBe("");
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
  it("lets a view on an active project open the form", () => {
    expect(newTicketBlocked("ACP", [OLD, ...PROJECTS])).toBeNull();
    expect(newTicketBlocked("acp", PROJECTS)).toBeNull();
  });

  it("names the archived project a view shows, matched ignoring case", () => {
    const reason = "Aaa archived is archived. Unarchive it to add tickets.";
    expect(newTicketBlocked("OLD", [OLD, ...PROJECTS])).toBe(reason);
    expect(newTicketBlocked("old", [OLD, ...PROJECTS])).toBe(reason);
    // Also when it is the only project: the archived one is the reason given.
    expect(newTicketBlocked("OLD", [OLD])).toBe(reason);
  });

  it("asks for a project when none is active", () => {
    expect(newTicketBlocked("", [OLD])).toBe("Create a project to add tickets.");
    expect(newTicketBlocked("", [])).toBe("Create a project to add tickets.");
  });

  it("waits for the projects to load", () => {
    expect(newTicketBlocked("ACP", null)).toBe("Loading projects…");
  });
});

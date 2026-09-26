// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, createEvent, fireEvent, render, screen } from "@testing-library/react";
import type { Project } from "../api/client";

const mockApi = vi.hoisted(() => ({
  epics: { list: vi.fn(() => Promise.resolve({ epics: [], noEpic: {} })) },
  labels: { list: vi.fn(() => Promise.resolve([])) },
  documents: { createImage: vi.fn() },
}));
vi.mock("../api/client", () => ({ api: mockApi }));

import CreateTicketModal from "./CreateTicketModal";
import { EMPTY_FILTERS } from "../lib/filters";

const project: Project = {
  id: "p1", name: "Auth", prefix: "AUTH", description: "", icon: "", color: "#3b82f6", status: "active", createdAt: "", updatedAt: "",
};

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("CreateTicketModal: project dropdown follows the filter bar's rules", () => {
  const alpha: Project = {
    id: "p-alpha", name: "Alpha", prefix: "ALP", description: "", icon: "", color: "#3b82f6", status: "active", createdAt: "", updatedAt: "",
  };
  const bravo: Project = {
    id: "p-bravo", name: "Bravo", prefix: "BRA", description: "", icon: "", color: "#3b82f6", status: "active", createdAt: "", updatedAt: "",
  };
  const archived: Project = {
    id: "p-old", name: "Zzz archived", prefix: "OLD", description: "", icon: "", color: "#3b82f6", status: "archived", createdAt: "", updatedAt: "",
  };

  const projectSelect = () => screen.getByLabelText("Project") as HTMLSelectElement;
  const optionLabels = (select: HTMLSelectElement) => Array.from(select.options).map((o) => o.textContent);

  it("offers only active projects, sorted by name, not API order", () => {
    // Listed archived-then-Bravo-then-Alpha, so the API order wouldn't match.
    render(
      <CreateTicketModal
        projects={[archived, bravo, alpha]}
        filters={{ ...EMPTY_FILTERS, project: "ALP" }}
        onClose={vi.fn()}
        onCreate={vi.fn()}
      />,
    );
    expect(optionLabels(projectSelect())).toEqual(["Alpha", "Bravo"]);
  });

  it("does not offer, or preselect, an archived project the view names", () => {
    render(
      <CreateTicketModal
        projects={[archived, alpha]}
        filters={{ ...EMPTY_FILTERS, project: "OLD" }}
        onClose={vi.fn()}
        onCreate={vi.fn()}
      />,
    );
    const select = projectSelect();
    expect(optionLabels(select)).toEqual(["Alpha"]);
    expect(select.value).toBe("p-alpha");
  });

  it("shows 'No active projects' and cannot silently pick an archived one", () => {
    const onCreate = vi.fn();
    render(
      <CreateTicketModal
        projects={[archived]}
        filters={{ ...EMPTY_FILTERS, project: "" }}
        onClose={vi.fn()}
        onCreate={onCreate}
      />,
    );
    const select = projectSelect();
    expect(select.value).toBe("");
    expect(optionLabels(select)).toEqual(["No active projects"]);

    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Ticket" } });
    fireEvent.click(screen.getByText("Create Ticket"));
    expect(onCreate).not.toHaveBeenCalled();
  });

  it("shows 'No projects' when there are none at all", () => {
    render(<CreateTicketModal projects={[]} filters={EMPTY_FILTERS} onClose={vi.fn()} onCreate={vi.fn()} />);
    expect(optionLabels(projectSelect())).toEqual(["No projects"]);
  });

  it("labels a project with its icon and name, and one without an icon by its name alone", () => {
    // The API leaves out an empty icon, so it can be missing as well as blank.
    const noIcon: Partial<Project> = { ...bravo, id: "p-charlie", name: "Charlie", prefix: "CHA" };
    delete noIcon.icon;
    render(
      <CreateTicketModal
        projects={[{ ...alpha, icon: "🧭" }, bravo, noIcon as Project]}
        filters={{ ...EMPTY_FILTERS, project: "ALP" }}
        onClose={vi.fn()}
        onCreate={vi.fn()}
      />,
    );
    expect(optionLabels(projectSelect())).toEqual(["🧭 Alpha", "Bravo", "Charlie"]);
  });

  describe("a project archived by a live refresh while the form is open", () => {
    const archive = (p: Project): Project => ({ ...p, status: "archived" });
    const renderOn = (view: string, onCreate = vi.fn()) => {
      const props = { filters: { ...EMPTY_FILTERS, project: view }, onClose: vi.fn(), onCreate };
      const { rerender } = render(<CreateTicketModal projects={[alpha, bravo]} {...props} />);
      return { onCreate, refresh: (projects: Project[]) => rerender(<CreateTicketModal projects={projects} {...props} />) };
    };
    const submit = () => {
      fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Ticket" } });
      fireEvent.click(screen.getByText("Create Ticket"));
    };

    it("is dropped when the user picked it, and the ticket waits for another pick", () => {
      const { onCreate, refresh } = renderOn("ALP");
      fireEvent.change(projectSelect(), { target: { value: "p-bravo" } });
      refresh([alpha, archive(bravo)]);

      expect(projectSelect().value).toBe("");
      expect(optionLabels(projectSelect())).toEqual(["Select project…", "Alpha"]);
      submit();
      expect(onCreate).not.toHaveBeenCalled();

      fireEvent.change(projectSelect(), { target: { value: "p-alpha" } });
      submit();
      expect(onCreate).toHaveBeenCalledWith(expect.objectContaining({ projectId: "p-alpha" }));
    });

    it("is dropped when the form started on it, rather than moving to another project on its own", () => {
      const { onCreate, refresh } = renderOn("BRA");
      expect(projectSelect().value).toBe("p-bravo");
      refresh([alpha, archive(bravo)]);

      expect(projectSelect().value).toBe("");
      submit();
      expect(onCreate).not.toHaveBeenCalled();
    });

    it("leaves nothing to submit to when it was the last active project", () => {
      const { onCreate, refresh } = renderOn("ALP");
      refresh([archive(alpha), archive(bravo)]);

      expect(optionLabels(projectSelect())).toEqual(["No active projects"]);
      submit();
      expect(onCreate).not.toHaveBeenCalled();
    });
  });
});

describe("CreateTicketModal: images before the ticket exists", () => {
  const description = () => screen.getByLabelText("Description") as HTMLTextAreaElement;
  const png = new File(["x"], "shot.png", { type: "image/png" });

  it("says an image can be added once the ticket is created, and keeps the text as it was", () => {
    render(<CreateTicketModal projects={[project]} filters={{ ...EMPTY_FILTERS, project: "p1" }} onClose={vi.fn()} onCreate={vi.fn()} />);
    fireEvent.change(description(), { target: { value: "Draft" } });
    const pasted = createEvent.paste(description(), { clipboardData: { types: ["Files"], files: [png] } });
    fireEvent(description(), pasted);
    expect(pasted.defaultPrevented).toBe(true);
    expect(description().value).toBe("Draft");
    expect(screen.getByRole("alert").textContent).toContain("Images can be pasted or dropped once the ticket is created.");

    // A dropped image never opens in place of the form.
    const dropped = createEvent.drop(description(), { dataTransfer: { types: ["Files"], files: [png] } });
    fireEvent(description(), dropped);
    expect(dropped.defaultPrevented).toBe(true);
    expect(description().value).toBe("Draft");
    expect(mockApi.documents.createImage).not.toHaveBeenCalled();
  });

  it("pastes text as before", () => {
    render(<CreateTicketModal projects={[project]} filters={{ ...EMPTY_FILTERS, project: "p1" }} onClose={vi.fn()} onCreate={vi.fn()} />);
    const pasted = createEvent.paste(description(), { clipboardData: { types: ["text/plain"], files: [] } });
    fireEvent(description(), pasted);
    expect(pasted.defaultPrevented).toBe(false);
    expect(screen.queryByRole("alert")).toBeNull();
  });
});

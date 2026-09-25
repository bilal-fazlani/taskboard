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

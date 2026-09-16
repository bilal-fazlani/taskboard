// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { Project, Subtask, Ticket } from "../api/client";

// The editor and its pickers only talk to the API through this module, so
// mocking it keeps every test away from a real server.
const mockApi = vi.hoisted(() => ({
  tickets: {
    get: vi.fn(),
    list: vi.fn(),
    addSubtask: vi.fn(),
  },
  subtasks: {
    toggle: vi.fn(),
    delete: vi.fn(),
  },
  labels: {
    list: vi.fn(),
  },
}));

vi.mock("../api/client", () => ({ api: mockApi }));

import TicketEditor from "./TicketEditor";

const project: Project = {
  id: "p1",
  name: "Auth System",
  prefix: "AUTH",
  description: "",
  icon: "",
  color: "#3b82f6",
  status: "active",
  createdAt: "",
  updatedAt: "",
};

function sub(id: string, title: string, completed = false): Subtask {
  return { id, ticketId: "t1", title, completed, position: 0 };
}

function makeTicket(overrides: Partial<Ticket> = {}): Ticket {
  return {
    id: "t1",
    projectId: "p1",
    number: 7,
    title: "Ship login page",
    description: "Some **markdown**",
    status: "todo",
    priority: "high",
    dueDate: "2026-10-01",
    position: 0,
    createdAt: "",
    updatedAt: "",
    projectPrefix: "AUTH",
    repos: ["acme/auth-web"],
    labels: [{ id: "l1", name: "frontend", color: "#3b82f6", ticketCount: 0 }],
    subtasks: [sub("s1", "Write tests")],
    dependsOn: [{ id: "t2", key: "AUTH-2", title: "Build login UI", status: "in_progress" }],
    ...overrides,
  };
}

function renderEditor(ticket = makeTicket(), extra: { ticketUrl?: string } = {}) {
  const onClose = vi.fn();
  const onUpdate = vi.fn();
  const onDelete = vi.fn();
  const utils = render(
    <TicketEditor
      ticket={ticket}
      projects={[project]}
      onClose={onClose}
      onUpdate={onUpdate}
      onDelete={onDelete}
      {...extra}
    />,
  );
  return { ...utils, onClose, onUpdate, onDelete };
}

const saveButton = () => screen.queryByRole("button", { name: "Save Changes" });

beforeEach(() => {
  mockApi.tickets.get.mockImplementation((id: string) => Promise.resolve(makeTicket({ id })));
  mockApi.tickets.list.mockResolvedValue([]);
  mockApi.labels.list.mockResolvedValue([]);
});

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

describe("TicketEditor as a modal", () => {
  it("is a labelled modal dialog with the key and a status badge in the header", async () => {
    renderEditor();
    const dialog = screen.getByRole("dialog", { name: "AUTH-7" });
    expect(dialog.getAttribute("aria-modal")).toBe("true");
    expect(screen.getByTestId("ticket-editor-status").textContent).toBe("Todo");
    // Neither the key nor the badge may wrap in a narrow header.
    for (const el of [screen.getByRole("heading", { name: "AUTH-7" }), screen.getByTestId("ticket-editor-status")]) {
      expect(el.className).toMatch(/\bwhitespace-nowrap\b/);
      expect(el.className).toMatch(/\bshrink-0\b/);
    }
    await waitFor(() => expect(mockApi.tickets.get).toHaveBeenCalledWith("t1"));
  });

  it("puts content in the first column and fields in the second", () => {
    renderEditor();
    const body = screen.getByTestId("ticket-editor-body");
    expect(body.className).toMatch(/\blg:grid\b/);
    expect(body.className).toMatch(/lg:grid-cols-/);
    const [content, fields] = Array.from(body.children) as HTMLElement[];
    expect(content.getAttribute("aria-label")).toBe("Ticket content");
    expect(fields.getAttribute("aria-label")).toBe("Ticket fields");
    expect(within(content).getByLabelText("Title")).toBeTruthy();
    expect(within(content).getByText("Subtasks")).toBeTruthy();
    expect(within(fields).getByLabelText("Status")).toBeTruthy();
    expect(within(fields).getByText("Depends on")).toBeTruthy();
  });

  it("shows no link or copy button until it is given a ticket URL", () => {
    renderEditor();
    expect(screen.queryByTestId("ticket-url")).toBeNull();
    expect(screen.queryByRole("button", { name: "Copy link" })).toBeNull();
  });

  it("shows a given ticket URL and copies it", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    renderEditor(makeTicket(), { ticketUrl: "http://localhost:3011/?ticket=t1" });
    expect(screen.getByTestId("ticket-url").textContent).toBe("http://localhost:3011/?ticket=t1");
    fireEvent.click(screen.getByRole("button", { name: "Copy link" }));
    expect(writeText).toHaveBeenCalledWith("http://localhost:3011/?ticket=t1");
    await screen.findByRole("button", { name: "Link copied" });
  });
});

describe("closing", () => {
  it("closes from the close button", () => {
    const { onClose } = renderEditor();
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("closes on Escape", () => {
    const { onClose } = renderEditor();
    fireEvent.keyDown(screen.getByLabelText("Title"), { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("leaves Escape to a control that already handled it", () => {
    const { onClose } = renderEditor();
    const title = screen.getByLabelText("Title");
    title.addEventListener("keydown", (e) => e.preventDefault());
    fireEvent.keyDown(title, { key: "Escape" });
    expect(onClose).not.toHaveBeenCalled();
  });

  it("closes on a scrim click but not on a click inside the window", () => {
    const { onClose } = renderEditor();
    fireEvent.click(screen.getByRole("dialog"));
    fireEvent.click(screen.getByLabelText("Title"));
    expect(onClose).not.toHaveBeenCalled();
    fireEvent.click(screen.getByTestId("ticket-editor-scrim"));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("ignores other keys", () => {
    const { onClose } = renderEditor();
    fireEvent.keyDown(document, { key: "Enter" });
    expect(onClose).not.toHaveBeenCalled();
  });

  it("stops listening for Escape once closed", () => {
    const { onClose, unmount } = renderEditor();
    unmount();
    fireEvent.keyDown(document, { key: "Escape" });
    expect(onClose).not.toHaveBeenCalled();
  });
});

const closePaths: [string, () => void][] = [
  ["the close button", () => fireEvent.click(screen.getByRole("button", { name: "Close" }))],
  ["Escape", () => fireEvent.keyDown(screen.getByLabelText("Title"), { key: "Escape" })],
  ["a scrim click", () => fireEvent.click(screen.getByTestId("ticket-editor-scrim"))],
];

const confirmDialog = () => screen.queryByRole("alertdialog", { name: "Discard unsaved changes?" });

function editTitle(value = "Edited title") {
  fireEvent.change(screen.getByLabelText("Title"), { target: { value } });
}

describe("closing with unsaved edits", () => {
  it.each(closePaths)("closes straight away from %s when nothing is unsaved", (_name, close) => {
    const { onClose } = renderEditor();
    close();
    expect(confirmDialog()).toBeNull();
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it.each(closePaths)("asks before %s discards edits, and closes on Discard", (_name, close) => {
    const { onClose, onUpdate } = renderEditor();
    editTitle();
    close();
    const confirm = confirmDialog();
    expect(confirm).not.toBeNull();
    expect(confirm!.getAttribute("aria-modal")).toBe("true");
    expect(onClose).not.toHaveBeenCalled();
    // Focus starts on the safe choice.
    expect(document.activeElement).toBe(within(confirm!).getByRole("button", { name: "Keep editing" }));

    fireEvent.click(within(confirm!).getByRole("button", { name: "Discard" }));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(onUpdate).not.toHaveBeenCalled();
    expect(confirmDialog()).toBeNull();
  });

  it.each(closePaths)("keeps the edits open when %s is cancelled", async (_name, close) => {
    const { onClose } = renderEditor();
    const title = screen.getByLabelText("Title") as HTMLInputElement;
    title.focus();
    editTitle();
    close();
    fireEvent.click(within(confirmDialog()!).getByRole("button", { name: "Keep editing" }));

    expect(confirmDialog()).toBeNull();
    expect(onClose).not.toHaveBeenCalled();
    expect(title.value).toBe("Edited title");
    expect(saveButton()).not.toBeNull();
    const dialog = screen.getByRole("dialog");
    await waitFor(() => expect(dialog.contains(document.activeElement)).toBe(true));
  });

  it("gives focus back to the control that had it when cancelled", async () => {
    renderEditor();
    editTitle();
    const closeButton = screen.getByRole("button", { name: "Close" });
    closeButton.focus();
    fireEvent.click(closeButton);
    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    await waitFor(() => expect(document.activeElement).toBe(closeButton));
  });

  it("cancels the confirm, not the editor, on Escape while the confirm is open", async () => {
    const { onClose } = renderEditor();
    editTitle();
    fireEvent.keyDown(screen.getByLabelText("Title"), { key: "Escape" });
    const keep = within(confirmDialog()!).getByRole("button", { name: "Keep editing" });
    fireEvent.keyDown(keep, { key: "Escape" });
    expect(confirmDialog()).toBeNull();
    expect(onClose).not.toHaveBeenCalled();
    expect((screen.getByLabelText("Title") as HTMLInputElement).value).toBe("Edited title");
    // A further Escape asks again rather than closing.
    fireEvent.keyDown(screen.getByLabelText("Title"), { key: "Escape" });
    expect(confirmDialog()).not.toBeNull();
    expect(onClose).not.toHaveBeenCalled();
  });

  it("ignores further close requests while the confirm is open", async () => {
    const { onClose } = renderEditor();
    editTitle();
    const title = screen.getByLabelText("Title");
    title.focus();
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    // Clicking the outer scrim moves focus off the confirm in a real browser.
    (document.activeElement as HTMLElement).blur();
    fireEvent.click(screen.getByTestId("ticket-editor-scrim"));
    expect(confirmDialog()).not.toBeNull();
    expect(onClose).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(onClose).not.toHaveBeenCalled();
    await waitFor(() => expect(document.activeElement).toBe(title));
  });

  it("makes the header and body inert only while the confirm is open", () => {
    renderEditor();
    const header = screen.getByRole("dialog").querySelector("header")!;
    const body = screen.getByTestId("ticket-editor-body");
    expect(header.hasAttribute("inert")).toBe(false);
    expect(body.hasAttribute("inert")).toBe(false);
    editTitle();
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(header.hasAttribute("inert")).toBe(true);
    expect(body.hasAttribute("inert")).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(header.hasAttribute("inert")).toBe(false);
    expect(body.hasAttribute("inert")).toBe(false);
  });

  it("cancels on a click beside the confirm", () => {
    const { onClose } = renderEditor();
    editTitle();
    fireEvent.click(screen.getByTestId("ticket-editor-scrim"));
    fireEvent.click(screen.getByTestId("discard-confirm-backdrop"));
    expect(confirmDialog()).toBeNull();
    expect(onClose).not.toHaveBeenCalled();
  });

  it("keeps Tab inside the confirm", () => {
    renderEditor();
    editTitle();
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    const confirm = confirmDialog()!;
    const keep = within(confirm).getByRole("button", { name: "Keep editing" });
    const discard = within(confirm).getByRole("button", { name: "Discard" });
    fireEvent.keyDown(discard, { key: "Tab" });
    expect(document.activeElement).toBe(keep);
    fireEvent.keyDown(keep, { key: "Tab", shiftKey: true });
    expect(document.activeElement).toBe(discard);
    // Focus that somehow left the confirm is pulled back into it.
    screen.getByLabelText("Title").focus();
    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Tab" });
    expect(document.activeElement).toBe(keep);
  });

  it("closes straight away once the edits are saved", () => {
    const { onClose, onUpdate } = renderEditor();
    editTitle();
    fireEvent.click(saveButton()!);
    expect(onUpdate).toHaveBeenCalledTimes(1);
    fireEvent.keyDown(document, { key: "Escape" });
    expect(confirmDialog()).toBeNull();
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("still deletes without asking", () => {
    const { onClose, onDelete } = renderEditor();
    editTitle();
    fireEvent.click(screen.getByRole("button", { name: "Delete ticket" }));
    expect(confirmDialog()).toBeNull();
    expect(onDelete).toHaveBeenCalledWith("t1");
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});

describe("focus", () => {
  it("moves focus into the dialog on open and back to the opener on close", () => {
    const opener = document.createElement("button");
    document.body.appendChild(opener);
    opener.focus();
    const { unmount } = renderEditor();
    expect(document.activeElement).toBe(screen.getByRole("dialog"));
    unmount();
    expect(document.activeElement).toBe(opener);
    opener.remove();
  });

  it("keeps Tab inside the dialog", () => {
    renderEditor();
    const dialog = screen.getByRole("dialog");
    // Shift+Tab from the dialog wraps to its last control, in the fields column.
    fireEvent.keyDown(dialog, { key: "Tab", shiftKey: true });
    const last = document.activeElement as HTMLElement;
    expect(last).not.toBe(dialog);
    expect(last).toBe(screen.getByPlaceholderText("Search tickets to add..."));
    // Tab from the last control wraps to the first, in the header.
    fireEvent.keyDown(last, { key: "Tab" });
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Delete ticket" }));
  });
});

describe("the dirty flag and saving", () => {
  it("shows Save only after an edit and saves every field", () => {
    const { onUpdate } = renderEditor();
    expect(saveButton()).toBeNull();

    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "New title" } });
    expect(saveButton()).not.toBeNull();

    fireEvent.click(saveButton()!);
    expect(onUpdate).toHaveBeenCalledWith("t1", {
      title: "New title",
      description: "Some **markdown**",
      status: "todo",
      priority: "high",
      dueDate: "2026-10-01",
      repos: ["acme/auth-web"],
      labels: ["frontend"],
      dependsOn: ["t2"],
    });
    expect(saveButton()).toBeNull();
  });

  it("sends no due date once it is cleared", () => {
    const { onUpdate } = renderEditor();
    fireEvent.change(screen.getByLabelText("Due Date"), { target: { value: "" } });
    fireEvent.click(saveButton()!);
    expect(onUpdate.mock.calls[0][1].dueDate).toBeUndefined();
  });

  const edits: [string, () => void, Record<string, unknown>][] = [
    [
      "status",
      () => fireEvent.change(screen.getByLabelText("Status"), { target: { value: "done" } }),
      { status: "done" },
    ],
    [
      "priority",
      () => fireEvent.change(screen.getByLabelText("Priority"), { target: { value: "low" } }),
      { priority: "low" },
    ],
    [
      "due date",
      () => fireEvent.change(screen.getByLabelText("Due Date"), { target: { value: "2026-12-24" } }),
      { dueDate: "2026-12-24" },
    ],
    [
      "description",
      () => {
        fireEvent.click(screen.getByRole("button", { name: "Write" }));
        fireEvent.change(screen.getByLabelText("Description"), { target: { value: "Plain" } });
      },
      { description: "Plain" },
    ],
    [
      "repos",
      () => {
        const input = screen.getByPlaceholderText("acme/billing-web, then Enter");
        fireEvent.change(input, { target: { value: "acme/api" } });
        fireEvent.keyDown(input, { key: "Enter" });
      },
      { repos: ["acme/auth-web", "acme/api"] },
    ],
    [
      "labels",
      () => {
        const input = screen.getByPlaceholderText("Add a label and press Enter");
        fireEvent.change(input, { target: { value: "urgent" } });
        fireEvent.keyDown(input, { key: "Enter" });
      },
      { labels: ["frontend", "urgent"] },
    ],
    [
      "depends on",
      () => fireEvent.click(screen.getByRole("button", { name: "Remove dependency AUTH-2" })),
      { dependsOn: [] },
    ],
  ];

  it.each(edits)("marks the ticket dirty when the %s changes", async (_name, edit, expected) => {
    const { onUpdate } = renderEditor();
    // Let the detail fetch settle so it cannot race the edit.
    await waitFor(() => expect(mockApi.tickets.get).toHaveBeenCalled());
    await act(async () => {});
    expect(saveButton()).toBeNull();
    edit();
    expect(saveButton()).not.toBeNull();
    fireEvent.click(saveButton()!);
    expect(onUpdate).toHaveBeenCalledWith("t1", expect.objectContaining(expected));
  });

  it("keeps edits made while the full ticket is still loading", async () => {
    let resolve!: (t: Ticket) => void;
    mockApi.tickets.get.mockReturnValue(new Promise<Ticket>((r) => (resolve = r)));
    const { onUpdate } = renderEditor();

    fireEvent.click(screen.getByRole("button", { name: "Remove frontend" }));
    await act(async () => {
      resolve(
        makeTicket({
          labels: [{ id: "l9", name: "server", color: "#fff", ticketCount: 0 }],
          blocks: [{ id: "t5", key: "AUTH-5", title: "Release", status: "todo" }],
        }),
      );
    });

    // The fetched ticket still fills in the read-only Blocks list...
    expect(screen.getByText("Release")).toBeTruthy();
    // ...but not the labels the user just edited.
    expect(screen.queryByText("server")).toBeNull();
    fireEvent.click(saveButton()!);
    expect(onUpdate.mock.calls[0][1].labels).toEqual([]);
  });

  it("fills in the full ticket when nothing was edited", async () => {
    mockApi.tickets.get.mockResolvedValue(
      makeTicket({
        labels: [{ id: "l9", name: "server", color: "#fff", ticketCount: 0 }],
        subtasks: [sub("s1", "Write tests"), sub("s2", "Deploy")],
      }),
    );
    renderEditor();
    await screen.findByText("server");
    expect(screen.getByText("Deploy")).toBeTruthy();
    expect(saveButton()).toBeNull();
  });
});

describe("subtasks", () => {
  it("adds, toggles and deletes subtasks without marking the ticket dirty", async () => {
    mockApi.tickets.addSubtask.mockResolvedValue(sub("s2", "Deploy"));
    mockApi.subtasks.toggle.mockResolvedValue(sub("s1", "Write tests", true));
    mockApi.subtasks.delete.mockResolvedValue(undefined);
    renderEditor();
    await act(async () => {});

    fireEvent.change(screen.getByLabelText("New subtask"), { target: { value: "Deploy" } });
    fireEvent.click(screen.getByRole("button", { name: "Add" }));
    await screen.findByText("Deploy");
    expect(mockApi.tickets.addSubtask).toHaveBeenCalledWith("t1", "Deploy");
    expect((screen.getByLabelText("New subtask") as HTMLInputElement).value).toBe("");

    fireEvent.click(screen.getByRole("button", { name: "Mark done: Write tests" }));
    await screen.findByRole("button", { name: "Mark not done: Write tests" });
    expect(mockApi.subtasks.toggle).toHaveBeenCalledWith("s1");

    fireEvent.click(screen.getByRole("button", { name: "Delete subtask: Deploy" }));
    await waitFor(() => expect(screen.queryByText("Deploy")).toBeNull());
    expect(mockApi.subtasks.delete).toHaveBeenCalledWith("s2");

    expect(saveButton()).toBeNull();
  });

  it("ignores a blank subtask", () => {
    renderEditor();
    fireEvent.change(screen.getByLabelText("New subtask"), { target: { value: "   " } });
    fireEvent.click(screen.getByRole("button", { name: "Add" }));
    expect(mockApi.tickets.addSubtask).not.toHaveBeenCalled();
  });
});

describe("description", () => {
  it("opens a described ticket in preview and switches to write", () => {
    renderEditor();
    expect(screen.getByTestId("description-preview").innerHTML).toContain("<strong>markdown</strong>");
    expect(screen.queryByLabelText("Description")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Write" }));
    expect((screen.getByLabelText("Description") as HTMLTextAreaElement).value).toBe("Some **markdown**");
    fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    expect(screen.getByTestId("description-preview")).toBeTruthy();
  });

  it("opens an undescribed ticket in write mode", () => {
    renderEditor(makeTicket({ description: "" }));
    expect(screen.getByLabelText("Description")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    fireEvent.click(screen.getByText("Add a description…"));
    expect(screen.getByLabelText("Description")).toBeTruthy();
  });
});

describe("fields", () => {
  it("shows the project name and read-only blocks", async () => {
    mockApi.tickets.get.mockResolvedValue(
      makeTicket({ blocks: [{ id: "t5", key: "AUTH-5", title: "Release", status: "in_progress" }] }),
    );
    renderEditor();
    expect(screen.getByText("Auth System")).toBeTruthy();
    await screen.findByText("Release");
    expect(screen.getAllByText("in progress")).toHaveLength(2);
  });
});

describe("deleting", () => {
  it("deletes the ticket and closes", () => {
    const { onDelete, onClose } = renderEditor();
    fireEvent.click(screen.getByRole("button", { name: "Delete ticket" }));
    expect(onDelete).toHaveBeenCalledWith("t1");
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});

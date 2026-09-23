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
  epics: {
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

function renderEditor(
  ticket = makeTicket(),
  extra: { ticketUrl?: string; closeRequested?: boolean; onOpenTicket?: (id: string) => void } = {},
) {
  const onClose = vi.fn();
  const onUpdate = vi.fn();
  const onDelete = vi.fn();
  const onCloseCancelled = vi.fn();
  const onDirtyChange = vi.fn();
  const utils = render(
    <TicketEditor
      ticket={ticket}
      projects={[project]}
      onClose={onClose}
      onUpdate={onUpdate}
      onDelete={onDelete}
      onCloseCancelled={onCloseCancelled}
      onDirtyChange={onDirtyChange}
      {...extra}
    />,
  );
  const rerenderWith = (props: { closeRequested?: boolean; ticket?: Ticket }) =>
    utils.rerender(
      <TicketEditor
        ticket={props.ticket ?? ticket}
        projects={[project]}
        onClose={onClose}
        onUpdate={onUpdate}
        onDelete={onDelete}
        onCloseCancelled={onCloseCancelled}
        onDirtyChange={onDirtyChange}
        {...extra}
        closeRequested={props.closeRequested ?? extra.closeRequested}
      />,
    );
  return { ...utils, rerenderWith, onClose, onUpdate, onDelete, onCloseCancelled, onDirtyChange };
}

const saveButton = () => screen.queryByRole("button", { name: "Save Changes" });

const NO_PROGRESS = { counts: {}, total: 0, complete: false, lastActivityAt: null };
const epicList = (...names: string[]) => ({
  epics: names.map((name) => ({ ...NO_PROGRESS, id: `e-${name}`, projectId: "p1", name, createdAt: "", updatedAt: "" })),
  noEpic: NO_PROGRESS,
});

beforeEach(() => {
  mockApi.epics.list.mockResolvedValue(epicList());
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

  it("offers every status in its dropdown and badges agent_review in violet", async () => {
    renderEditor(makeTicket({ status: "agent_review" }));
    const status = screen.getByLabelText("Status") as HTMLSelectElement;
    expect([...status.options].map((o) => o.value)).toEqual(["todo", "in_progress", "agent_review", "done"]);
    expect(status.value).toBe("agent_review");
    const badge = screen.getByTestId("ticket-editor-status");
    expect(badge.textContent).toBe("Agent Review");
    expect(badge.className).toMatch(/\bbg-violet-500\/20\b/);
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

describe("the due date input", () => {
  it("shows an existing RFC 3339 due date with no day shift", () => {
    // The API stores and returns due dates as midnight UTC. Formatting this
    // through a Date object in a negative-offset timezone (anything west of
    // UTC) would land on September 30th; slicing the string must not. This is
    // the value an <input type="date"> itself validates against, so a wrong
    // value here is the real guard against the RFC 3339 string reappearing
    // unparsed.
    renderEditor(makeTicket({ dueDate: "2026-10-01T00:00:00Z" }));
    expect((screen.getByLabelText("Due Date") as HTMLInputElement).value).toBe("2026-10-01");
  });

  it("shows a plain YYYY-MM-DD due date unchanged, matching what CreateTicketModal sends", () => {
    renderEditor(makeTicket({ dueDate: "2026-10-01" }));
    expect((screen.getByLabelText("Due Date") as HTMLInputElement).value).toBe("2026-10-01");
  });

  it("shows an empty due date input when the ticket has none", () => {
    renderEditor(makeTicket({ dueDate: undefined }));
    expect((screen.getByLabelText("Due Date") as HTMLInputElement).value).toBe("");
  });

  it("leaves an untouched due date out of the save, whatever shape it arrived in", () => {
    // An omitted field is the API's "leave it unchanged", so a due date the
    // user never touched is never re-sent — not even as the same day.
    const { onUpdate } = renderEditor(makeTicket({ dueDate: "2026-10-01T00:00:00Z" }));
    expect((screen.getByLabelText("Due Date") as HTMLInputElement).value).toBe("2026-10-01");
    editTitle();
    fireEvent.click(saveButton()!);
    expect("dueDate" in onUpdate.mock.calls[0][1]).toBe(false);
  });

  it("sends an edited due date as a plain date, matching the API's real shape", () => {
    const { onUpdate } = renderEditor(makeTicket({ dueDate: "2026-10-01T00:00:00Z" }));
    fireEvent.change(screen.getByLabelText("Due Date"), { target: { value: "2026-12-24" } });
    fireEvent.click(saveButton()!);
    expect(onUpdate.mock.calls[0][1].dueDate).toBe("2026-12-24");
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

  it("closes straight away once the edits are saved", async () => {
    const { onClose, onUpdate } = renderEditor();
    editTitle();
    // Saving is asynchronous now: the edits are only let go once the save has
    // come back, so every one of these waits for it.
    await act(async () => fireEvent.click(saveButton()!));
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
  it("shows Save only after an edit, and sends just the fields that changed", async () => {
    const { onUpdate } = renderEditor();
    expect(saveButton()).toBeNull();

    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "New title" } });
    expect(saveButton()).not.toBeNull();

    await act(async () => fireEvent.click(saveButton()!));
    // Every other field is left out, so whatever the server holds for it
    // stays: Save merges by field rather than writing the whole ticket back.
    expect(onUpdate).toHaveBeenCalledWith("t1", { title: "New title" });
    expect(saveButton()).toBeNull();
  });

  it("keeps the edits, and says why, when the save fails", async () => {
    const { onUpdate, onDirtyChange } = renderEditor();
    await waitFor(() => expect(mockApi.tickets.get).toHaveBeenCalled());
    await act(async () => {});
    editTitle("Mine");
    fireEvent.change(screen.getByLabelText("Priority"), { target: { value: "low" } });
    // The ticket was deleted while the editor was open: the PUT 404s.
    onUpdate.mockRejectedValue(new Error('API error 404: {"error":"ticket not found"}'));

    await act(async () => fireEvent.click(saveButton()!));

    const error = screen.getByTestId("ticket-save-error");
    expect(error.getAttribute("role")).toBe("alert");
    expect(error.textContent).toContain("no longer exists");
    expect(error.textContent).toContain("Your edits are still here");
    // Nothing was given up: the edits, the dirty flag and the editor all stay.
    expect((screen.getByLabelText("Title") as HTMLInputElement).value).toBe("Mine");
    expect((screen.getByLabelText("Priority") as HTMLSelectElement).value).toBe("low");
    expect(saveButton()).not.toBeNull();
    expect(onDirtyChange).toHaveBeenLastCalledWith(true);
  });

  it("sends the same fields again when a failed save is retried, and clears the error", async () => {
    const { onUpdate } = renderEditor();
    await waitFor(() => expect(mockApi.tickets.get).toHaveBeenCalled());
    await act(async () => {});
    editTitle("Mine");
    onUpdate.mockRejectedValueOnce(new Error("API error 500: {}"));
    await act(async () => fireEvent.click(saveButton()!));
    expect(screen.getByTestId("ticket-save-error")).toBeTruthy();

    await act(async () => fireEvent.click(saveButton()!));
    expect(onUpdate.mock.calls).toEqual([
      ["t1", { title: "Mine" }],
      ["t1", { title: "Mine" }],
    ]);
    expect(screen.queryByTestId("ticket-save-error")).toBeNull();
    expect(saveButton()).toBeNull();
  });

  it("waits for the save before letting the edits go", async () => {
    let finish!: () => void;
    const { onUpdate, onDirtyChange } = renderEditor();
    await waitFor(() => expect(mockApi.tickets.get).toHaveBeenCalled());
    await act(async () => {});
    editTitle("Mine");
    onUpdate.mockReturnValue(new Promise<void>((resolve) => (finish = resolve)));

    await act(async () => fireEvent.click(saveButton()!));
    // Still unsaved while the request is in flight, so nothing can unmount the
    // editor on the strength of a save that has not landed.
    expect(onDirtyChange).toHaveBeenLastCalledWith(true);
    expect(saveButton()).not.toBeNull();

    await act(async () => finish());
    expect(onDirtyChange).toHaveBeenLastCalledWith(false);
    expect(saveButton()).toBeNull();
  });

  it("sends only what changed since the last save", async () => {
    const { onUpdate } = renderEditor();
    await waitFor(() => expect(mockApi.tickets.get).toHaveBeenCalled());
    await act(async () => {});
    editTitle("First");
    await act(async () => fireEvent.click(saveButton()!));
    fireEvent.change(screen.getByLabelText("Priority"), { target: { value: "low" } });
    await act(async () => fireEvent.click(saveButton()!));
    expect(onUpdate.mock.calls[1][1]).toEqual({ priority: "low" });
  });

  it("sends an explicit clear once the due date is cleared", () => {
    const { onUpdate } = renderEditor();
    fireEvent.change(screen.getByLabelText("Due Date"), { target: { value: "" } });
    fireEvent.click(saveButton()!);
    // "" is the API's explicit "clear the due date", distinct from omitting
    // the field entirely (which the API reads as "leave it unchanged").
    expect(onUpdate.mock.calls[0][1].dueDate).toBe("");
    expect("dueDate" in onUpdate.mock.calls[0][1]).toBe(true);
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

// The ticket can change under the editor: a live refresh (ACP-7) hands the
// page a newer version while the editor is open. What happens next depends on
// whether there is anything unsaved to lose.
describe("a ticket that changed elsewhere", () => {
  const notice = () => screen.queryByTestId("ticket-changed-notice");
  const reload = () => screen.getByRole("button", { name: "Reload" });

  // What a live refresh does: the page passes the refreshed ticket down, and
  // the editor's own fetch for the full one answers with it too.
  async function changeTo(
    rerenderWith: (props: { ticket?: Ticket }) => void,
    overrides: Partial<Ticket>,
  ) {
    const next = makeTicket({ updatedAt: "2026-09-16T23:00:00Z", ...overrides });
    mockApi.tickets.get.mockResolvedValue(next);
    await act(async () => rerenderWith({ ticket: next }));
    return next;
  }

  async function settled() {
    await waitFor(() => expect(mockApi.tickets.get).toHaveBeenCalled());
    await act(async () => {});
  }

  it("refreshes a clean editor quietly", async () => {
    const { rerenderWith } = renderEditor();
    await settled();

    await changeTo(rerenderWith, {
      title: "Renamed elsewhere",
      status: "in_progress",
      priority: "low",
      dueDate: "2026-12-24",
      labels: [{ id: "l9", name: "server", color: "#fff", ticketCount: 0 }],
    });

    expect((screen.getByLabelText("Title") as HTMLInputElement).value).toBe("Renamed elsewhere");
    expect((screen.getByLabelText("Status") as HTMLSelectElement).value).toBe("in_progress");
    expect((screen.getByLabelText("Priority") as HTMLSelectElement).value).toBe("low");
    expect((screen.getByLabelText("Due Date") as HTMLInputElement).value).toBe("2026-12-24");
    expect(screen.getByText("server")).toBeTruthy();
    expect(screen.getByTestId("ticket-editor-status").textContent).toBe("In Progress");
    // Quietly: no notice, and nothing to save.
    expect(notice()).toBeNull();
    expect(saveButton()).toBeNull();
  });

  it("leaves the focused control and its caret where they are", async () => {
    const { rerenderWith } = renderEditor();
    await settled();
    const title = screen.getByLabelText("Title") as HTMLInputElement;
    title.focus();
    title.setSelectionRange(4, 4);

    await changeTo(rerenderWith, { status: "in_progress" });

    // The controls are filled in, not rebuilt, so a refresh under someone
    // reading or about to type takes neither the focus nor the caret away.
    expect(screen.getByLabelText("Title")).toBe(title);
    expect(document.activeElement).toBe(title);
    expect(title.selectionStart).toBe(4);
    expect((screen.getByLabelText("Status") as HTMLSelectElement).value).toBe("in_progress");
  });

  it("keeps every edit and says the ticket changed when there are unsaved edits", async () => {
    const { rerenderWith, onDirtyChange } = renderEditor();
    await settled();
    editTitle();
    fireEvent.change(screen.getByLabelText("Priority"), { target: { value: "low" } });

    await changeTo(rerenderWith, { title: "Renamed elsewhere", status: "done" });

    // Not one control is filled in from the server behind the user's back.
    expect((screen.getByLabelText("Title") as HTMLInputElement).value).toBe("Edited title");
    expect((screen.getByLabelText("Priority") as HTMLSelectElement).value).toBe("low");
    expect((screen.getByLabelText("Status") as HTMLSelectElement).value).toBe("todo");
    expect(saveButton()).not.toBeNull();
    expect(onDirtyChange).toHaveBeenLastCalledWith(true);
    expect(notice()!.textContent).toContain("This ticket changed");
    expect(notice()!.getAttribute("role")).toBe("status");
  });

  it("says nothing when the change touched no field the editor holds", async () => {
    const { rerenderWith } = renderEditor();
    await settled();
    editTitle();
    // A subtask added elsewhere, say: a newer ticket with the same fields.
    await changeTo(rerenderWith, { subtasks: [sub("s1", "Write tests"), sub("s2", "Deploy")] });
    expect(notice()).toBeNull();
    expect((screen.getByLabelText("Title") as HTMLInputElement).value).toBe("Edited title");
  });

  it("asks before the reload discards the edits, and keeps them when cancelled", async () => {
    const { rerenderWith } = renderEditor();
    await settled();
    editTitle();
    await changeTo(rerenderWith, { title: "Renamed elsewhere" });

    fireEvent.click(reload());
    expect(confirmDialog()).toBeTruthy();
    fireEvent.click(within(confirmDialog()!).getByRole("button", { name: "Keep editing" }));

    expect((screen.getByLabelText("Title") as HTMLInputElement).value).toBe("Edited title");
    expect(saveButton()).not.toBeNull();
    expect(notice()).not.toBeNull();
  });

  it("shows the latest version once the discard is confirmed", async () => {
    const { rerenderWith, onUpdate, onClose } = renderEditor();
    await settled();
    editTitle();
    await changeTo(rerenderWith, { title: "Renamed elsewhere", status: "done" });

    fireEvent.click(reload());
    fireEvent.click(within(confirmDialog()!).getByRole("button", { name: "Discard" }));

    expect((screen.getByLabelText("Title") as HTMLInputElement).value).toBe("Renamed elsewhere");
    expect((screen.getByLabelText("Status") as HTMLSelectElement).value).toBe("done");
    expect(notice()).toBeNull();
    expect(saveButton()).toBeNull();
    // A reload is not a save and not a close.
    expect(onUpdate).not.toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();
  });

  it("reloads the newest version when another change arrived meanwhile", async () => {
    const { rerenderWith } = renderEditor();
    await settled();
    editTitle();
    await changeTo(rerenderWith, { title: "First rename" });
    await changeTo(rerenderWith, { title: "Second rename", updatedAt: "2026-09-16T23:30:00Z" });

    fireEvent.click(reload());
    fireEvent.click(within(confirmDialog()!).getByRole("button", { name: "Discard" }));
    expect((screen.getByLabelText("Title") as HTMLInputElement).value).toBe("Second rename");
  });

  it("saves the edited fields only, so the change elsewhere survives", async () => {
    const { rerenderWith, onUpdate } = renderEditor();
    await settled();
    editTitle();
    await changeTo(rerenderWith, { status: "done", priority: "urgent" });

    await act(async () => fireEvent.click(saveButton()!));
    // Status and priority are left out, so the server keeps what it has;
    // sending the whole ticket would have written "todo" and "high" back.
    expect(onUpdate).toHaveBeenCalledWith("t1", { title: "Edited title" });
    expect(notice()).toBeNull();
  });

  it("sends the user's value for a field they and the server both changed", async () => {
    const { rerenderWith, onUpdate } = renderEditor();
    await settled();
    editTitle("Mine");
    await changeTo(rerenderWith, { title: "Theirs" });
    fireEvent.click(saveButton()!);
    expect(onUpdate).toHaveBeenCalledWith("t1", { title: "Mine" });
  });

  it("shows the server's latest values for untouched fields once the save comes back", async () => {
    const { rerenderWith, onUpdate } = renderEditor();
    await settled();
    editTitle("Mine");
    await changeTo(rerenderWith, { status: "done" });
    fireEvent.click(saveButton()!);
    expect(onUpdate).toHaveBeenCalledWith("t1", { title: "Mine" });

    // The page refetches after a save; with nothing unsaved left, the editor
    // simply shows what came back.
    await changeTo(rerenderWith, {
      title: "Mine",
      status: "done",
      updatedAt: "2026-09-16T23:45:00Z",
    });
    expect((screen.getByLabelText("Title") as HTMLInputElement).value).toBe("Mine");
    expect((screen.getByLabelText("Status") as HTMLSelectElement).value).toBe("done");
    expect(notice()).toBeNull();
  });

  it("keeps showing the ticket when the refetch fails", async () => {
    const { rerenderWith } = renderEditor();
    await settled();
    editTitle();
    mockApi.tickets.get.mockRejectedValue(new Error("offline"));
    await act(async () => rerenderWith({ ticket: makeTicket({ updatedAt: "2026-09-16T23:00:00Z" }) }));
    expect((screen.getByLabelText("Title") as HTMLInputElement).value).toBe("Edited title");
    expect(notice()).toBeNull();
  });
});

describe("linked tickets", () => {
  const withBlocks = () =>
    mockApi.tickets.get.mockResolvedValue(
      makeTicket({ blocks: [{ id: "t5", key: "AUTH-5", title: "Release", status: "todo" }] }),
    );

  it("opens a ticket it depends on", () => {
    const onOpenTicket = vi.fn();
    renderEditor(makeTicket(), { onOpenTicket });
    fireEvent.click(screen.getByRole("button", { name: "Open AUTH-2: Build login UI" }));
    expect(onOpenTicket).toHaveBeenCalledWith("t2");
  });

  it("opens a ticket it blocks", async () => {
    withBlocks();
    const onOpenTicket = vi.fn();
    renderEditor(makeTicket(), { onOpenTicket });
    fireEvent.click(await screen.findByRole("button", { name: "Open AUTH-5: Release" }));
    expect(onOpenTicket).toHaveBeenCalledWith("t5");
  });

  it("removes a dependency without opening it", () => {
    const onOpenTicket = vi.fn();
    renderEditor(makeTicket(), { onOpenTicket });
    fireEvent.click(screen.getByRole("button", { name: "Remove dependency AUTH-2" }));
    expect(onOpenTicket).not.toHaveBeenCalled();
  });

  it("asks before leaving unsaved edits for a linked ticket", () => {
    const onOpenTicket = vi.fn();
    renderEditor(makeTicket(), { onOpenTicket });
    editTitle();
    fireEvent.click(screen.getByRole("button", { name: "Open AUTH-2: Build login UI" }));
    expect(onOpenTicket).not.toHaveBeenCalled();

    fireEvent.click(within(confirmDialog()!).getByRole("button", { name: "Keep editing" }));
    expect(onOpenTicket).not.toHaveBeenCalled();
    expect((screen.getByLabelText("Title") as HTMLInputElement).value).toBe("Edited title");

    fireEvent.click(screen.getByRole("button", { name: "Open AUTH-2: Build login UI" }));
    fireEvent.click(within(confirmDialog()!).getByRole("button", { name: "Discard" }));
    expect(onOpenTicket).toHaveBeenCalledWith("t2");
  });

  it("shows plain rows when there is nowhere to open them", async () => {
    withBlocks();
    renderEditor();
    await screen.findByText("Release");
    expect(screen.queryByRole("button", { name: /^Open AUTH-/ })).toBeNull();
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

// The URL owns which ticket is open, so a Back that drops the `ticket`
// parameter asks the editor to close rather than unmounting it. Unsaved edits
// must be no easier to lose that way than through the close button.
describe("a close asked for by the URL", () => {
  it("reports unsaved edits, so the URL's owner knows it cannot just unmount", () => {
    const { onDirtyChange } = renderEditor();
    expect(onDirtyChange).toHaveBeenLastCalledWith(false);
    editTitle();
    expect(onDirtyChange).toHaveBeenLastCalledWith(true);
  });

  it("closes straight away when nothing is unsaved", () => {
    const { rerenderWith, onClose, onCloseCancelled } = renderEditor();
    rerenderWith({ closeRequested: true });
    expect(confirmDialog()).toBeNull();
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(onCloseCancelled).not.toHaveBeenCalled();
  });

  it("asks before discarding unsaved edits", () => {
    const { rerenderWith, onClose } = renderEditor();
    editTitle();
    rerenderWith({ closeRequested: true });
    expect(confirmDialog()).toBeTruthy();
    expect(onClose).not.toHaveBeenCalled();
  });

  it("closes once the discard is confirmed", () => {
    const { rerenderWith, onClose, onCloseCancelled } = renderEditor();
    editTitle();
    rerenderWith({ closeRequested: true });
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(onCloseCancelled).not.toHaveBeenCalled();
  });

  it("asks for the parameter back when the discard is cancelled", () => {
    const { rerenderWith, onClose, onCloseCancelled } = renderEditor();
    editTitle();
    rerenderWith({ closeRequested: true });
    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(onCloseCancelled).toHaveBeenCalledTimes(1);
    expect(onClose).not.toHaveBeenCalled();

    // The request stands until the URL's owner has put the parameter back; the
    // question goes with it, and the edits are still there to keep editing.
    rerenderWith({ closeRequested: false });
    expect(confirmDialog()).toBeNull();
    expect(screen.getByLabelText("Title")).toHaveProperty("value", "Edited title");
  });

  it("asks for the parameter back when Escape cancels the discard", () => {
    const { rerenderWith, onClose, onCloseCancelled } = renderEditor();
    editTitle();
    rerenderWith({ closeRequested: true });
    fireEvent.keyDown(document, { key: "Escape" });
    expect(onCloseCancelled).toHaveBeenCalledTimes(1);
    expect(onClose).not.toHaveBeenCalled();
    rerenderWith({ closeRequested: false });
    expect(confirmDialog()).toBeNull();
  });

  // Back pressed while the close button's question is already up: cancelling
  // has to undo that Back too, or the editor would sit on a URL that no longer
  // names it.
  it("undoes a URL close asked for behind the question already on screen", () => {
    const { rerenderWith, onClose, onCloseCancelled } = renderEditor();
    editTitle();
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(confirmDialog()).toBeTruthy();
    rerenderWith({ closeRequested: true });
    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(onCloseCancelled).toHaveBeenCalledTimes(1);
    expect(onClose).not.toHaveBeenCalled();
    rerenderWith({ closeRequested: false });
    expect(confirmDialog()).toBeNull();
  });
});

// The Epic select sits in the fields grid beside Due date. It offers "No
// epic" and the project's epics by name, and saves through the ticket's epic
// field, merging with changes made elsewhere like every other field.
describe("the epic picker", () => {
  const epicSelect = () => screen.getByLabelText("Epic") as HTMLSelectElement;
  const optionNames = () => [...epicSelect().options].map((o) => o.textContent);
  const inViews = { epic: { id: "e-Views", name: "Views" } };

  beforeEach(() => {
    mockApi.epics.list.mockResolvedValue(epicList("Views", "agents", "Realtime"));
    mockApi.tickets.get.mockImplementation((id: string) => Promise.resolve(makeTicket({ id, ...inViews })));
  });

  async function settled() {
    await waitFor(() => expect(mockApi.epics.list).toHaveBeenCalled());
    await act(async () => {});
  }

  it("sits beside Due date in the fields grid", async () => {
    renderEditor(makeTicket(inViews));
    await settled();
    const grid = screen.getByLabelText("Due Date").closest(".grid")!;
    const labels = [...grid.querySelectorAll(":scope > div > label, :scope > div > span")].map((l) => l.textContent);
    expect(labels).toEqual(["Status", "Priority", "Due Date", "Epic", "Project"]);
  });

  it("offers No epic and the project's epics by name, with the ticket's selected", async () => {
    renderEditor(makeTicket(inViews));
    await settled();
    expect(mockApi.epics.list).toHaveBeenCalledWith("p1");
    expect(optionNames()).toEqual(["No epic", "agents", "Realtime", "Views"]);
    expect(epicSelect().value).toBe("e-Views");
  });

  it("shows No epic for a ticket without one", async () => {
    mockApi.tickets.get.mockImplementation((id: string) => Promise.resolve(makeTicket({ id })));
    renderEditor(makeTicket());
    await settled();
    expect(epicSelect().value).toBe("");
    expect(epicSelect().selectedOptions[0].textContent).toBe("No epic");
  });

  it("shows the ticket's epic before the project's epics arrive, and if they never do", async () => {
    mockApi.epics.list.mockRejectedValue(new Error("offline"));
    renderEditor(makeTicket(inViews));
    expect(epicSelect().value).toBe("e-Views");
    await settled();
    expect(optionNames()).toEqual(["No epic", "Views"]);
    expect(epicSelect().value).toBe("e-Views");
  });

  it("saves a chosen epic by id", async () => {
    const { onUpdate } = renderEditor(makeTicket(inViews));
    await settled();
    fireEvent.change(epicSelect(), { target: { value: "e-Realtime" } });
    await act(async () => fireEvent.click(saveButton()!));
    expect(onUpdate).toHaveBeenCalledWith("t1", { epic: "e-Realtime" });
  });

  it("saves No epic as the API's explicit clear", async () => {
    const { onUpdate } = renderEditor(makeTicket(inViews));
    await settled();
    fireEvent.change(epicSelect(), { target: { value: "" } });
    await act(async () => fireEvent.click(saveButton()!));
    expect(onUpdate).toHaveBeenCalledWith("t1", { epic: "" });
  });

  it("leaves the epic out when it wasn't touched, so a change elsewhere survives", async () => {
    const { onUpdate, rerenderWith } = renderEditor(makeTicket(inViews));
    await settled();
    editTitle("Mine");
    const next = makeTicket({ updatedAt: "2026-09-23T18:00:00Z", epic: { id: "e-agents", name: "agents" } });
    mockApi.tickets.get.mockResolvedValue(next);
    await act(async () => rerenderWith({ ticket: next }));
    await act(async () => fireEvent.click(saveButton()!));
    expect(onUpdate).toHaveBeenCalledWith("t1", { title: "Mine" });
  });

  it("keeps the user's epic when the server changed only other fields", async () => {
    const { onUpdate, rerenderWith } = renderEditor(makeTicket(inViews));
    await settled();
    fireEvent.change(epicSelect(), { target: { value: "e-agents" } });
    const next = makeTicket({ updatedAt: "2026-09-23T18:00:00Z", status: "done", ...inViews });
    mockApi.tickets.get.mockResolvedValue(next);
    await act(async () => rerenderWith({ ticket: next }));
    expect(epicSelect().value).toBe("e-agents");
    await act(async () => fireEvent.click(saveButton()!));
    expect(onUpdate).toHaveBeenCalledWith("t1", { epic: "e-agents" });
  });

  it("follows an epic changed elsewhere when nothing is unsaved", async () => {
    const { rerenderWith } = renderEditor(makeTicket(inViews));
    await settled();
    const next = makeTicket({ updatedAt: "2026-09-23T18:00:00Z", epic: { id: "e-Realtime", name: "Realtime" } });
    mockApi.tickets.get.mockResolvedValue(next);
    await act(async () => rerenderWith({ ticket: next }));
    expect(epicSelect().value).toBe("e-Realtime");
    expect(saveButton()).toBeNull();
  });
});

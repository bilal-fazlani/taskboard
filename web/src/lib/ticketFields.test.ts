import { describe, expect, it } from "vitest";
import type { DependencyKind, Ticket } from "../api/client";
import { changedFields, editedWrite, ticketFields, toDateInputValue, type TicketFields } from "./ticketFields";

function makeTicket(overrides: Partial<Ticket> = {}): Ticket {
  return {
    id: "t1",
    projectId: "p1",
    number: 7,
    title: "Ship login page",
    description: "Some **markdown**",
    status: "todo",
    priority: "high",
    dueDate: "2026-10-01T00:00:00Z",
    position: 0,
    createdAt: "",
    updatedAt: "",
    projectPrefix: "AUTH",
    repos: ["acme/auth-web"],
    labels: [{ id: "l1", name: "frontend", color: "#3b82f6" }],
    subtasks: [],
    dependsOn: [{ id: "t2", key: "AUTH-2", title: "Build login UI", status: "todo" }],
    blocks: [],
    epic: { id: "e1", name: "Login" },
    ...overrides,
  };
}

describe("toDateInputValue", () => {
  it("reads the calendar date the server stored, with no timezone shift", () => {
    expect(toDateInputValue("2026-10-01T00:00:00Z")).toBe("2026-10-01");
    expect(toDateInputValue("2026-10-01")).toBe("2026-10-01");
    expect(toDateInputValue(undefined)).toBe("");
    expect(toDateInputValue("")).toBe("");
  });
});

describe("ticketFields", () => {
  it("reads the editable fields as the controls hold them", () => {
    expect(ticketFields(makeTicket())).toEqual({
      title: "Ship login page",
      description: "Some **markdown**",
      status: "todo",
      priority: "high",
      dueDate: "2026-10-01",
      epic: "e1",
      repos: ["acme/auth-web"],
      labels: ["frontend"],
      dependsOn: [d("t2")],
      surfacedFrom: "",
    });
  });

  it("reads each dependency's kind and note, needs_work and no note when the API leaves them out", () => {
    const fields = ticketFields(
      makeTicket({
        dependsOn: [
          { id: "t2", key: "AUTH-2", title: "Build login UI", status: "todo" },
          { id: "t3", key: "AUTH-3", title: "Session store", status: "todo", kind: "conflict_only", note: "store.go" },
        ],
        surfacedFrom: { id: "t9", key: "AUTH-9", title: "Audit", status: "done" },
      }),
    );
    expect(fields.dependsOn).toEqual([d("t2"), d("t3", "conflict_only", "store.go")]);
    expect(fields.surfacedFrom).toBe("t9");
  });

  it("reads a ticket without an epic, which the API leaves out, as no epic", () => {
    expect(ticketFields(makeTicket({ epic: undefined })).epic).toBe("");
  });

  it("reads the empty collections the API leaves out as empty", () => {
    const bare = ticketFields(makeTicket({ repos: undefined, labels: [], dependsOn: undefined, dueDate: undefined }));
    expect(bare).toMatchObject({ repos: [], labels: [], dependsOn: [], dueDate: "" });
  });
});

/** A dependency as the fields hold it. */
const d = (ticket: string, kind: DependencyKind = "needs_work", note = "") => ({ ticket, kind, note });

const base = ticketFields(makeTicket());
const edited = (overrides: Partial<TicketFields>): TicketFields => ({ ...base, ...overrides });

describe("changedFields", () => {
  it("finds nothing between a version and itself", () => {
    expect(changedFields(base, { ...base, repos: [...base.repos] })).toEqual([]);
  });

  it("finds each changed field, in a stable order", () => {
    expect(changedFields(base, edited({ priority: "low", title: "New" }))).toEqual(["title", "priority"]);
  });

  it("compares collections by their values", () => {
    expect(changedFields(base, edited({ labels: ["frontend"] }))).toEqual([]);
    expect(changedFields(base, edited({ labels: [] }))).toEqual(["labels"]);
    expect(changedFields(base, edited({ repos: ["acme/auth-web", "acme/api"] }))).toEqual(["repos"]);
    expect(changedFields(base, edited({ dependsOn: [d("t3"), d("t2")] }))).toEqual(["dependsOn"]);
    expect(changedFields(base, edited({ dependsOn: [d("t3")] }))).toEqual(["dependsOn"]);
  });

  it("reads repos, labels and dependencies as sets, so another order is no change", () => {
    const many = edited({
      repos: ["acme/auth-web", "acme/api"],
      labels: ["frontend", "urgent"],
      dependsOn: [d("t2"), d("t3")],
    });
    const reordered = edited({
      repos: ["acme/api", "acme/auth-web"],
      labels: ["urgent", "frontend"],
      dependsOn: [d("t3"), d("t2")],
    });
    expect(changedFields(many, reordered)).toEqual([]);
    expect(editedWrite(many, reordered)).toEqual({});
  });

  it("still sees a swap of one member for another as a change", () => {
    const many = edited({ labels: ["frontend", "urgent"], dependsOn: [d("t2"), d("t3")] });
    expect(changedFields(many, edited({ labels: ["frontend", "backend"], dependsOn: [d("t3"), d("t4")] }))).toEqual([
      "labels",
      "dependsOn",
    ]);
  });

  it("sees a changed kind or note on the same dependency as a change", () => {
    expect(changedFields(base, edited({ dependsOn: [d("t2", "conflict_only")] }))).toEqual(["dependsOn"]);
    expect(changedFields(base, edited({ dependsOn: [d("t2", "needs_work", "store.go")] }))).toEqual(["dependsOn"]);
    expect(changedFields(base, edited({ dependsOn: [d("t2")] }))).toEqual([]);
  });

  it("counts a set, changed or removed surfaced-from link as a change", () => {
    expect(changedFields(base, edited({ surfacedFrom: "t9" }))).toEqual(["surfacedFrom"]);
    const linked = edited({ surfacedFrom: "t9" });
    expect(changedFields(linked, edited({ surfacedFrom: "t8" }))).toEqual(["surfacedFrom"]);
    expect(changedFields(linked, edited({ surfacedFrom: "" }))).toEqual(["surfacedFrom"]);
  });

  it("counts clearing the due date as a change", () => {
    expect(changedFields(base, edited({ dueDate: "" }))).toEqual(["dueDate"]);
  });

  it("counts a changed or cleared epic as a change", () => {
    expect(changedFields(base, edited({ epic: "e2" }))).toEqual(["epic"]);
    expect(changedFields(base, edited({ epic: "" }))).toEqual(["epic"]);
  });
});

describe("editedWrite", () => {
  it("sends only the fields the user changed", () => {
    expect(editedWrite(base, edited({ status: "done" }))).toEqual({ status: "done" });
  });

  it("sends nothing at all when nothing changed", () => {
    expect(editedWrite(base, { ...base })).toEqual({});
  });

  it("sends an empty due date as the API's explicit clear", () => {
    const write = editedWrite(base, edited({ dueDate: "" }));
    expect(write).toEqual({ dueDate: "" });
    expect("dueDate" in write).toBe(true);
  });

  it("sends a chosen epic by id, and no epic as the API's explicit clear", () => {
    expect(editedWrite(base, edited({ epic: "e2" }))).toEqual({ epic: "e2" });
    const cleared = editedWrite(base, edited({ epic: "" }));
    expect(cleared).toEqual({ epic: "" });
    expect("epic" in cleared).toBe(true);
  });

  it("leaves an untouched epic out, so one changed elsewhere survives the save", () => {
    const write = editedWrite(base, edited({ priority: "low" }));
    expect("epic" in write).toBe(false);
  });

  it("leaves an untouched due date out entirely, whatever shape it arrived in", () => {
    // An RFC 3339 due date read into the date input and never touched must not
    // be re-sent: an omitted field is the API's "leave it unchanged".
    expect(editedWrite(base, edited({ title: "New" }))).toEqual({ title: "New" });
  });

  it("sends the whole dependency list, kinds and notes included, when any of it changed", () => {
    const current = edited({ dependsOn: [d("t2", "conflict_only", "store.go"), d("t3")] });
    const write = editedWrite(base, current);
    expect(write).toEqual({
      dependsOn: [
        { ticket: "t2", kind: "conflict_only", note: "store.go" },
        { ticket: "t3", kind: "needs_work", note: "" },
      ],
    });
    current.dependsOn[0].note = "later";
    expect(write.dependsOn?.[0].note).toBe("store.go");
  });

  it("sends a removed surfaced-from link as the API's explicit clear", () => {
    const linked = edited({ surfacedFrom: "t9" });
    const write = editedWrite(linked, edited({ surfacedFrom: "" }));
    expect(write).toEqual({ surfacedFrom: "" });
  });

  it("sends collections as copies, so later edits cannot reach a sent payload", () => {
    const current = edited({ labels: ["frontend", "urgent"] });
    const write = editedWrite(base, current);
    expect(write.labels).toEqual(["frontend", "urgent"]);
    current.labels.push("later");
    expect(write.labels).toEqual(["frontend", "urgent"]);
  });

  it("sends every field when the user changed every field", () => {
    const all = {
      title: "New",
      description: "New body",
      status: "done",
      priority: "low",
      dueDate: "2026-12-24",
      epic: "",
      repos: ["acme/api"],
      labels: ["urgent"],
      dependsOn: [d("t3", "conflict_only", "store.go")],
      surfacedFrom: "t9",
    };
    expect(editedWrite(base, all)).toEqual(all);
  });
});

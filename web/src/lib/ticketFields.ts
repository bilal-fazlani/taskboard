// The editor's view of a ticket: the fields it edits, how they are read off a
// ticket, and what an edit of them sends.
//
// Save merges by field. The editor keeps the server version it last synced
// from, and sends only the fields whose value has moved away from it, so a
// change made elsewhere to a field the user never touched survives the save
// rather than being written back over. Where the user and the server both
// changed the same field, the user's value is what gets sent: they were told
// the ticket changed and kept editing anyway.

import type { DependencyWrite, Ticket, TicketWrite } from "../api/client";

/** The fields the editor edits, as its controls hold them. */
export interface TicketFields {
  title: string;
  description: string;
  status: string;
  priority: string;
  /** As <input type="date"> holds it: "YYYY-MM-DD", or "" for no due date. */
  dueDate: string;
  /** The epic's id, or "" for no epic. */
  epic: string;
  repos: string[];
  /** Label names, which is what the API takes. */
  labels: string[];
  /** Dependencies by ticket id, with their kind and note, which is what the API takes. */
  dependsOn: DependencyWrite[];
  /** The id of the ticket this one was surfaced from, or "" for none. */
  surfacedFrom: string;
}

export type FieldKey = keyof TicketFields;

export const FIELD_KEYS: readonly FieldKey[] = [
  "title",
  "description",
  "status",
  "priority",
  "dueDate",
  "epic",
  "repos",
  "labels",
  "dependsOn",
  "surfacedFrom",
];

// The API returns dueDate as an RFC 3339 timestamp at midnight UTC (e.g.
// "2026-10-01T00:00:00Z"), which an <input type="date"> rejects outright — it
// wants a bare "yyyy-MM-dd". Slicing off everything from "T" onward reads the
// calendar date exactly as the server stored it, with no Date object and so
// no local-timezone conversion: parsing that timestamp with `new Date(...)`
// and formatting it back would shift the displayed day for anyone west of
// UTC. A plain "YYYY-MM-DD" (as CreateTicketModal sends and the API echoes
// back) passes through unchanged since it has no "T" to slice at.
export function toDateInputValue(dueDate?: string): string {
  return dueDate ? dueDate.slice(0, 10) : "";
}

/** A ticket's fields as the editor's controls would hold them. */
export function ticketFields(ticket: Ticket): TicketFields {
  return {
    title: ticket.title,
    description: ticket.description,
    status: ticket.status,
    priority: ticket.priority,
    dueDate: toDateInputValue(ticket.dueDate),
    epic: ticket.epic?.id ?? "",
    repos: ticket.repos || [],
    labels: (ticket.labels || []).map((l) => l.name),
    dependsOn: (ticket.dependsOn || []).map(dependencyWrite),
    surfacedFrom: ticket.surfacedFrom?.id ?? "",
  };
}

/** A dependency as the API takes it back: its ticket's id, its kind and its note. */
export function dependencyWrite(ref: { id: string; kind?: DependencyWrite["kind"]; note?: string }): DependencyWrite {
  return { ticket: ref.id, kind: ref.kind ?? "needs_work", note: ref.note ?? "" };
}

// Repos, labels and dependencies are sets: the server keeps no order for
// them and may list the same ones in another order, which is no change. A
// dependency is the same only with the same kind and note.
function member(value: string | DependencyWrite): string {
  return typeof value === "string" ? value : [value.ticket, value.kind, value.note ?? ""].join("\u0000");
}

function same(a: TicketFields[FieldKey], b: TicketFields[FieldKey]): boolean {
  if (Array.isArray(a) && Array.isArray(b)) {
    const inA = new Set(a.map(member));
    const inB = new Set(b.map(member));
    return inA.size === inB.size && [...inA].every((value) => inB.has(value));
  }
  return a === b;
}

/** The fields that differ between two versions, in FIELD_KEYS order. */
export function changedFields(a: TicketFields, b: TicketFields): FieldKey[] {
  return FIELD_KEYS.filter((key) => !same(a[key], b[key]));
}

/**
 * What to send for an edit of `base` into `current`: the changed fields and
 * nothing else. The API reads an omitted field as "leave it unchanged", which
 * is what makes a field-by-field save possible — including for dueDate and
 * the epic, where an omitted field leaves it alone and an empty string clears
 * it.
 */
export function editedWrite(base: TicketFields, current: TicketFields): TicketWrite {
  const write: TicketWrite = {};
  for (const key of changedFields(base, current)) {
    switch (key) {
      case "repos":
      case "labels":
        write[key] = [...current[key]];
        break;
      case "dependsOn":
        // The API replaces the whole list, kinds and notes included.
        write.dependsOn = current.dependsOn.map((d) => ({ ...d }));
        break;
      default:
        write[key] = current[key];
    }
  }
  return write;
}

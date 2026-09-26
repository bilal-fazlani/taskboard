export interface Project {
  id: string;
  name: string;
  prefix: string;
  description: string;
  icon: string;
  color: string;
  status: string;
  createdAt: string;
  updatedAt: string;
  // How agents should work on the project's tickets. Only reads of one
  // project (get, create, update) carry it; the list leaves it out.
  agentInstructions?: string;
  // Whether the project has agent instructions; only the list carries it.
  hasAgentInstructions?: boolean;
}

export interface Label {
  id: string;
  name: string;
  color: string;
  ticketCount: number;
}

/**
 * A label as it appears embedded in a ticket: no ticketCount. Computing each
 * label's real ticket count there would be an extra query per label on every
 * ticket in a list, and nothing reads it there. Fetch Label on its own
 * (list_labels, or a create/update label response) for the real count.
 */
export interface EmbeddedLabel {
  id: string;
  name: string;
  color: string;
}

export interface EpicRef {
  id: string;
  name: string;
}

export interface EpicProgress {
  /** One entry per status, zeros included. */
  counts: Record<string, number>;
  total: number;
  /** At least one ticket and every one done. An empty epic is not complete. */
  complete: boolean;
  lastActivityAt: string | null;
}

export interface Epic extends EpicProgress {
  id: string;
  projectId: string;
  name: string;
  description?: string;
  createdAt: string;
  updatedAt: string;
  /** How many documents the epic has. */
  documentCount?: number;
  /** Its documents, without content; only a single epic carries them. */
  documents?: DocumentMeta[];
}

export interface EpicList {
  epics: Epic[];
  noEpic: EpicProgress;
}

export interface TicketRef {
  id: string;
  key: string;
  title: string;
  status: string;
}

export interface Subtask {
  id: string;
  ticketId: string;
  title: string;
  completed: boolean;
  position: number;
}

export interface Ticket {
  id: string;
  projectId: string;
  number: number;
  title: string;
  description: string;
  status: string;
  priority: string;
  dueDate?: string;
  position: number;
  createdAt: string;
  updatedAt: string;
  projectPrefix: string;
  repos?: string[];
  labels: EmbeddedLabel[];
  subtasks: Subtask[];
  dependsOn?: TicketRef[];
  blocks?: TicketRef[];
  /** Left out when the ticket has no epic. */
  epic?: EpicRef;
  /** How many times the ticket has entered agent_review. Optional because
   * fixtures and mocks leave it out; a missing value means 0, not unknown. */
  reviewRounds?: number;
  /** How many documents the ticket has. Lists and the full ticket carry it. */
  documentCount?: number;
  /** The documents, without content. Only the full ticket carries it. */
  documents?: DocumentMeta[];
}

/** One change of a ticket's status. The first, written at creation, has an empty fromStatus. */
export interface StatusChange {
  id: string;
  ticketId: string;
  fromStatus: string;
  toStatus: string;
  note: string;
  createdAt: string;
}

/** The formats a document holding text may have. */
export type TextDocumentFormat = "markdown" | "html";
/** The image formats (internal/models/document.go); images are uploaded as files. */
export type ImageDocumentFormat = "png" | "jpeg" | "gif" | "webp";
export type DocumentFormat = TextDocumentFormat | ImageDocumentFormat;

/** A document without its content, as lists carry it. size is in bytes. */
export interface DocumentMeta {
  id: string;
  /** Exactly one of ticketId and epicId is set: the document's owner. */
  ticketId?: string;
  epicId?: string;
  name: string;
  format: DocumentFormat;
  size: number;
  /** An image's size in pixels, as shown; absent for text documents. */
  width?: number;
  height?: number;
  /** Counts content saves; a rename leaves it alone. */
  revision: number;
  createdAt: string;
  updatedAt: string;
}

export interface DocumentWithContent extends DocumentMeta {
  content: string;
}

/**
 * A place in an owner's text that uses one of its images: the ticket's
 * description, or one of the owner's markdown or HTML documents (by id and
 * display name, "Plan.md").
 */
export type ImagePlace = { kind: "description" } | { kind: "document"; documentId: string; name: string };

/** What a document belongs to: one ticket or one epic. */
export type DocumentOwnerRef = { ticketId: string } | { epicId: string };

/**
 * Fields accepted when creating or updating a ticket. Deliberately NOT
 * Partial<Ticket>: the API takes label names and dependency IDs-or-keys as
 * strings, whereas a Ticket carries resolved Label and TicketRef objects.
 */
export interface TicketWrite {
  projectId?: string;
  title?: string;
  description?: string;
  status?: string;
  priority?: string;
  dueDate?: string;
  position?: number;
  repos?: string[];
  labels?: string[];
  dependsOn?: string[];
  /** An epic's name or id in the ticket's project; "" clears it, and leaving it out leaves it alone. */
  epic?: string;
  /** Saved in the ticket's history with a status change; ignored without one. */
  note?: string;
}

export interface BoardColumn {
  status: string;
  tickets: Ticket[];
}

export interface Board {
  projectId: string;
  columns: BoardColumn[];
}

async function request<T>(url: string, options?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    headers: { "Content-Type": "application/json" },
    ...options,
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`API error ${res.status}: ${text}`);
  }
  if (res.status === 204) return undefined as T;
  return res.json();
}

export const api = {
  projects: {
    list: () => request<Project[]>("/api/projects"),
    get: (id: string) => request<Project>(`/api/projects/${id}`),
    create: (data: Partial<Project>) =>
      request<Project>("/api/projects", {
        method: "POST",
        body: JSON.stringify(data),
      }),
    update: (id: string, data: Partial<Project>) =>
      request<Project>(`/api/projects/${id}`, {
        method: "PUT",
        body: JSON.stringify(data),
      }),
    delete: (id: string) =>
      request<void>(`/api/projects/${id}`, { method: "DELETE" }),
  },

  tickets: {
    list: (params?: {
      projectId?: string;
      status?: string;
      priority?: string;
      label?: string;
      repo?: string;
    }) => {
      const qs = new URLSearchParams(
        Object.entries(params ?? {}).filter(([, v]) => v) as [string, string][]
      ).toString();
      return request<Ticket[]>(`/api/tickets${qs ? `?${qs}` : ""}`);
    },
    get: (id: string) => request<Ticket>(`/api/tickets/${id}`),
    create: (data: TicketWrite) =>
      request<Ticket>("/api/tickets", {
        method: "POST",
        body: JSON.stringify(data),
      }),
    update: (id: string, data: TicketWrite) =>
      request<Ticket>(`/api/tickets/${id}`, {
        method: "PUT",
        body: JSON.stringify(data),
      }),
    delete: (id: string) =>
      request<void>(`/api/tickets/${id}`, { method: "DELETE" }),
    history: (id: string) => request<StatusChange[]>(`/api/tickets/${id}/history`),
    move: (id: string, status: string, position?: number) =>
      request<Ticket>(`/api/tickets/${id}/move`, {
        method: "POST",
        body: JSON.stringify({ status, position }),
      }),
    addSubtask: (id: string, title: string) =>
      request<Subtask>(`/api/tickets/${id}/subtasks`, {
        method: "POST",
        body: JSON.stringify({ title }),
      }),
  },

  subtasks: {
    toggle: (id: string) =>
      request<Subtask>(`/api/subtasks/${id}/toggle`, { method: "POST" }),
    delete: (id: string) =>
      request<void>(`/api/subtasks/${id}`, { method: "DELETE" }),
  },

  labels: {
    list: () => request<Label[]>("/api/labels"),
    create: (data: Partial<Label>) =>
      request<Label>("/api/labels", {
        method: "POST",
        body: JSON.stringify(data),
      }),
    update: (id: string, data: Partial<Label>) =>
      request<Label>(`/api/labels/${id}`, {
        method: "PUT",
        body: JSON.stringify(data),
      }),
    delete: (id: string) =>
      request<void>(`/api/labels/${id}`, { method: "DELETE" }),
  },

  epics: {
    list: (projectId: string) =>
      request<EpicList>(`/api/epics?projectId=${encodeURIComponent(projectId)}`),
    create: (data: { projectId: string; name: string; description?: string }) =>
      request<Epic>("/api/epics", {
        method: "POST",
        body: JSON.stringify(data),
      }),
    update: (id: string, data: { name?: string; description?: string }) =>
      request<Epic>(`/api/epics/${id}`, {
        method: "PUT",
        body: JSON.stringify(data),
      }),
    delete: (id: string) =>
      request<void>(`/api/epics/${id}`, { method: "DELETE" }),
  },

  documents: {
    list: (owner: DocumentOwnerRef) =>
      request<DocumentMeta[]>(
        "ticketId" in owner ? `/api/tickets/${owner.ticketId}/documents` : `/api/epics/${owner.epicId}/documents`,
      ),
    get: (id: string) => request<DocumentWithContent>(`/api/documents/${encodeURIComponent(id)}`),
    /** Add a document; a refused name or an unknown owner is a 400 with the store's message. */
    create: (data: DocumentOwnerRef & { name: string; format: TextDocumentFormat; content: string }) =>
      request<DocumentWithContent>("/api/documents", { method: "POST", body: JSON.stringify(data) }),
    /**
     * Rename and/or replace the content. With `expectedRevision`, a content
     * save the document has moved past is refused with a 409 whose body
     * carries the document as it is now (conflictDocument reads it).
     */
    update: (id: string, data: { name?: string; content?: string; expectedRevision?: number }) =>
      request<DocumentWithContent>(`/api/documents/${encodeURIComponent(id)}`, {
        method: "PUT",
        body: JSON.stringify(data),
      }),
    delete: (id: string) => request<void>(`/api/documents/${encodeURIComponent(id)}`, { method: "DELETE" }),
    downloadUrl: (id: string) => `/api/documents/${encodeURIComponent(id)}/download`,
    /** The page itself, served sandboxed; a new revision is a new URL, so the frame reloads. */
    rawUrl: (id: string, revision: number) => `/api/documents/${encodeURIComponent(id)}/raw?rev=${revision}`,
    /**
     * Add an image from its file, sent as it is. The server takes the format
     * and the name from `filename`; a refused file is a 400 with the store's
     * message.
     */
    createImage: (owner: DocumentOwnerRef, file: File) => {
      const q = new URLSearchParams(
        "ticketId" in owner ? { ticket: owner.ticketId, filename: file.name } : { epic: owner.epicId, filename: file.name },
      );
      return request<DocumentMeta>(`/api/documents/images?${q.toString()}`, {
        method: "POST",
        headers: { "Content-Type": file.type || "application/octet-stream" },
        body: file,
      });
    },
    /** An image's file; a new revision is a new URL, so an agent's replace shows at once. */
    imageUrl: (id: string, revision: number) => `/api/documents/${encodeURIComponent(id)}/image?rev=${revision}`,
    /**
     * Where an image is used in its owner's text, worked out from the text
     * now: the description and the documents whose references name it.
     */
    usage: (id: string) => request<{ places: ImagePlace[] }>(`/api/documents/${encodeURIComponent(id)}/usage`),
    /** An image's small thumbnail, by revision as imageUrl is. */
    thumbnailUrl: (id: string, revision: number) => `/api/documents/${encodeURIComponent(id)}/thumbnail?rev=${revision}`,
    /**
     * The ids of the project's tickets that have a document whose display
     * name or readable text contains `q`, ignoring case. Epic documents are
     * not searched.
     */
    search: (q: string, projectId: string) =>
      request<{ ticketIds: string[] }>(`/api/documents/search?${new URLSearchParams({ q, projectId }).toString()}`),
  },

  board: {
    get: (projectId?: string) =>
      request<Board>(`/api/board${projectId ? `?projectId=${projectId}` : ""}`),
  },
};

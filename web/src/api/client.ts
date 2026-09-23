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
}

export interface Label {
  id: string;
  name: string;
  color: string;
  ticketCount: number;
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
  labels: Label[];
  subtasks: Subtask[];
  dependsOn?: TicketRef[];
  blocks?: TicketRef[];
}

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

  board: {
    get: (projectId?: string) =>
      request<Board>(`/api/board${projectId ? `?projectId=${projectId}` : ""}`),
  },
};

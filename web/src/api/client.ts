// Project, Ticket, Epic and Board below are the app-facing shapes: every
// field the Go models mark `omitempty` (internal/models) and that carries no
// meaning of its own when absent is normalised here at the API boundary, so
// the rest of the app can read a string or an array without a fallback.
// agentInstructions, hasAgentInstructions, dueDate, epic and documents are
// left optional on purpose: their absence means something an empty value
// doesn't (not fetched, no due date, no epic, ...). See the Raw* types and
// normalise* functions below `request` for exactly what each one does.
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
  description: string;
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

/** needs_work: builds on the other ticket. conflict_only: waits for it only so the two don't change the same files. */
export type DependencyKind = "needs_work" | "conflict_only";

export interface TicketRef {
  id: string;
  key: string;
  title: string;
  status: string;
  /** On a dependsOn or blocks entry: the dependency's kind. Left out elsewhere. */
  kind?: DependencyKind;
  /** On a dependsOn or blocks entry: its note, left out when it has none. */
  note?: string;
}

/** One dependsOn entry as the API takes it; a write sends the whole list. */
export interface DependencyWrite {
  /** The ticket's id or display key. */
  ticket: string;
  kind: DependencyKind;
  note?: string;
}

export interface Subtask {
  id: string;
  ticketId: string;
  title: string;
  completed: boolean;
  position: number;
}

/** One worker in a session: the main agent, or a subagent it launched. It
 * lives as long as its process and is not a lasting identity. Ticket.agent's
 * shape: no nested session (unlike EntryAgent below, which entries need
 * eagerly to show who wrote them, and which extends this). */
export interface Agent {
  id: string;
  sessionId: string;
  role: string;
  model: string;
  provider: string;
  createdAt: string;
  lastSeenAt: string;
  /** When any agent of this agent's session was last seen. Optional because
   * fixtures leave it out; a real read always sets it. */
  sessionLastSeenAt?: string;
  /** Whether the agent's session has gone unseen past the stale threshold.
   * Optional for the same reason. */
  stale?: boolean;
}

/** One agent as GET /api/agents lists it, or GET /api/agents/{id} answers
 * it: the agent with its session (EntryAgent, below), and the tickets it
 * currently holds. */
export interface AgentListItem extends EntryAgent {
  heldTickets: TicketRef[];
}

/** The types of user input an agent can ask the person for. */
export type UserInputType = "approval" | "question";

/** An agent's request for user input on a ticket. A ticket has at most one
 * unanswered request at a time. */
export interface TicketRequest {
  id: string;
  ticketId: string;
  /** The agent that asked, and that collects the answer. */
  agentId: string;
  /** A type this client does not know yet is kept, not refused; `string & {}`
   * keeps the known ones offered by autocomplete. */
  type: UserInputType | (string & {});
  prompt: string;
  /** For a question, suggested answers: the person may answer with any
   * non-empty text instead, and one matching a choice, ignoring case, is
   * stored as the choice is written. Empty when the answer is free. An
   * approval's choices change nothing. */
  choices: string[];
  /** A question's answer is the person's text (or the choice it matched);
   * an approval's is always "approved" or "declined". Left out until answered. */
  answer?: string;
  /** Who answered: the local person today. */
  answeredBy?: string;
  /** The person's optional note, sent with the answer of either type. */
  note?: string;
  createdAt: string;
  answeredAt?: string;
}

/** What the person sends to answer a request: see TicketRequest.answer. */
export interface RequestAnswer {
  answer: string;
  note?: string;
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
  repos: string[];
  labels: EmbeddedLabel[];
  subtasks: Subtask[];
  dependsOn: TicketRef[];
  blocks: TicketRef[];
  /** The ticket this one was found during; left out when there is none. */
  surfacedFrom?: TicketRef;
  /** The tickets found during this one. Only the full ticket carries it. */
  surfaced?: TicketRef[];
  /** Left out when the ticket has no epic. */
  epic?: EpicRef;
  /** How many times the ticket has entered agent_review. Optional because
   * fixtures and mocks leave it out; a missing value means 0, not unknown. */
  reviewRounds?: number;
  /** When a done ticket last moved to done, from its status history; left
   * out when it is not done. One done before the history began carries its
   * createdAt, earlier than every logged move. Only lists carry it. */
  doneAt?: string;
  /** How many documents the ticket has. Lists and the full ticket carry it. */
  documentCount?: number;
  /** The documents, without content. Only the full ticket carries it. */
  documents?: DocumentMeta[];
  /** Where the ticket's work lives and where it landed. Only the full ticket
   * carries it, and only when a field is set. */
  delivery?: Delivery;
  /** Open notes on the ticket's epic and on its project, counted but not
   * read. Only the full ticket carries them, and each is left out at 0. */
  epicOpenNotes?: number;
  projectOpenNotes?: number;
  /** The agent holding the ticket; left out when no agent holds it. */
  agent?: Agent;
  /** The request for user input the ticket waits on; left out when it has
   * none. Only set while the ticket is needs_user_input. */
  openRequest?: TicketRequest;
}

/** The entry types (internal/models/entry.go). The set is open: an unknown
 * type from a newer server is kept, not refused. */
export type EntryType = "decision" | "learning" | "hand_off" | "proof" | "review" | "note";

/** What an entry sits on: one project, epic or ticket. */
export type EntryOwnerRef = { ticketId: string } | { epicId: string } | { projectId: string };

/**
 * One entry: a short typed record of why the work is as it is. Entries are
 * never edited; a later one of the same type can replace one (`replaces`),
 * which is then kept with `replacedBy` set. Exactly one of agentId and
 * authorName names the writer: an agent, or the person.
 */
export interface Entry {
  id: string;
  projectId?: string;
  epicId?: string;
  ticketId?: string;
  type: EntryType | string;
  text: string;
  agentId?: string;
  authorName?: string;
  /** A decision's source: "agent", or "person" for the person's call recorded by an agent. */
  source?: string;
  replaces?: string;
  replacedBy?: string;
  /** The entry a note points at: the one the person challenges. */
  about?: string;
  /** The agent that handled a note, and when; absent while it is open. */
  handledBy?: string;
  handledAt?: string;
  /** A review's verdict ("approve" or "changes"), its finding counts by
   * severity, and the name of the document holding its full report. */
  verdict?: string;
  findings?: Record<string, number>;
  reportDocument?: string;
  createdAt: string;
}

/** A session: one conversation in a vendor's tool. */
export interface AgentSession {
  id: string;
  vendor: string;
  vendorSessionId: string;
  machine: string;
  resumeCommand: string;
  webUrl?: string;
  createdAt: string;
}

/** An agent that wrote an entry or handled a note, with its session. Its
 * agent fields are Agent's own (declared once there); sessionLastSeenAt and
 * stale are optional on Agent itself because fixtures leave them out. */
export interface EntryAgent extends Agent {
  session: AgentSession;
}

/**
 * A run of one owner's entries, newest first, with the agents they name
 * keyed by id. When hasMore is true, the next (older) page is the one read
 * with before set to nextBefore.
 */
export interface EntryPage {
  entries: Entry[];
  total: number;
  hasMore: boolean;
  nextBefore?: string;
  agents: Record<string, EntryAgent>;
}

/** A new entry from the person: a note, optionally pointing at the entry it challenges. */
export interface NoteWrite {
  type: "note";
  text: string;
  about?: string;
}

/** A ticket's delivery fields, set by agents through MCP, the CLI or the API.
 * Each field is left out when it is not set. */
export interface Delivery {
  branch?: string;
  worktree?: string;
  prUrl?: string;
  /** The commits the ticket landed as, in the order they were given. */
  landedCommits?: LandedCommit[];
}

/** One commit a ticket landed as: a lowercase sha (7 to 64 hex characters,
 * short or full) and the repo it landed in. */
export interface LandedCommit {
  sha: string;
  repo: string;
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

/**
 * One status change in a project's activity feed, with its ticket's key,
 * title and epic as they are now. The API leaves epic out for a ticket
 * without one.
 */
export interface ProjectActivityEntry extends StatusChange {
  ticketKey: string;
  ticketTitle: string;
  epic?: { id: string; name: string };
}

/**
 * A run of a project's activity, newest first. When hasMore is true, the next
 * (older) page is the one read with before set to nextBefore.
 */
export interface ActivityPage {
  entries: ProjectActivityEntry[];
  hasMore: boolean;
  nextBefore?: string;
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
 * Partial<Ticket>: the API takes label names as strings and dependencies as
 * {ticket, kind, note} objects, whereas a Ticket carries resolved Label and
 * TicketRef objects.
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
  /** The whole list of dependencies, kinds and notes included. */
  dependsOn?: DependencyWrite[];
  /** The ticket this one was found during, by id or key; "" removes the link, and leaving it out leaves it alone. */
  surfacedFrom?: string;
  /** An epic's name or id in the ticket's project; "" clears it, and leaving it out leaves it alone. */
  epic?: string;
  /** Saved in the ticket's history with a status change; ignored without one. */
  note?: string;
}

/** The build serving the app, as GET /api/version answers it. */
export interface BuildInfo {
  /** The release tag, or `git describe` output for a Makefile build; "unknown" when nothing recorded it. */
  version: string;
  /** The full hash of the commit it was built from, or "unknown". */
  commit: string;
  /** True for every build but a release binary and the one `make install` builds. */
  dev: boolean;
}

export interface BoardColumn {
  status: string;
  tickets: Ticket[];
}

export interface Board {
  projectId: string;
  columns: BoardColumn[];
}

/** Where a ticket in agent_review stands, from its latest `Review <round>`
 * document: its reviewer is still at work, or the review approved it (it
 * waits on the person) or asked for changes. */
export type NowReview = "running" | "approved" | "changes";

/** A waiting ticket's open request for user input, as the Now page shows it. */
export interface NowRequest {
  type: UserInputType;
  prompt: string;
  /** When the agent asked. */
  createdAt: string;
}

/** A ticket in progress, waiting on the person or in review, as the Now page shows it. */
export interface NowTicket {
  id: string;
  key: string;
  title: string;
  status: string;
  projectPrefix: string;
  subtasksDone: number;
  subtasksTotal: number;
  /** How many times it has entered agent_review. */
  reviewRounds: number;
  /** When it last entered its current status. */
  since: string;
  /** Set only on a ticket in agent_review. */
  review?: NowReview;
  /** What it waits on; set only on a ticket in needs_user_input. */
  request?: NowRequest;
}

/** A ticket that moved to done in the last day, with its landed commits. */
export interface LandedTicket {
  id: string;
  key: string;
  title: string;
  projectPrefix: string;
  doneAt: string;
  commits: LandedCommit[];
}

/** GET /api/now: what is moving now, and what just landed. */
export interface Now {
  inProgress: NowTicket[];
  /** The tickets waiting on the person (needs_user_input), oldest wait first. */
  waiting: NowTicket[];
  inReview: NowTicket[];
  landed: LandedTicket[];
}

// Wire (raw) shapes: what the Go models (internal/models) actually send,
// `omitempty` fields marked optional exactly as they are on the struct. Kept
// private to this module — the rest of the app never sees these, only the
// normalised app-facing types above, produced by the normalise* functions
// below.
interface RawProject extends Omit<Project, "description" | "icon" | "color"> {
  description?: string;
  icon?: string;
  color?: string;
}

interface RawTicket extends Omit<Ticket, "description" | "projectPrefix" | "repos" | "labels" | "subtasks" | "dependsOn" | "blocks"> {
  description?: string;
  projectPrefix?: string;
  repos?: string[];
  labels?: EmbeddedLabel[];
  subtasks?: Subtask[];
  dependsOn?: TicketRef[];
  blocks?: TicketRef[];
}

interface RawEpic extends Omit<Epic, "description"> {
  description?: string;
}

interface RawEpicList {
  epics?: RawEpic[];
  noEpic: EpicProgress;
}

interface RawBoardColumn extends Omit<BoardColumn, "tickets"> {
  tickets?: RawTicket[];
}

interface RawBoard extends Omit<Board, "projectId" | "columns"> {
  projectId?: string;
  columns?: RawBoardColumn[];
}

interface RawEntryPage extends Omit<EntryPage, "entries" | "agents"> {
  entries?: Entry[];
  agents?: Record<string, EntryAgent>;
}

/** The entries route for an owner. */
function entriesUrl(owner: EntryOwnerRef): string {
  if ("ticketId" in owner) return `/api/tickets/${encodeURIComponent(owner.ticketId)}/entries`;
  if ("epicId" in owner) return `/api/epics/${encodeURIComponent(owner.epicId)}/entries`;
  return `/api/projects/${encodeURIComponent(owner.projectId)}/entries`;
}

function normalizeProject(raw: RawProject): Project {
  return { ...raw, description: raw.description ?? "", icon: raw.icon ?? "", color: raw.color ?? "" };
}

function normalizeTicket(raw: RawTicket): Ticket {
  return {
    ...raw,
    description: raw.description ?? "",
    projectPrefix: raw.projectPrefix ?? "",
    repos: raw.repos ?? [],
    labels: raw.labels ?? [],
    subtasks: raw.subtasks ?? [],
    dependsOn: raw.dependsOn ?? [],
    blocks: raw.blocks ?? [],
  };
}

function normalizeEpic(raw: RawEpic): Epic {
  return { ...raw, description: raw.description ?? "" };
}

function normalizeEpicList(raw: RawEpicList): EpicList {
  return { epics: (raw.epics ?? []).map(normalizeEpic), noEpic: raw.noEpic };
}

function normalizeBoard(raw: RawBoard): Board {
  return {
    projectId: raw.projectId ?? "",
    columns: (raw.columns ?? []).map((c) => ({ status: c.status, tickets: (c.tickets ?? []).map(normalizeTicket) })),
  };
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
    list: () => request<RawProject[]>("/api/projects").then((raw) => raw.map(normalizeProject)),
    get: (id: string) => request<RawProject>(`/api/projects/${id}`).then(normalizeProject),
    create: (data: Partial<Project>) =>
      request<RawProject>("/api/projects", {
        method: "POST",
        body: JSON.stringify(data),
      }).then(normalizeProject),
    update: (id: string, data: Partial<Project>) =>
      request<RawProject>(`/api/projects/${id}`, {
        method: "PUT",
        body: JSON.stringify(data),
      }).then(normalizeProject),
    delete: (id: string) =>
      request<void>(`/api/projects/${id}`, { method: "DELETE" }),
    /**
     * One page of the project's status changes, newest first. epics narrows
     * it to tickets in any of them (names, or "none" for no epic); before is
     * the previous page's nextBefore.
     */
    activity: (id: string, params: { epics?: readonly string[]; before?: string; limit?: number } = {}) => {
      const qs = new URLSearchParams();
      for (const epic of params.epics ?? []) qs.append("epic", epic);
      if (params.before) qs.set("before", params.before);
      if (params.limit !== undefined) qs.set("limit", String(params.limit));
      const query = qs.toString();
      return request<ActivityPage>(`/api/projects/${encodeURIComponent(id)}/activity${query ? `?${query}` : ""}`);
    },
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
      return request<RawTicket[]>(`/api/tickets${qs ? `?${qs}` : ""}`).then((raw) => raw.map(normalizeTicket));
    },
    get: (id: string) => request<RawTicket>(`/api/tickets/${id}`).then(normalizeTicket),
    create: (data: TicketWrite) =>
      request<RawTicket>("/api/tickets", {
        method: "POST",
        body: JSON.stringify(data),
      }).then(normalizeTicket),
    update: (id: string, data: TicketWrite) =>
      request<RawTicket>(`/api/tickets/${id}`, {
        method: "PUT",
        body: JSON.stringify(data),
      }).then(normalizeTicket),
    delete: (id: string) =>
      request<void>(`/api/tickets/${id}`, { method: "DELETE" }),
    history: (id: string) => request<StatusChange[]>(`/api/tickets/${id}/history`),
    move: (id: string, status: string, position?: number) =>
      request<RawTicket>(`/api/tickets/${id}/move`, {
        method: "POST",
        body: JSON.stringify({ status, position }),
      }).then(normalizeTicket),
    /** The person stopping the work an agent does on the ticket: it answers
     * with the ticket, freed and back in todo. */
    stop: (id: string) => request<RawTicket>(`/api/tickets/${id}/stop`, { method: "POST" }).then(normalizeTicket),
    addSubtask: (id: string, title: string) =>
      request<Subtask>(`/api/tickets/${id}/subtasks`, {
        method: "POST",
        body: JSON.stringify({ title }),
      }),
  },

  entries: {
    /**
     * One page of an owner's entries, newest first: current ones only unless
     * includeReplaced; types keeps only those; before is the previous page's
     * nextBefore. limit is 1 to 100 (the server's default is 20).
     */
    list: (
      owner: EntryOwnerRef,
      params: { includeReplaced?: boolean; types?: readonly string[]; before?: string; limit?: number } = {},
    ) => {
      const qs = new URLSearchParams();
      if (params.includeReplaced) qs.set("includeReplaced", "true");
      if (params.types && params.types.length > 0) qs.set("type", params.types.join(","));
      if (params.before) qs.set("before", params.before);
      if (params.limit !== undefined) qs.set("limit", String(params.limit));
      const query = qs.toString();
      return request<RawEntryPage>(`${entriesUrl(owner)}${query ? `?${query}` : ""}`).then(
        (raw): EntryPage => ({ ...raw, entries: raw.entries ?? [], agents: raw.agents ?? {} }),
      );
    },
    /** Leave a note as the person; the server names the person as its user. */
    createNote: (owner: EntryOwnerRef, note: NoteWrite) =>
      request<Entry>(entriesUrl(owner), { method: "POST", body: JSON.stringify(note) }),
  },

  requests: {
    /** A ticket's requests for user input, answered or not, newest first. */
    list: (ticketId: string) =>
      request<TicketRequest[] | null>(`/api/tickets/${encodeURIComponent(ticketId)}/requests`).then((raw) => raw ?? []),
    /** Answer as the person; the server records the local person as who answered. */
    answer: (id: string, data: RequestAnswer) =>
      request<TicketRequest>(`/api/requests/${encodeURIComponent(id)}/answer`, {
        method: "POST",
        body: JSON.stringify(data),
      }),
  },

  agents: {
    get: (id: string) => request<AgentListItem>(`/api/agents/${encodeURIComponent(id)}`),
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
      request<RawEpicList>(`/api/epics?projectId=${encodeURIComponent(projectId)}`).then(normalizeEpicList),
    create: (data: { projectId: string; name: string; description?: string }) =>
      request<RawEpic>("/api/epics", {
        method: "POST",
        body: JSON.stringify(data),
      }).then(normalizeEpic),
    update: (id: string, data: { name?: string; description?: string }) =>
      request<RawEpic>(`/api/epics/${id}`, {
        method: "PUT",
        body: JSON.stringify(data),
      }).then(normalizeEpic),
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
      request<RawBoard>(`/api/board${projectId ? `?projectId=${projectId}` : ""}`).then(normalizeBoard),
  },

  version: {
    get: () => request<BuildInfo>("/api/version"),
  },

  now: {
    /** Every project's, or one project's when projectId (an id or prefix) is given. */
    get: (projectId?: string) =>
      request<Now>(`/api/now${projectId ? `?projectId=${encodeURIComponent(projectId)}` : ""}`),
  },
};

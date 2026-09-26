import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "./client";

function stubFetch(status: number, body: unknown) {
  const fetch = vi.fn().mockResolvedValue(
    new Response(typeof body === "string" ? body : JSON.stringify(body), { status }),
  );
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

afterEach(() => vi.unstubAllGlobals());

// The Go models (internal/models) mark labels, subtasks, repos, description
// and other fields `omitempty`, so the server leaves them out of the JSON
// entirely when they're empty, rather than sending "" or []. ACP-26's label
// filter crashed on exactly this. These tests send the API's real shape (the
// fields simply missing) and check the client always turns it into a ticket,
// project and board the rest of the app can read without a fallback.
describe("wire normalisation", () => {
  it("fills in a ticket's labels, subtasks, repos, description, dependsOn and blocks when the API leaves them out", async () => {
    stubFetch(200, {
      id: "t1",
      projectId: "p1",
      number: 7,
      title: "No optional fields",
      status: "todo",
      priority: "high",
      position: 0,
      createdAt: "",
      updatedAt: "",
      // description, projectPrefix, repos, labels, subtasks, dependsOn and
      // blocks are all omitted, exactly as the API sends an empty ticket.
    });
    const ticket = await api.tickets.get("t1");
    expect(ticket.description).toBe("");
    expect(ticket.projectPrefix).toBe("");
    expect(ticket.repos).toEqual([]);
    expect(ticket.labels).toEqual([]);
    expect(ticket.subtasks).toEqual([]);
    expect(ticket.dependsOn).toEqual([]);
    expect(ticket.blocks).toEqual([]);
    // dueDate and epic carry meaning when absent (no due date, no epic), so
    // they stay undefined rather than being filled in.
    expect(ticket.dueDate).toBeUndefined();
    expect(ticket.epic).toBeUndefined();
  });

  it("normalises every ticket in a list response the same way", async () => {
    stubFetch(200, [{ id: "t1", projectId: "p1", number: 1, title: "A", status: "todo", priority: "low", position: 0, createdAt: "", updatedAt: "" }]);
    const [ticket] = await api.tickets.list();
    expect(ticket.labels).toEqual([]);
    expect(ticket.repos).toEqual([]);
  });

  it("fills in a project's description, icon and color when the API leaves them out", async () => {
    stubFetch(200, { id: "p1", name: "Auth", prefix: "AUTH", status: "active", createdAt: "", updatedAt: "" });
    const project = await api.projects.get("p1");
    expect(project.description).toBe("");
    expect(project.icon).toBe("");
    expect(project.color).toBe("");
    // agentInstructions carries "not fetched" vs "fetched, none set" meaning,
    // so it is left as the API sent it: absent here.
    expect(project.agentInstructions).toBeUndefined();
  });

  it("fills in an epic's description, including within an epics list response", async () => {
    stubFetch(200, {
      epics: [{ id: "e1", projectId: "p1", name: "Graph", createdAt: "", updatedAt: "", counts: {}, total: 0, complete: false, lastActivityAt: null }],
      noEpic: { counts: {}, total: 0, complete: false, lastActivityAt: null },
    });
    const { epics } = await api.epics.list("p1");
    expect(epics[0].description).toBe("");
  });

  it("normalises the tickets nested in a board response", async () => {
    stubFetch(200, {
      columns: [{ status: "todo", tickets: [{ id: "t1", projectId: "p1", number: 1, title: "A", status: "todo", priority: "low", position: 0, createdAt: "", updatedAt: "" }] }],
    });
    const board = await api.board.get("p1");
    expect(board.projectId).toBe("");
    expect(board.columns[0].tickets[0].labels).toEqual([]);
    expect(board.columns[0].tickets[0].repos).toEqual([]);
  });
});

describe("api.documents", () => {
  it("creates a document with a POST carrying the owner, name, format and content", async () => {
    const fetch = stubFetch(201, { id: "d9" });
    const created = await api.documents.create({ ticketId: "t1", name: "Notes", format: "markdown", content: "" });
    expect(created).toEqual({ id: "d9" });
    const [url, init] = fetch.mock.calls[0];
    expect(url).toBe("/api/documents");
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body)).toEqual({ ticketId: "t1", name: "Notes", format: "markdown", content: "" });
  });

  it("creates an epic's document with its epicId", async () => {
    const fetch = stubFetch(201, { id: "d9" });
    await api.documents.create({ epicId: "e1", name: "Rollout", format: "html", content: "<p>x</p>" });
    expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual({ epicId: "e1", name: "Rollout", format: "html", content: "<p>x</p>" });
  });

  it("uploads an image file as it is, named by its filename, to a ticket or an epic", async () => {
    const fetch = vi.fn().mockImplementation(async () => new Response(JSON.stringify({ id: "d9", format: "png" }), { status: 201 }));
    vi.stubGlobal("fetch", fetch);
    const file = new File([new Uint8Array([137, 80, 78, 71])], "Login screen.png", { type: "image/png" });
    expect(await api.documents.createImage({ ticketId: "t1" }, file)).toEqual({ id: "d9", format: "png" });
    const [url, init] = fetch.mock.calls[0];
    expect(url).toBe("/api/documents/images?ticket=t1&filename=Login+screen.png");
    expect(init.method).toBe("POST");
    expect(init.body).toBe(file);
    expect(init.headers["Content-Type"]).toBe("image/png");

    await api.documents.createImage({ epicId: "e1" }, new File(["x"], "a.webp"));
    const [epicUrl, epicInit] = fetch.mock.calls[1];
    expect(epicUrl).toBe("/api/documents/images?epic=e1&filename=a.webp");
    expect(epicInit.headers["Content-Type"]).toBe("application/octet-stream");
  });

  it("points an image and its thumbnail at the revision shown", () => {
    expect(api.documents.imageUrl("d1", 4)).toBe("/api/documents/d1/image?rev=4");
    expect(api.documents.thumbnailUrl("d1", 4)).toBe("/api/documents/d1/thumbnail?rev=4");
  });

  it("lists a ticket's documents or an epic's", async () => {
    const fetch = vi.fn().mockImplementation(async () => new Response("[]", { status: 200 }));
    vi.stubGlobal("fetch", fetch);
    await api.documents.list({ ticketId: "t1" });
    await api.documents.list({ epicId: "e1" });
    expect(fetch.mock.calls.map((c) => c[0])).toEqual(["/api/tickets/t1/documents", "/api/epics/e1/documents"]);
  });

  it("saves content with the revision it started from", async () => {
    const fetch = stubFetch(200, { id: "d1", revision: 3 });
    await api.documents.update("d1", { content: "mine", expectedRevision: 2 });
    const [url, init] = fetch.mock.calls[0];
    expect(url).toBe("/api/documents/d1");
    expect(init.method).toBe("PUT");
    expect(JSON.parse(init.body)).toEqual({ content: "mine", expectedRevision: 2 });
  });

  it("searches a project's ticket documents with the text encoded", async () => {
    const fetch = stubFetch(200, { ticketIds: ["t1"] });
    const found = await api.documents.search("R&D plan.md", "ACP");
    expect(found).toEqual({ ticketIds: ["t1"] });
    expect(fetch.mock.calls[0][0]).toBe("/api/documents/search?q=R%26D+plan.md&projectId=ACP");
  });

  it("rejects a stale save with the 409 body, for conflictDocument to read", async () => {
    stubFetch(409, { error: "changed", current: { id: "d1", revision: 2, content: "theirs" } });
    await expect(api.documents.update("d1", { content: "mine", expectedRevision: 1 })).rejects.toThrow(
      /^API error 409: .*"current"/,
    );
  });
});

describe("the project journal calls", () => {
  it("read a page, with before and limit only when given", async () => {
    let fetch = stubFetch(200, { entries: [], total: 0, hasMore: false });
    await api.projects.journal("p1");
    expect(fetch.mock.calls[0][0]).toBe("/api/projects/p1/journal");
    fetch = stubFetch(200, { entries: [], total: 0, hasMore: false });
    await api.projects.journal("p1", { before: "e9", limit: 5 });
    expect(fetch.mock.calls[0][0]).toBe("/api/projects/p1/journal?before=e9&limit=5");
  });

  it("append an entry with a POST of its author and text", async () => {
    const fetch = stubFetch(201, { id: "e1", projectId: "p1", author: "Bilal", text: "Hi", createdAt: "" });
    const entry = await api.projects.appendJournal("p1", { author: "Bilal", text: "Hi" });
    expect(entry.id).toBe("e1");
    const [url, init] = fetch.mock.calls[0];
    expect(url).toBe("/api/projects/p1/journal");
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body)).toEqual({ author: "Bilal", text: "Hi" });
  });
});

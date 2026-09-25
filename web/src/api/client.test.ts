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

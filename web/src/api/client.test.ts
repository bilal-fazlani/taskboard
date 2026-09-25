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

  it("saves content with the revision it started from", async () => {
    const fetch = stubFetch(200, { id: "d1", revision: 3 });
    await api.documents.update("d1", { content: "mine", expectedRevision: 2 });
    const [url, init] = fetch.mock.calls[0];
    expect(url).toBe("/api/documents/d1");
    expect(init.method).toBe("PUT");
    expect(JSON.parse(init.body)).toEqual({ content: "mine", expectedRevision: 2 });
  });

  it("rejects a stale save with the 409 body, for conflictDocument to read", async () => {
    stubFetch(409, { error: "changed", current: { id: "d1", revision: 2, content: "theirs" } });
    await expect(api.documents.update("d1", { content: "mine", expectedRevision: 1 })).rejects.toThrow(
      /^API error 409: .*"current"/,
    );
  });
});

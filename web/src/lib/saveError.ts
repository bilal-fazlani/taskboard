// What to tell the user when a save did not go through.
//
// api/client.ts throws `API error <status>: <body>`, where the body is the
// API's `{"error": "…"}` for anything it refused on purpose, and a fetch that
// never reached the server throws a TypeError of the browser's own wording,
// which says nothing a user can act on. Both become one short line, shown
// beside the edits the failed save left untouched.

/** The API client's message shape, e.g. `API error 404: {"error":"…"}`. */
const API_ERROR = /^API error (\d{3}): ?([\s\S]*)$/;

/** What a 404 on a save means: the ticket went while the editor was open. */
export const GONE_MESSAGE = "This ticket no longer exists, so the save went nowhere. Your edits are still here.";

const FALLBACK = "The save did not go through. Your edits are still here.";

/** The `error` field of an API error body, or the body as it stands. */
function bodyMessage(body: string): string {
  const text = body.trim();
  if (text === "") return "";
  try {
    const parsed: unknown = JSON.parse(text);
    if (parsed && typeof parsed === "object" && typeof (parsed as { error?: unknown }).error === "string") {
      return (parsed as { error: string }).error;
    }
    // JSON, but not the API's shape: nothing in it is worth showing.
    return "";
  } catch {
    // Not JSON: whatever the server said is better than nothing.
    return text;
  }
}

/** One line about a save that failed, always ending in what is still true: the edits are kept. */
export function saveErrorMessage(error: unknown): string {
  const raw = error instanceof Error ? error.message : typeof error === "string" ? error : "";
  const api = API_ERROR.exec(raw);
  if (!api) return FALLBACK;
  if (api[1] === "404") return GONE_MESSAGE;
  const message = bodyMessage(api[2]).replace(/\.$/, "");
  return message === "" ? FALLBACK : `${message}. Your edits are still here.`;
}

// What to tell the user when an API call the editor made on their behalf did
// not go through.
//
// api/client.ts throws `API error <status>: <body>`, where the body is the
// API's `{"error": "…"}` for anything it refused on purpose, and a fetch that
// never reached the server throws a TypeError of the browser's own wording,
// which says nothing a user can act on. Both become one short line. The three
// functions below shape that line differently for a save (the edits are kept
// and worth mentioning), a delete (nothing is kept — the ticket either went or
// it didn't), and an in-place action like a subtask toggle (nothing to keep
// either, and no ticket-wide "gone" case worth calling out specially).

/** The API client's message shape, e.g. `API error 404: {"error":"…"}`. */
const API_ERROR = /^API error (\d{3}): ?([\s\S]*)$/;

/** What a 404 on a save means: the ticket went while the editor was open. */
export const GONE_MESSAGE = "This ticket no longer exists, so the save went nowhere. Your edits are still here.";

const FALLBACK = "The save did not go through. Your edits are still here.";

/** What a 404 on a delete means: someone else already deleted this ticket. */
export const DELETE_GONE_MESSAGE = "This ticket was already deleted elsewhere.";

const DELETE_FALLBACK = "The delete did not go through.";

const ACTION_FALLBACK = "That didn't go through.";

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

/** The status and body of an `API error …` message, or null for anything else
 * (a network failure, or no error at all). */
function apiError(error: unknown): { status: string; body: string } | null {
  const raw = error instanceof Error ? error.message : typeof error === "string" ? error : "";
  const api = API_ERROR.exec(raw);
  return api ? { status: api[1], body: api[2] } : null;
}

/** One line about a save that failed, always ending in what is still true: the edits are kept. */
export function saveErrorMessage(error: unknown): string {
  const api = apiError(error);
  if (!api) return FALLBACK;
  if (api.status === "404") return GONE_MESSAGE;
  const message = bodyMessage(api.body).replace(/\.$/, "");
  return message === "" ? FALLBACK : `${message}. Your edits are still here.`;
}

/** One line about a delete that failed: what the API refused it for, or a
 * generic fallback. Unlike a save there are no edits to reassure the user
 * about, so the message carries no "your edits are still here" tail, and a
 * 404 reads as informational — someone else got to it first — rather than as
 * a refusal. */
export function deleteErrorMessage(error: unknown): string {
  const api = apiError(error);
  if (!api) return DELETE_FALLBACK;
  if (api.status === "404") return DELETE_GONE_MESSAGE;
  const message = bodyMessage(api.body).replace(/\.$/, "");
  return message === "" ? DELETE_FALLBACK : `${message}.`;
}

/** One line about some other in-place ticket action — a subtask added,
 * toggled or deleted — that failed. Shaped the same way, but without a
 * ticket-wide "gone" case: a subtask 404 already says so in its own body. */
export function actionErrorMessage(error: unknown): string {
  const api = apiError(error);
  if (!api) return ACTION_FALLBACK;
  const message = bodyMessage(api.body).replace(/\.$/, "");
  return message === "" ? ACTION_FALLBACK : `${message}.`;
}

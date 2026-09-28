// The ticket page's requests for user input: the open one first, with the
// form its type calls for, then the earlier ones in a fold, newest first.
// A reading block of its own, never part of the ticket's editing forms, so it
// stays when those forms retire: answering is one step, and sends at once.
import { useCallback, useEffect, useId, useRef, useState } from "react";
import Markdown from "react-markdown";
import { X } from "lucide-react";
import { api, type Agent, type Ticket, type TicketRequest } from "../api/client";
import { useLiveRefresh } from "../hooks/useLiveRefresh";
import { activityTime } from "../lib/activity";
import {
  answerToast,
  answerTone,
  askedAgo,
  collectorPhrase,
  earlierRequests,
  questionAnswer,
  requestKindLabel,
} from "../lib/requests";
import { actionErrorMessage } from "../lib/saveError";
import { VendorMark } from "./EntryCard";
import { EntryFold } from "./EntryParts";

type AgentInfo = Pick<Agent, "id" | "role" | "model" | "provider">;

/** How long the toast after an answer stays, unless dismissed. */
export const TOAST_MS = 8000;

/** Sends an answer and its note; rejects with the server's reason. */
type Answer = (answer: string, note: string) => Promise<void>;

const KIND_TAG = "rounded px-1.5 py-px text-[10.5px] font-bold uppercase tracking-[0.07em]";
const TEXTAREA =
  "min-h-[54px] w-full resize-y rounded-lg border border-slate-700 bg-slate-800 px-2.5 py-2 text-[13px] text-slate-200 placeholder-slate-500 focus:outline-none focus:ring-1 focus:ring-blue-500";
const BUTTON =
  "rounded-lg px-3.5 py-1.5 text-[13px] font-semibold transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-400 focus-visible:ring-offset-2 focus-visible:ring-offset-slate-900 disabled:cursor-not-allowed disabled:opacity-50";

export default function RequestBlock({
  ticket,
  readOnly = false,
  onAnswered,
}: {
  /** The full ticket: its openRequest and the agent holding it. */
  ticket: Ticket;
  /** A deleted ticket: shown, but nothing can be answered. */
  readOnly?: boolean;
  /** Called once an answer has gone through, so the page can reload the ticket. */
  onAnswered?: () => void;
}) {
  // The ticket's whole history, kept with the ticket it was read for.
  const [history, setHistory] = useState<{ ticketId: string; list: TicketRequest[] } | null>(null);
  // Requests answered from this page, as the answer came back: the open
  // request goes the moment its answer lands, not when the ticket reloads.
  const [answered, setAnswered] = useState<Record<string, TicketRequest>>({});
  const [toast, setToast] = useState<{ seq: number; text: string } | null>(null);
  const seq = useRef(0);

  const reload = useCallback(() => {
    const n = ++seq.current;
    const ticketId = ticket.id;
    // Through a promise, so a test's API mock without `requests` fails the
    // load rather than the render.
    Promise.resolve()
      .then(() => api.requests.list(ticketId))
      .then((list) => {
        if (n === seq.current && Array.isArray(list)) setHistory({ ticketId, list });
      })
      .catch(() => {
        // A failed load keeps what is on screen.
      });
  }, [ticket.id]);

  // Read again when the ticket moves (a new request, an answer from the
  // CLI) and on every live change.
  const openId = ticket.openRequest?.id;
  useEffect(() => {
    reload();
  }, [reload, ticket.updatedAt, openId]);
  useLiveRefresh(reload);

  const open = ticket.openRequest && !answered[ticket.openRequest.id] ? ticket.openRequest : undefined;
  const asker = useRequestAgent(open?.agentId, ticket.agent);

  const all = (history?.ticketId === ticket.id ? history.list : []).map((r) => answered[r.id] ?? r);
  for (const r of Object.values(answered)) {
    if (r.ticketId === ticket.id && !all.some((x) => x.id === r.id)) all.push(r);
  }
  const earlier = earlierRequests(all, open?.id);

  const answer = (request: TicketRequest, collector: AgentInfo | undefined) => async (text: string, note: string) => {
    const done = await api.requests.answer(request.id, { answer: text, ...(note.trim() ? { note: note.trim() } : {}) });
    setAnswered((prev) => ({ ...prev, [request.id]: done }));
    const toastText = answerToast(request.type, done.answer ?? text, collector);
    setToast((prev) => ({ seq: (prev?.seq ?? 0) + 1, text: toastText }));
    onAnswered?.();
    reload();
  };

  if (!open && earlier.length === 0 && !toast) return null;

  return (
    <div data-testid="ticket-requests" className="space-y-3">
      {toast && <AnswerToast key={toast.seq} text={toast.text} onDismiss={() => setToast(null)} />}
      {open && (
        <div inert={readOnly} className={readOnly ? "opacity-60" : undefined}>
          <OpenRequest key={open.id} request={open} agent={asker} onAnswer={answer(open, asker)} />
        </div>
      )}
      {earlier.length > 0 && (
        <EntryFold label={`Earlier requests (${earlier.length})`}>
          <ul className="space-y-1.5" aria-label="Earlier requests">
            {earlier.map((r) => (
              <EarlierRequest key={r.id} request={r} />
            ))}
          </ul>
        </EntryFold>
      )}
    </div>
  );
}

/**
 * The agent that asked, which is the one that collects the answer: usually
 * the ticket's holder, but an orchestrator may ask on its implementer's
 * ticket, so any other agent is read by its id.
 */
function useRequestAgent(agentId: string | undefined, holder: Agent | undefined): AgentInfo | undefined {
  const [fetched, setFetched] = useState<AgentInfo | null>(null);
  const other = agentId && holder?.id !== agentId ? agentId : null;
  useEffect(() => {
    if (!other) return;
    let cancelled = false;
    Promise.resolve()
      .then(() => api.agents.get(other))
      .then((a) => {
        if (!cancelled && a) setFetched(a);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [other]);
  if (!agentId) return undefined;
  if (holder?.id === agentId) return holder;
  return fetched?.id === agentId ? fetched : undefined;
}

/** "Asked 3m ago", kept current while the request waits. */
function useNow(): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 30_000);
    return () => clearInterval(timer);
  }, []);
  return now;
}

function OpenRequest({
  request,
  agent,
  onAnswer,
}: {
  request: TicketRequest;
  agent: AgentInfo | undefined;
  onAnswer: Answer;
}) {
  const now = useNow();
  const kind = requestKindLabel(request.type);
  const who = agent ? `the ${agent.role}` : "an agent";
  return (
    <section
      aria-label={request.type === "approval" ? `Approval asked by ${who}` : `${kind} from ${who}`}
      data-testid="open-request"
      data-type={request.type}
      className="flex flex-col gap-3 rounded-[10px] border border-red-500/50 bg-red-500/[0.05] px-4 py-3.5"
    >
      <div className="flex flex-wrap items-center gap-1.5 text-xs text-slate-400">
        <span className={`${KIND_TAG} bg-red-500/15 text-red-400`}>{kind}</span>
        {agent ? (
          <>
            <VendorMark provider={agent.provider} />
            <span>
              {agent.role}
              {agent.model && ` · ${agent.model}`}
            </span>
          </>
        ) : (
          <span>an agent</span>
        )}
        <span title={new Date(request.createdAt).toLocaleString()}>· asked {askedAgo(request.createdAt, now)}</span>
      </div>
      <div data-testid="request-prompt" className="prose-card" style={{ color: "#f1f5f9", fontSize: "14px" }}>
        <Markdown>{request.prompt}</Markdown>
      </div>
      {request.type === "question" ? (
        <QuestionForm request={request} agent={agent} onAnswer={onAnswer} />
      ) : request.type === "approval" ? (
        <ApprovalForm onAnswer={onAnswer} />
      ) : (
        <p className="text-xs text-slate-500">
          The board can&apos;t answer a &ldquo;{kind}&rdquo; request yet.
        </p>
      )}
    </section>
  );
}

/**
 * A question: its choices, then the person's own words, which win over a
 * picked choice. One of the two is needed before it can be sent.
 */
function QuestionForm({
  request,
  agent,
  onAnswer,
}: {
  request: TicketRequest;
  agent: AgentInfo | undefined;
  onAnswer: Answer;
}) {
  const name = useId();
  const [choice, setChoice] = useState("");
  const [text, setText] = useState("");
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const answer = questionAnswer(choice, text);
  const typed = text.trim() !== "";
  const hasChoices = request.choices.length > 0;

  const send = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!answer || sending) return;
    setSending(true);
    setError(null);
    try {
      await onAnswer(answer, "");
    } catch (err) {
      setError(actionErrorMessage(err));
      setSending(false);
    }
  };

  const hint = !answer
    ? hasChoices
      ? "Pick a choice or write your own answer."
      : "Write your answer."
    : typed && choice
      ? "Your words replace the choice."
      : `${collectorPhrase(agent)} collects it on its next call.`;

  return (
    <form onSubmit={send} className="flex flex-col gap-3">
      {hasChoices && (
        <>
          <div
            role="radiogroup"
            aria-label="Choices"
            className={`flex flex-col gap-1.5 transition-opacity ${typed ? "opacity-50" : ""}`}
          >
            {request.choices.map((c) => (
              <label
                key={c}
                className={`flex cursor-pointer items-center gap-2.5 rounded-lg border px-2.5 py-1.5 text-sm transition-colors focus-within:ring-2 focus-within:ring-blue-400 ${
                  choice === c
                    ? "border-red-500/50 bg-red-500/10 text-slate-100"
                    : "border-slate-700 text-slate-300 hover:border-slate-600"
                }`}
              >
                <input
                  type="radio"
                  name={name}
                  value={c}
                  checked={choice === c}
                  onChange={() => setChoice(c)}
                  className="accent-red-500"
                />
                {c}
              </label>
            ))}
          </div>
          <div className="text-xs text-slate-500">or answer in your own words</div>
        </>
      )}
      <textarea
        aria-label={hasChoices ? "Answer in your own words" : "Your answer"}
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder={hasChoices ? "Your answer. It replaces the choice above if you write one." : "Your answer"}
        className={TEXTAREA}
      />
      {error && (
        <p role="alert" className="text-xs text-red-300">
          {error}
        </p>
      )}
      <div className="flex flex-wrap items-center gap-2">
        <button type="submit" disabled={!answer || sending} className={`${BUTTON} bg-blue-600 text-white hover:bg-blue-500`}>
          Send answer
        </button>
        <span className="text-xs text-slate-500">{hint}</span>
      </div>
    </form>
  );
}

/** An approval: Approve or Decline, with one optional note that goes with either. */
function ApprovalForm({ onAnswer }: { onAnswer: Answer }) {
  const [note, setNote] = useState("");
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const send = async (answer: "approved" | "declined") => {
    if (sending) return;
    setSending(true);
    setError(null);
    try {
      await onAnswer(answer, note);
    } catch (err) {
      setError(actionErrorMessage(err));
      setSending(false);
    }
  };

  return (
    <div className="flex flex-col gap-3">
      <textarea
        aria-label="Note to the agent (optional)"
        value={note}
        onChange={(e) => setNote(e.target.value)}
        placeholder="Optional note to the agent"
        className={TEXTAREA}
      />
      {error && (
        <p role="alert" className="text-xs text-red-300">
          {error}
        </p>
      )}
      <div className="flex flex-wrap items-center gap-2">
        <button
          type="button"
          disabled={sending}
          onClick={() => send("approved")}
          className={`${BUTTON} bg-green-700 text-white hover:bg-green-600`}
        >
          Approve
        </button>
        <button
          type="button"
          disabled={sending}
          onClick={() => send("declined")}
          className={`${BUTTON} border border-slate-700 text-slate-200 hover:bg-slate-800`}
        >
          Decline
        </button>
      </div>
    </div>
  );
}

const TONE_STYLES = {
  approved: "text-green-400",
  declined: "text-red-400",
  answer: "text-slate-100",
  waiting: "text-slate-500",
} as const;

/** One earlier request: its type, prompt, answer and note, who answered, and when. */
function EarlierRequest({ request }: { request: TicketRequest }) {
  const tone = answerTone(request);
  const asked = activityTime(request.createdAt);
  const answeredAt = request.answeredAt ? activityTime(request.answeredAt) : "";
  return (
    <li
      data-testid="earlier-request"
      className="grid grid-cols-[auto_minmax(0,1fr)_auto] items-baseline gap-2 rounded-md bg-slate-800/50 px-2.5 py-1.5 text-[12.5px]"
    >
      <span className={`${KIND_TAG} text-slate-500`}>{requestKindLabel(request.type)}</span>
      <div className="min-w-0 space-y-0.5">
        <p className="line-clamp-3 whitespace-pre-wrap text-slate-300" title={request.prompt}>
          {request.prompt}
        </p>
        <p className="text-slate-400">
          {tone === "waiting" ? (
            <span className={TONE_STYLES.waiting}>Not answered</span>
          ) : (
            <>
              {request.type === "approval" ? (
                <span className={`font-medium ${TONE_STYLES[tone]}`}>{tone === "declined" ? "Declined" : "Approved"}</span>
              ) : (
                <>
                  Answer: <span className={TONE_STYLES.answer}>{request.answer}</span>
                </>
              )}
              {request.answeredBy && ` by ${request.answeredBy}`}
              {answeredAt && (
                <span className="text-slate-500" title={new Date(request.answeredAt!).toLocaleString()}>
                  {" "}
                  · {answeredAt}
                </span>
              )}
            </>
          )}
        </p>
        {request.note && (
          <p data-testid="request-note" className="whitespace-pre-wrap text-slate-400">
            Note: {request.note}
          </p>
        )}
      </div>
      <span className="whitespace-nowrap text-slate-500" title={new Date(request.createdAt).toLocaleString()}>
        asked {asked}
      </span>
    </li>
  );
}

/** Says what an answer did and which agent collects it; goes by itself after TOAST_MS. */
function AnswerToast({ text, onDismiss }: { text: string; onDismiss: () => void }) {
  const [shown, setShown] = useState(false);
  const dismissRef = useRef(onDismiss);
  useEffect(() => {
    dismissRef.current = onDismiss;
  });
  useEffect(() => {
    const frame = requestAnimationFrame(() => setShown(true));
    const timer = setTimeout(() => dismissRef.current(), TOAST_MS);
    return () => {
      cancelAnimationFrame(frame);
      clearTimeout(timer);
    };
  }, []);
  // "Answer sent." or "Approved." leads, in bold; "Declined." isn't green.
  const cut = text.indexOf(". ") + 1;
  const lead = text.slice(0, cut);
  const rest = text.slice(cut);
  return (
    <div
      role="status"
      data-testid="answer-toast"
      className={`flex w-fit max-w-full items-start gap-2 rounded-lg border border-slate-700 bg-slate-800 px-3 py-1.5 text-[12.5px] text-slate-100 shadow-lg transition-all duration-300 ease-out motion-reduce:transition-none ${
        shown ? "translate-y-0 opacity-100" : "-translate-y-1 opacity-0"
      }`}
    >
      <span>
        <b className={`font-semibold ${lead === "Declined." ? "text-slate-100" : "text-green-400"}`}>{lead}</b>
        {rest}
      </span>
      <button
        type="button"
        aria-label="Dismiss"
        onClick={onDismiss}
        className="mt-0.5 shrink-0 text-slate-500 transition-colors hover:text-slate-300"
      >
        <X className="h-3 w-3" />
      </button>
    </div>
  );
}

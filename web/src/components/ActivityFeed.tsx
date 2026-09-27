import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { History } from "lucide-react";
import { api, type ActivityPage, type ProjectActivityEntry } from "../api/client";
import { useChangeGlow } from "../hooks/useChangeGlow";
import { useLiveRefresh } from "../hooks/useLiveRefresh";
import {
  FEED_FRESH_CLASS,
  FEED_PAGE_SIZE,
  feedClock,
  groupByDay,
  noteParts,
  readNewest,
  sinceBaseline,
  type FeedBaseline,
  type FetchActivityPage,
} from "../lib/activityFeed";
import { ActivityNote, StatusChangeEntry } from "./ActivityList";

function Note({ note }: { note: string }) {
  return (
    <ActivityNote>
      {noteParts(note).map((part, i) =>
        part.sha ? (
          <span key={i} className="rounded bg-slate-800 px-1.5 py-px font-mono text-[11px] text-slate-300">
            {part.text}
          </span>
        ) : (
          part.text
        ),
      )}
    </ActivityNote>
  );
}

function Entry({
  entry,
  fresh,
  onOpen,
}: {
  entry: ProjectActivityEntry;
  fresh: boolean;
  onOpen: (entry: ProjectActivityEntry) => void;
}) {
  return (
    <li>
      <button
        type="button"
        data-testid="feed-entry"
        onClick={() => onOpen(entry)}
        className={`grid w-full grid-cols-[54px_1fr] gap-3 rounded-md px-2.5 py-2 text-left transition-colors hover:bg-slate-800/50 ${
          fresh ? FEED_FRESH_CLASS : ""
        }`}
      >
        <time
          dateTime={entry.createdAt}
          title={new Date(entry.createdAt).toLocaleString()}
          className="pt-0.5 text-[11px] tabular-nums text-slate-500"
        >
          {feedClock(entry.createdAt)}
        </time>
        <span className="min-w-0">
          <span className="flex flex-wrap items-center gap-2">
            <span className="font-mono text-xs text-slate-500">{entry.ticketKey}</span>
            <span className="min-w-0 text-sm font-medium text-slate-200">{entry.ticketTitle}</span>
            <StatusChangeEntry change={entry} />
            <span className="text-[11px] text-slate-500">{entry.epic?.name ?? "No epic"}</span>
          </span>
          {entry.note && <Note note={entry.note} />}
        </span>
      </button>
    </li>
  );
}

/**
 * One project's activity, newest first under day headings, as the Activity
 * page shows it. It reads its own pages, from `project` and `epics` as they
 * were when it mounted: the page gives it a key made of both, so a change to
 * either starts a fresh feed from the newest page rather than mixing the two.
 *
 * A live change reads the feed again down to the oldest entry shown (see
 * readNewest), so pages loaded with "Show older" stay. Entries it brings in glow for a moment,
 * as changed cards do on the board (useChangeGlow); the first load and older
 * pages never do.
 */
export default function ActivityFeed({
  project,
  epics,
  onOpen,
  now,
}: {
  project: string;
  epics: readonly string[];
  onOpen: (entry: ProjectActivityEntry) => void;
  /** Today, for the day headings; tests pin it. */
  now?: Date;
}) {
  const [page, setPage] = useState<ActivityPage | null>(null);
  const [failed, setFailed] = useState(false);
  const [loadingOlder, setLoadingOlder] = useState(false);
  // Whether the last "Show older" failed; cleared when it is tried again.
  const [olderFailed, setOlderFailed] = useState(false);
  // The newest entry of the first load, undefined until it lands.
  const [baseline, setBaseline] = useState<FeedBaseline | undefined>(undefined);

  // What the feed reads, fixed for its life (see the doc comment), and what
  // it shows, for the async paths below to check against.
  const [fetchPage] = useState<FetchActivityPage>(
    () => (before: string | undefined, limit: number) => api.projects.activity(project, { epics, before, limit }),
  );
  const shown = useRef<ActivityPage | null>(null);
  const refreshSeq = useRef(0);
  const show = useCallback((next: ActivityPage) => {
    shown.current = next;
    setPage(next);
  }, []);

  const refresh = useCallback(() => {
    const run = async () => {
      const mine = ++refreshSeq.current;
      const current = shown.current?.entries ?? [];
      const want = current.length;
      try {
        const next = await readNewest(fetchPage, want, current[want - 1]);
        if (mine !== refreshSeq.current) return;
        // An older page landed while this read was out: read again, so it stays.
        if ((shown.current?.entries.length ?? 0) > want && next.hasMore) {
          void run();
          return;
        }
        setBaseline((b) => (b !== undefined ? b : (next.entries[0] ?? null)));
        show(next);
        setFailed(false);
      } catch {
        // A failed read keeps what is on screen; the next change tries again.
        if (mine === refreshSeq.current) setFailed(true);
      }
    };
    void run();
  }, [fetchPage, show]);

  useEffect(() => refresh(), [refresh]);
  useLiveRefresh(refresh);

  const showOlder = async () => {
    const before = shown.current?.nextBefore;
    if (!before || loadingOlder) return;
    setLoadingOlder(true);
    setOlderFailed(false);
    try {
      const older = await fetchPage(before, FEED_PAGE_SIZE);
      const current = shown.current;
      // Dropped when a refresh moved the end of the feed meanwhile; the
      // button reads on from the new end.
      if (current && current.nextBefore === before) {
        const have = new Set(current.entries.map((e) => e.id));
        show({
          entries: [...current.entries, ...older.entries.filter((e) => !have.has(e.id))],
          hasMore: older.hasMore,
          nextBefore: older.nextBefore,
        });
      }
    } catch {
      // Said under the entries, with the button offering another try.
      setOlderFailed(true);
    } finally {
      setLoadingOlder(false);
    }
  };

  const entries = page?.entries;
  const glowInput = useMemo(
    () => (entries && baseline !== undefined ? sinceBaseline(entries, baseline) : null),
    [entries, baseline],
  );
  const glowing = useChangeGlow(glowInput);
  const days = useMemo(() => groupByDay(entries ?? [], now), [entries, now]);

  if (!page) {
    return failed ? (
      <div className="flex flex-col items-center justify-center gap-1 h-64 text-center">
        <p role="alert" className="text-sm text-red-400">
          Couldn't load the activity
        </p>
        <p className="text-xs text-slate-600">The next refresh will try again.</p>
      </div>
    ) : (
      <div className="flex items-center justify-center h-64 text-slate-600">Loading activity…</div>
    );
  }

  if (page.entries.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center h-64 text-slate-600 space-y-3">
        <History className="w-10 h-10 text-slate-700" />
        <p className="text-sm">{epics.length > 0 ? "No activity in these epics" : "No activity yet"}</p>
      </div>
    );
  }

  return (
    <div className="px-4 pt-2 pb-4">
      {days.map((day) => (
        <section key={day.label} aria-label={day.label}>
          <h2 className="px-2.5 pt-3.5 pb-1.5 text-[11px] font-medium uppercase tracking-wider text-slate-600">
            {day.label}
          </h2>
          <ol>
            {day.entries.map((entry) => (
              <Entry key={entry.id} entry={entry} fresh={glowing.has(entry.id)} onOpen={onOpen} />
            ))}
          </ol>
        </section>
      ))}
      {page.hasMore && (
        <div className="mt-3 flex flex-col items-center gap-2">
          {olderFailed && !loadingOlder && (
            <p role="alert" className="text-xs text-red-400">
              Couldn't load older activity
            </p>
          )}
          <button
            type="button"
            onClick={showOlder}
            disabled={loadingOlder}
            className="rounded-md border border-slate-700 bg-slate-900 px-3.5 py-1.5 text-xs text-slate-400 transition-colors hover:text-slate-200 disabled:opacity-50"
          >
            {loadingOlder ? "Loading…" : olderFailed ? "Try again" : "Show older"}
          </button>
        </div>
      )}
    </div>
  );
}

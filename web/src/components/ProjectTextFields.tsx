import { useId, useRef, useState, type KeyboardEvent } from "react";
import { Bot, FileText } from "lucide-react";

// A project's two texts in one place on the project form: Description / Agent
// instructions tabs over a single textarea. The tabs follow the ARIA tablist
// pattern (arrow keys, Home and End move between them and select), a tab whose
// field has text shows a small blue dot while the other tab is selected, and
// the help text under the textarea follows the selected tab. Whitespace alone
// is no text: the store trims agent instructions.

export type ProjectTextTab = "description" | "instructions";

const TABS: ProjectTextTab[] = ["description", "instructions"];

export const DESCRIPTION_HELP =
  "What the project is: its goals, scope and context. Shown as the project's summary.";
export const INSTRUCTIONS_HELP =
  "How agents should work on this project's tickets. The board never acts on it.";

const TAB_CLASS =
  "inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs transition-colors focus:outline-none focus-visible:ring-1 focus-visible:ring-blue-500";
const TEXTAREA_CLASS =
  "w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white placeholder-slate-600 focus:outline-none focus:ring-1 focus:ring-blue-500 resize-y font-mono read-only:opacity-60";

export default function ProjectTextFields({
  description,
  onDescriptionChange,
  instructions,
  onInstructionsChange,
  instructionsHaveText,
  instructionsState,
}: {
  description: string;
  onDescriptionChange: (value: string) => void;
  instructions: string;
  onInstructionsChange: (value: string) => void;
  // Whether the instructions have text other than whitespace, for the tab's
  // dot. While they load, the caller answers from what the project list said.
  instructionsHaveText: boolean;
  // "loading" and "error" keep the instructions read-only: an edited project's
  // instructions are fetched when the form opens.
  instructionsState: "ready" | "loading" | "error";
}) {
  const [tab, setTab] = useState<ProjectTextTab>("description");
  const ids = useId();
  const tabRefs = useRef<Record<ProjectTextTab, HTMLButtonElement | null>>({
    description: null,
    instructions: null,
  });
  const tabId = (t: ProjectTextTab) => `${ids}-tab-${t}`;
  const panelId = `${ids}-panel`;
  const helpId = `${ids}-help`;

  const select = (t: ProjectTextTab) => {
    setTab(t);
    tabRefs.current[t]?.focus();
  };

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const i = TABS.indexOf(tab);
    let next: ProjectTextTab | null = null;
    if (e.key === "ArrowRight") next = TABS[(i + 1) % TABS.length];
    else if (e.key === "ArrowLeft") next = TABS[(i - 1 + TABS.length) % TABS.length];
    else if (e.key === "Home") next = TABS[0];
    else if (e.key === "End") next = TABS[TABS.length - 1];
    if (!next) return;
    e.preventDefault();
    select(next);
  };

  const hasText: Record<ProjectTextTab, boolean> = {
    description: description.trim() !== "",
    instructions: instructionsHaveText,
  };

  const renderTab = (t: ProjectTextTab, label: string, Icon: typeof Bot) => {
    const selected = tab === t;
    return (
      <button
        key={t}
        ref={(el) => {
          tabRefs.current[t] = el;
        }}
        type="button"
        role="tab"
        id={tabId(t)}
        aria-selected={selected}
        aria-controls={panelId}
        tabIndex={selected ? 0 : -1}
        onClick={() => setTab(t)}
        className={`${TAB_CLASS} ${
          selected ? "bg-slate-700 text-white" : "text-slate-500 hover:text-slate-300"
        }`}
      >
        <Icon aria-hidden="true" className="h-3 w-3" />
        {label}
        {!selected && hasText[t] && (
          <>
            <span
              aria-hidden="true"
              data-testid={`${t}-has-text`}
              className="ml-0.5 inline-block h-1.5 w-1.5 rounded-full bg-blue-400"
            />
            <span className="sr-only">, has text</span>
          </>
        )}
      </button>
    );
  };

  const showingInstructions = tab === "instructions";
  const instructionsReadOnly = instructionsState !== "ready";

  return (
    <div>
      <div
        role="tablist"
        aria-label="Project text"
        onKeyDown={onKeyDown}
        className="flex items-center gap-1 mb-1.5"
      >
        {renderTab("description", "Description", FileText)}
        {renderTab("instructions", "Agent instructions", Bot)}
      </div>
      <div role="tabpanel" id={panelId} aria-labelledby={tabId(tab)}>
        {showingInstructions ? (
          <textarea
            key="instructions"
            aria-labelledby={tabId("instructions")}
            aria-describedby={helpId}
            value={instructions}
            onChange={(e) => onInstructionsChange(e.target.value)}
            readOnly={instructionsReadOnly}
            placeholder={
              instructionsState === "loading"
                ? "Loading agent instructions…"
                : "Supports markdown — how agents should work on this project's tickets: branches, review, verify commands, commit style…"
            }
            rows={12}
            className={TEXTAREA_CLASS}
          />
        ) : (
          <textarea
            key="description"
            aria-labelledby={tabId("description")}
            aria-describedby={helpId}
            value={description}
            onChange={(e) => onDescriptionChange(e.target.value)}
            placeholder="Supports markdown — what the project is: goals, scope and context…"
            rows={12}
            className={TEXTAREA_CLASS}
          />
        )}
        {showingInstructions && instructionsState === "error" ? (
          <p id={helpId} role="alert" className="mt-1 text-[11px] text-red-400">
            Couldn't load the agent instructions, so they can't be edited now. Saving keeps them as they are.
          </p>
        ) : (
          <p id={helpId} className="mt-1 text-[11px] text-slate-600">
            {showingInstructions ? INSTRUCTIONS_HELP : DESCRIPTION_HELP}
          </p>
        )}
      </div>
    </div>
  );
}

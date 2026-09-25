import { Fragment, useEffect, useId, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { Check, ChevronDown } from "lucide-react";
import { fieldClass } from "./controlStyles";
import { useEscape } from "../lib/escapeStack";
import { multiSelectOptions, toggleValue, type SelectOption } from "../lib/filters";

/** Calls `run` on Escape while mounted and topmost (see lib/escapeStack). */
function OnEscape({ run }: { run: () => void }) {
  useEscape(run);
  return null;
}

/**
 * A filter bar control that takes several values: a button the size of the
 * bar's other fields (controlStyles.ts) that opens a list of checkboxes.
 *
 * Closed, it reads like a select: the "all" label with nothing chosen, the
 * value with one, and with several the values comma-joined and a count, cut
 * short at the button's width with every value in the tooltip. Open, a choice
 * applies at once and the list stays open, so the view changes behind it; the
 * footer clears just this filter. Escape closes only the list, as the topmost
 * layer, and gives the button its focus back; so do a click or focus
 * anywhere else, without taking the focus.
 *
 * The values it's handed are the URL's, as written; what it hands back are
 * option values in option order, so a choice always writes the same URL.
 */
export default function FilterMultiSelect({
  name,
  allLabel,
  values,
  options,
  ignoreCase,
  divideAfter,
  renderOption,
  onChange,
}: {
  name: string;
  /** What the control reads with nothing chosen, which sets no filter. */
  allLabel: string;
  values: readonly string[];
  options: readonly SelectOption[];
  /** Whether the filter matches ignoring case, so a URL value in another case ticks its option. */
  ignoreCase?: boolean;
  /** The option after which the list draws a divider, such as "No epic". */
  divideAfter?: string;
  /** How a row draws its option, after its checkbox; its label by default. */
  renderOption?: (option: SelectOption) => ReactNode;
  onChange: (values: string[]) => void;
}) {
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const wrapper = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  const list = useRef<HTMLDivElement>(null);
  const id = useId();

  const shown = multiSelectOptions(options, values, ignoreCase);
  const chosen = new Set(shown.chosen);
  const chosenLabels = shown.options.filter((o) => chosen.has(o.value)).map((o) => o.label);
  const set = chosenLabels.length > 0;
  const activeIndex = Math.min(active, shown.options.length - 1);

  const openList = () => {
    const first = shown.options.findIndex((o) => chosen.has(o.value));
    setActive(first < 0 ? 0 : first);
    setOpen(true);
  };
  const close = (refocus: boolean) => {
    setOpen(false);
    if (refocus) button.current?.focus();
  };
  const toggle = (value: string) => onChange(toggleValue(shown.options, shown.chosen, value));

  // The list takes the focus when it opens, and keeps the active row in view.
  useEffect(() => {
    if (open) list.current?.focus();
  }, [open]);
  useEffect(() => {
    if (open) list.current?.querySelector("[data-active]")?.scrollIntoView?.({ block: "nearest" });
  }, [open, activeIndex]);

  // A press anywhere outside closes the list.
  useEffect(() => {
    if (!open) return;
    const onPress = (e: MouseEvent) => {
      if (!wrapper.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onPress, true);
    return () => document.removeEventListener("mousedown", onPress, true);
  }, [open]);

  const onListKey = (e: KeyboardEvent) => {
    const last = shown.options.length - 1;
    const moves: Record<string, number> = {
      ArrowDown: Math.min(activeIndex + 1, last),
      ArrowUp: Math.max(activeIndex - 1, 0),
      Home: 0,
      End: last,
    };
    if (e.key in moves) {
      e.preventDefault();
      setActive(moves[e.key]);
    } else if ((e.key === " " || e.key === "Enter") && shown.options[activeIndex]) {
      e.preventDefault();
      toggle(shown.options[activeIndex].value);
    }
  };

  const rowId = (i: number) => `${id}-option-${i}`;

  return (
    <div
      ref={wrapper}
      role="group"
      aria-label={name}
      className="relative"
      onBlur={(e) => {
        if (open && !wrapper.current?.contains(e.relatedTarget as Node | null)) setOpen(false);
      }}
    >
      <button
        ref={button}
        type="button"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? `${id}-list` : undefined}
        title={set ? `${name}: ${chosenLabels.join(", ")}` : undefined}
        onClick={() => (open ? close(false) : openList())}
        onKeyDown={(e) => {
          if (!open && (e.key === "ArrowDown" || e.key === "ArrowUp")) {
            e.preventDefault();
            openList();
          }
        }}
        className={`${fieldClass(set)} inline-flex max-w-[12rem] items-center gap-1.5 ${open ? "ring-1 ring-blue-500" : ""}`}
      >
        <span className="truncate">{set ? chosenLabels.join(", ") : allLabel}</span>
        {chosenLabels.length > 1 && (
          <span className="shrink-0 rounded bg-blue-500/20 px-1 text-[10px] font-medium leading-4 text-blue-300">
            {chosenLabels.length}
          </span>
        )}
        <ChevronDown
          aria-hidden="true"
          className={`-mr-0.5 h-3.5 w-3.5 shrink-0 ${open ? "rotate-180 text-slate-200" : "text-slate-400"}`}
        />
      </button>
      {open && (
        <div
          // Presses inside keep the focus where it is, so a click on a row or
          // on Clear never blurs the list shut first.
          onMouseDown={(e) => e.preventDefault()}
          className="absolute left-0 top-full z-50 mt-1 w-max min-w-full max-w-[18rem] rounded-md border border-slate-700 bg-slate-900 p-1 shadow-xl shadow-black/50"
        >
          <OnEscape run={() => close(true)} />
          <div
            ref={list}
            id={`${id}-list`}
            role="listbox"
            aria-multiselectable="true"
            aria-label={name}
            aria-activedescendant={shown.options.length > 0 ? rowId(activeIndex) : undefined}
            tabIndex={-1}
            onKeyDown={onListKey}
            className="max-h-72 overflow-y-auto focus:outline-none"
          >
            {shown.options.map((o, i) => {
              const on = chosen.has(o.value);
              const isActive = i === activeIndex;
              return (
                <Fragment key={o.value}>
                  <div
                    id={rowId(i)}
                    role="option"
                    aria-selected={on}
                    data-active={isActive || undefined}
                    onClick={() => toggle(o.value)}
                    onMouseEnter={() => setActive(i)}
                    className={`flex h-7 cursor-pointer items-center gap-2 rounded px-2 text-xs text-slate-200 ${
                      isActive ? "bg-slate-800 ring-1 ring-inset ring-blue-500/60" : ""
                    }`}
                  >
                    {on ? (
                      <span className="flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded-[3px] border border-blue-500 bg-blue-500">
                        <Check aria-hidden="true" className="h-2.5 w-2.5 text-white" strokeWidth={3} />
                      </span>
                    ) : (
                      <span className="h-3.5 w-3.5 shrink-0 rounded-[3px] border border-slate-600 bg-slate-800" />
                    )}
                    {renderOption ? renderOption(o) : <span className="truncate">{o.label}</span>}
                  </div>
                  {o.value === divideAfter && <div aria-hidden="true" className="my-1 h-px bg-slate-800" />}
                </Fragment>
              );
            })}
          </div>
          {chosen.size > 0 && (
            <div className="mt-1 flex items-center justify-between gap-4 border-t border-slate-800 px-2 pt-1.5 pb-0.5 text-[11px]">
              <span className="text-slate-500">{chosen.size} selected</span>
              <button
                type="button"
                onClick={() => {
                  onChange([]);
                  list.current?.focus();
                }}
                className="rounded px-1.5 py-0.5 text-slate-300 hover:bg-slate-800 hover:text-slate-100"
              >
                Clear
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

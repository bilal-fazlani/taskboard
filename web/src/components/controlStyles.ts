// The look of the filter bar's controls (FilterPanel and the ProjectSelect it
// shares with the Epics view), kept in one place so every control has the
// same box: 32px tall, 6px radius, 10px side padding, 12px text and a 1px
// border. The height is fixed rather than built from padding, because a
// select, a search input and a button each size their content differently.
// The line height is `normal` because Chrome forces it on a select; a button
// or input on any other line height centres its text half a pixel apart and
// rounds it onto a different baseline.

/** The box every filter bar control has. */
export const CONTROL_BOX = "h-8 rounded-md border px-2.5 py-0 text-xs leading-[normal]";

/** A select or text field; a set one stands out from the unset ones. */
export const fieldClass = (set: boolean) =>
  `${CONTROL_BOX} bg-slate-800 focus:outline-none focus:ring-1 focus:ring-blue-500 ${
    set ? "border-blue-500/60 text-slate-100" : "border-slate-700 text-slate-300"
  }`;

/**
 * A plain button beside the fields, such as Clear filters. Its border is
 * transparent, so its box and its text sit where the fields' do.
 */
export const CONTROL_BUTTON = `${CONTROL_BOX} inline-flex items-center gap-1 border-transparent text-slate-400 transition-colors hover:bg-slate-800 hover:text-slate-200`;

/**
 * A segmented control's frame, such as Dependencies' Dim | Hide: the fields'
 * height, radius, border and text, with its options inset by 2px. A set one,
 * off its default, has a set field's blue border.
 */
export const segmentedClass = (set: boolean) =>
  `inline-flex h-8 items-stretch gap-0.5 rounded-md border bg-slate-800 p-0.5 text-xs leading-[normal] ${
    set ? "border-blue-500/60" : "border-slate-700"
  }`;

/**
 * One option of a segmented control, a label around a visually hidden radio.
 * The selected one is filled: neutral while the control is on its default,
 * tinted blue when it's set, like a set field's border.
 */
export const segmentClass = (selected: boolean, set: boolean) =>
  `flex cursor-pointer items-center rounded px-2 transition-colors has-[:focus-visible]:ring-1 has-[:focus-visible]:ring-blue-500 ${
    selected ? (set ? "bg-blue-500/20 text-slate-100" : "bg-slate-700 text-slate-100") : "text-slate-400 hover:text-slate-200"
  }`;

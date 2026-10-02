// Custom form controls: consistent look on every browser and OS, fully keyboard accessible.
// Native date/time/select/range/checkbox widgets are not used anywhere in the app.
import {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
  type RefObject,
} from "react";
import { createPortal } from "react-dom";
import {
  addDays, addMonths, endOfDay, endOfMonth, format, isAfter, isBefore, isSameDay, isSameMonth, isToday,
  startOfDay, startOfMonth, startOfWeek, startOfYear, subDays, subMonths,
} from "date-fns";
import { CalendarDays, Check, ChevronDown, ChevronLeft, ChevronRight, ChevronUp, Clock, Minus, Plus } from "lucide-react";
import { useTranslation } from "react-i18next";
import i18n from "../lib/i18n";
import { cachedPrefs } from "../lib/prefs";
import { Button, cn } from "./ui";

// ── Popover: portal-positioned panel anchored to a trigger ───

export function Popover({ anchor, open, onClose, children, className, align = "start" }: {
  anchor: RefObject<HTMLElement | null>;
  open: boolean;
  onClose: () => void;
  children: ReactNode;
  className?: string;
  align?: "start" | "end";
}) {
  const panel = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ top: number; left: number; maxH: number } | null>(null);

  const place = useCallback(() => {
    const a = anchor.current?.getBoundingClientRect();
    const p = panel.current;
    if (!a || !p) return;
    const w = p.offsetWidth, h = p.offsetHeight, gap = 6, vw = window.innerWidth, vh = window.innerHeight;
    const below = vh - a.bottom - gap - 8, above = a.top - gap - 8;
    const up = h > below && above > below;
    let left = align === "end" ? a.right - w : a.left;
    left = Math.max(8, Math.min(left, vw - w - 8));
    setPos({ top: up ? Math.max(8, a.top - gap - Math.min(h, above)) : a.bottom + gap, left, maxH: up ? above : below });
  }, [anchor, align]);

  useLayoutEffect(() => {
    if (!open) return setPos(null);
    place();
    window.addEventListener("resize", place);
    window.addEventListener("scroll", place, true);
    return () => {
      window.removeEventListener("resize", place);
      window.removeEventListener("scroll", place, true);
    };
  }, [open, place]);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: PointerEvent) => {
      const t = e.target as Node;
      if (!panel.current?.contains(t) && !anchor.current?.contains(t)) onClose();
    };
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClose();
        anchor.current?.focus();
      }
    };
    document.addEventListener("pointerdown", onDown, true);
    document.addEventListener("keydown", onKey, true);
    return () => {
      document.removeEventListener("pointerdown", onDown, true);
      document.removeEventListener("keydown", onKey, true);
    };
  }, [open, onClose, anchor]);

  if (!open) return null;
  // Inside a modal <dialog> the panel must live in the dialog (top layer), not <body>.
  const host = anchor.current?.closest("dialog") ?? document.body;
  return createPortal(
    <div
      ref={panel}
      className={cn("fixed z-[450] overflow-auto rounded-xl border border-border bg-surface text-fg shadow-pop", className)}
      style={{ top: pos?.top ?? -9999, left: pos?.left ?? -9999, maxHeight: pos?.maxH, visibility: pos ? "visible" : "hidden" }}
    >
      {children}
    </div>,
    host,
  );
}

const triggerClass =
  "flex h-10 w-full items-center gap-2 rounded-lg border border-border-strong bg-surface px-3 text-left text-sm text-fg transition-colors hover:border-subtle focus-visible:border-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/25 disabled:opacity-50";

// ── Select ───────────────────────────────────────────────────

export type Option<T extends string> = { value: T; label: string; hint?: string; icon?: ReactNode };

export function Select<T extends string>({ value, onChange, options, id, label, className, disabled, placeholder }: {
  value: T;
  onChange: (v: T) => void;
  options: Option<T>[];
  id?: string;
  label?: string;
  className?: string;
  disabled?: boolean;
  placeholder?: string;
}) {
  const btn = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const listId = useId();
  const current = options.find((o) => o.value === value);

  const openList = () => {
    setActive(Math.max(0, options.findIndex((o) => o.value === value)));
    setOpen(true);
  };
  const choose = (o: Option<T>) => {
    onChange(o.value);
    setOpen(false);
    btn.current?.focus();
  };
  const onKey = (e: KeyboardEvent) => {
    if (!open && ["ArrowDown", "ArrowUp", "Enter", " "].includes(e.key)) {
      e.preventDefault();
      return openList();
    }
    if (!open) return;
    if (e.key === "ArrowDown") { e.preventDefault(); setActive((a) => Math.min(a + 1, options.length - 1)); }
    if (e.key === "ArrowUp") { e.preventDefault(); setActive((a) => Math.max(a - 1, 0)); }
    if (e.key === "Home") { e.preventDefault(); setActive(0); }
    if (e.key === "End") { e.preventDefault(); setActive(options.length - 1); }
    if (e.key === "Enter" || e.key === " ") { e.preventDefault(); choose(options[active]); }
    if (e.key === "Tab") setOpen(false);
  };

  return (
    <>
      <button
        ref={btn}
        id={id}
        type="button"
        role="combobox"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={listId}
        aria-label={label}
        aria-activedescendant={open ? `${listId}-${active}` : undefined}
        disabled={disabled}
        onClick={() => (open ? setOpen(false) : openList())}
        onKeyDown={onKey}
        className={cn(triggerClass, className)}
      >
        {current?.icon}
        <span className={cn("min-w-0 flex-1 truncate", !current && "text-subtle")}>{current?.label ?? placeholder}</span>
        <ChevronDown className={cn("size-4 shrink-0 text-subtle transition-transform", open && "rotate-180")} aria-hidden />
      </button>
      <Popover anchor={btn} open={open} onClose={() => setOpen(false)} className="p-1" >
        <ul id={listId} role="listbox" aria-label={label} style={{ minWidth: btn.current?.offsetWidth }}>
          {options.map((o, i) => (
            <li
              key={o.value}
              id={`${listId}-${i}`}
              role="option"
              aria-selected={o.value === value}
              onPointerEnter={() => setActive(i)}
              onClick={() => choose(o)}
              className={cn("flex cursor-pointer items-center gap-2.5 rounded-lg px-2.5 py-2 text-sm", i === active && "bg-surface-2")}
            >
              {o.icon}
              <span className="min-w-0 flex-1">
                <span className="block truncate">{o.label}</span>
                {o.hint && <span className="block truncate text-xs text-muted">{o.hint}</span>}
              </span>
              <Check className={cn("size-4 shrink-0 text-primary", o.value !== value && "invisible")} aria-hidden />
            </li>
          ))}
        </ul>
      </Popover>
    </>
  );
}

// ── Checkbox, Switch, RadioCards ─────────────────────────────

export function Checkbox({ checked, onChange, label, description }: { checked: boolean; onChange: (v: boolean) => void; label: string; description?: string }) {
  return (
    <button type="button" role="checkbox" aria-checked={checked} onClick={() => onChange(!checked)} className="group flex items-start gap-3 text-left">
      <span className={cn("mt-0.5 grid size-5 shrink-0 place-items-center rounded-md border transition-colors", checked ? "border-primary bg-primary text-primary-fg" : "border-border-strong bg-surface group-hover:border-subtle")}>
        <Check className={cn("size-3.5 transition-transform", checked ? "scale-100" : "scale-0")} strokeWidth={3} aria-hidden />
      </span>
      <span>
        <span className="block text-sm font-medium">{label}</span>
        {description && <span className="block text-sm text-muted">{description}</span>}
      </span>
    </button>
  );
}

export function Switch({ checked, onChange, label }: { checked: boolean; onChange: (v: boolean) => void; label: string }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      onClick={() => onChange(!checked)}
      className={cn("relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors", checked ? "bg-primary" : "bg-surface-3")}
    >
      <span className={cn("inline-block size-5 rounded-full bg-white shadow-sm transition-transform", checked ? "translate-x-[22px]" : "translate-x-0.5")} />
    </button>
  );
}

export function RadioCards<T extends string>({ value, onChange, options, label, columns = 2 }: {
  value: T;
  onChange: (v: T) => void;
  options: (Option<T> & { badge?: string })[];
  label: string;
  columns?: 1 | 2 | 3;
}) {
  const refs = useRef<(HTMLButtonElement | null)[]>([]);
  const move = (i: number) => {
    const n = (i + options.length) % options.length;
    onChange(options[n].value);
    refs.current[n]?.focus();
  };
  return (
    <div role="radiogroup" aria-label={label} className={cn("grid gap-2", columns === 2 && "sm:grid-cols-2", columns === 3 && "sm:grid-cols-3")}>
      {options.map((o, i) => {
        const on = o.value === value;
        return (
          <button
            key={o.value}
            ref={(el) => { refs.current[i] = el; }}
            type="button"
            role="radio"
            aria-checked={on}
            tabIndex={on || (!options.some((x) => x.value === value) && i === 0) ? 0 : -1}
            onClick={() => onChange(o.value)}
            onKeyDown={(e) => {
              if (e.key === "ArrowDown" || e.key === "ArrowRight") { e.preventDefault(); move(i + 1); }
              if (e.key === "ArrowUp" || e.key === "ArrowLeft") { e.preventDefault(); move(i - 1); }
            }}
            className={cn("flex items-center gap-3 rounded-xl border p-3 text-left transition-colors", on ? "border-primary bg-primary-subtle" : "border-border hover:bg-surface-2")}
          >
            <span className={cn("grid size-4 shrink-0 place-items-center rounded-full border-2", on ? "border-primary" : "border-border-strong")}>
              <span className={cn("size-1.5 rounded-full bg-primary transition-transform", on ? "scale-100" : "scale-0")} />
            </span>
            {o.icon}
            <span className="min-w-0">
              <span className="block text-sm font-medium">
                {o.label}
                {o.badge && <span className="ml-2 rounded-full bg-primary/15 px-1.5 py-0.5 text-[11px] font-semibold text-primary">{o.badge}</span>}
              </span>
              {o.hint && <span className="block text-xs text-muted">{o.hint}</span>}
            </span>
          </button>
        );
      })}
    </div>
  );
}

// ── Slider & Stepper ─────────────────────────────────────────

export function Slider({ value, onChange, min, max, step = 1, label, id, format: fmt }: {
  value: number;
  onChange: (v: number) => void;
  min: number;
  max: number;
  step?: number;
  label: string;
  id?: string;
  format?: (v: number) => string;
}) {
  const track = useRef<HTMLDivElement>(null);
  const clamp = (v: number) => Math.min(max, Math.max(min, Math.round((v - min) / step) * step + min));
  const fromPointer = (clientX: number) => {
    const r = track.current!.getBoundingClientRect();
    onChange(clamp(min + ((clientX - r.left) / r.width) * (max - min)));
  };
  const pct = ((value - min) / (max - min)) * 100;
  return (
    <div
      ref={track}
      className="relative flex h-6 cursor-pointer touch-none items-center"
      onPointerDown={(e) => {
        e.currentTarget.setPointerCapture(e.pointerId);
        fromPointer(e.clientX);
      }}
      onPointerMove={(e) => e.buttons === 1 && fromPointer(e.clientX)}
    >
      <div className="h-1.5 w-full rounded-full bg-surface-3">
        <div className="h-full rounded-full bg-primary" style={{ width: `${pct}%` }} />
      </div>
      <div
        id={id}
        role="slider"
        tabIndex={0}
        aria-label={label}
        aria-valuemin={min}
        aria-valuemax={max}
        aria-valuenow={value}
        aria-valuetext={fmt?.(value)}
        onKeyDown={(e) => {
          const big = (max - min) / 10;
          const next = { ArrowRight: value + step, ArrowUp: value + step, ArrowLeft: value - step, ArrowDown: value - step, PageUp: value + big, PageDown: value - big, Home: min, End: max }[e.key];
          if (next !== undefined) {
            e.preventDefault();
            onChange(clamp(next));
          }
        }}
        className="absolute size-5 -translate-x-1/2 rounded-full border-2 border-primary bg-surface shadow-md transition-transform hover:scale-110 focus-visible:ring-4 focus-visible:ring-primary/25 focus-visible:outline-none"
        style={{ left: `${pct}%` }}
      />
    </div>
  );
}

export function Stepper({ value, onChange, min, max, step = 1, label, suffix, id }: {
  value: number;
  onChange: (v: number) => void;
  min: number;
  max: number;
  step?: number;
  label: string;
  suffix?: string;
  id?: string;
}) {
  const [text, setText] = useState(String(value));
  useEffect(() => setText(String(value)), [value]);
  const set = (v: number) => onChange(Math.min(max, Math.max(min, v)));
  return (
    <div className="inline-flex h-10 items-center rounded-lg border border-border-strong bg-surface">
      <button type="button" className="grid h-full w-9 place-items-center text-muted hover:text-fg disabled:opacity-40" onClick={() => set(value - step)} disabled={value <= min} aria-label={`${label} −`}>
        <Minus className="size-4" />
      </button>
      <input
        id={id}
        role="spinbutton"
        inputMode="numeric"
        aria-label={label}
        aria-valuenow={value}
        aria-valuemin={min}
        aria-valuemax={max}
        value={text}
        onChange={(e) => setText(e.target.value.replace(/\D/g, ""))}
        onBlur={() => (text === "" ? setText(String(value)) : set(Number(text)))}
        onKeyDown={(e) => {
          if (e.key === "ArrowUp") { e.preventDefault(); set(value + step); }
          if (e.key === "ArrowDown") { e.preventDefault(); set(value - step); }
          if (e.key === "Enter") set(Number(text || value));
        }}
        className="h-full w-12 bg-transparent text-center text-sm tabular-nums focus:outline-none"
      />
      {suffix && <span className="pr-1 text-sm text-muted">{suffix}</span>}
      <button type="button" className="grid h-full w-9 place-items-center text-muted hover:text-fg disabled:opacity-40" onClick={() => set(value + step)} disabled={value >= max} aria-label={`${label} +`}>
        <Plus className="size-4" />
      </button>
    </div>
  );
}

// ── Time field (HH:MM spinbuttons) ───────────────────────────

function Segment({ value, max, label, onChange }: { value: number; max: number; label: string; onChange: (v: number) => void }) {
  const typed = useRef("");
  return (
    <span
      role="spinbutton"
      tabIndex={0}
      aria-label={label}
      aria-valuenow={value}
      aria-valuemin={0}
      aria-valuemax={max}
      onBlur={() => (typed.current = "")}
      onKeyDown={(e) => {
        if (e.key === "ArrowUp") { e.preventDefault(); onChange(value >= max ? 0 : value + 1); }
        if (e.key === "ArrowDown") { e.preventDefault(); onChange(value <= 0 ? max : value - 1); }
        if (/^\d$/.test(e.key)) {
          e.preventDefault();
          typed.current = (typed.current + e.key).slice(-2);
          onChange(Math.min(max, Number(typed.current)));
        }
      }}
      className="rounded px-0.5 tabular-nums outline-none focus:bg-primary focus:text-primary-fg"
    >
      {String(value).padStart(2, "0")}
    </span>
  );
}

export function TimeField({ value, onChange, label }: { value: { h: number; m: number }; onChange: (v: { h: number; m: number }) => void; label: string }) {
  const bump = (d: number) => {
    const total = (value.h * 60 + value.m + d + 1440) % 1440;
    onChange({ h: Math.floor(total / 60), m: total % 60 });
  };
  return (
    <div role="group" aria-label={label} className="flex h-10 items-center gap-1 rounded-lg border border-border-strong bg-surface pr-1 pl-3 text-sm">
      <Clock className="mr-1 size-4 text-subtle" aria-hidden />
      <Segment value={value.h} max={23} label={`${label} hour`} onChange={(h) => onChange({ ...value, h })} />
      <span className="text-subtle">:</span>
      <Segment value={value.m} max={59} label={`${label} minute`} onChange={(m) => onChange({ ...value, m })} />
      <span className="ml-auto flex flex-col">
        <button type="button" tabIndex={-1} aria-hidden className="text-subtle hover:text-fg" onClick={() => bump(15)}><ChevronUp className="size-3.5" /></button>
        <button type="button" tabIndex={-1} aria-hidden className="text-subtle hover:text-fg" onClick={() => bump(-15)}><ChevronDown className="size-3.5" /></button>
      </span>
    </div>
  );
}

// ── Calendar ─────────────────────────────────────────────────

const WEEK_START = 1; // Monday

export function Calendar({ month, onMonth, start, end, onPick, marked, maxDate, hover, onHover }: {
  month: Date;
  onMonth: (d: Date) => void;
  start?: Date | null;
  end?: Date | null;
  onPick: (d: Date) => void;
  marked?: Record<string, number>;
  maxDate?: Date;
  hover?: Date | null;
  onHover?: (d: Date | null) => void;
}) {
  const { t } = useTranslation();
  const [focus, setFocus] = useState<Date>(start ?? month);
  const grid = useRef<HTMLDivElement>(null);
  const days = useMemo(() => {
    const first = startOfWeek(startOfMonth(month), { weekStartsOn: WEEK_START });
    return Array.from({ length: 42 }, (_, i) => addDays(first, i));
  }, [month]);
  const weekdays = useMemo(() => days.slice(0, 7).map((d) => d.toLocaleDateString(i18n.language, { weekday: "narrow" })), [days]);

  useEffect(() => {
    if (!isSameMonth(focus, month)) setFocus(startOfMonth(month));
  }, [month]); // eslint-disable-line react-hooks/exhaustive-deps

  const moveFocus = (d: Date) => {
    setFocus(d);
    if (!isSameMonth(d, month)) onMonth(startOfMonth(d));
    requestAnimationFrame(() => grid.current?.querySelector<HTMLElement>(`[data-day="${format(d, "yyyy-MM-dd")}"]`)?.focus());
  };
  const rangeEnd = end ?? (start && hover && isAfter(hover, start) ? hover : null);

  return (
    <div className="w-[17.5rem] select-none">
      <div className="mb-2 flex items-center justify-between">
        <Button variant="ghost" size="icon" className="size-8" onClick={() => onMonth(subMonths(month, 1))} aria-label={t("cal.prevMonth")}><ChevronLeft className="size-4" /></Button>
        <p className="text-sm font-semibold" aria-live="polite">{month.toLocaleDateString(i18n.language, { month: "long", year: "numeric" })}</p>
        <Button variant="ghost" size="icon" className="size-8" onClick={() => onMonth(addMonths(month, 1))} aria-label={t("cal.nextMonth")} disabled={!!maxDate && isAfter(startOfMonth(addMonths(month, 1)), maxDate)}><ChevronRight className="size-4" /></Button>
      </div>
      <div className="grid grid-cols-7 text-center text-[11px] font-medium text-subtle" aria-hidden>
        {weekdays.map((w, i) => <span key={i} className="py-1">{w}</span>)}
      </div>
      <div ref={grid} role="grid" className="grid grid-cols-7" onPointerLeave={() => onHover?.(null)}>
        {days.map((d) => {
          const key = format(d, "yyyy-MM-dd");
          const disabled = !!maxDate && isAfter(startOfDay(d), maxDate);
          const isStart = !!start && isSameDay(d, start);
          const isEnd = !!rangeEnd && isSameDay(d, rangeEnd);
          const inRange = !!start && !!rangeEnd && isAfter(d, start) && isBefore(d, rangeEnd);
          const outside = !isSameMonth(d, month);
          return (
            <div key={key} role="gridcell" className={cn("relative py-0.5", inRange && "bg-primary-subtle", isStart && rangeEnd && !isEnd && "rounded-l-full bg-primary-subtle", isEnd && start && !isStart && "rounded-r-full bg-primary-subtle")}>
              <button
                type="button"
                data-day={key}
                tabIndex={isSameDay(d, focus) ? 0 : -1}
                disabled={disabled}
                aria-pressed={isStart || isEnd}
                aria-label={d.toLocaleDateString(i18n.language, { weekday: "long", day: "numeric", month: "long", year: "numeric" }) + (marked?.[key] ? `, ${t("cal.hasData")}` : "")}
                onClick={() => onPick(d)}
                onPointerEnter={() => onHover?.(d)}
                onKeyDown={(e) => {
                  const step = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 }[e.key];
                  if (step) { e.preventDefault(); moveFocus(addDays(d, step)); }
                  if (e.key === "PageUp") { e.preventDefault(); moveFocus(subMonths(d, 1)); }
                  if (e.key === "PageDown") { e.preventDefault(); moveFocus(addMonths(d, 1)); }
                }}
                className={cn(
                  "relative mx-auto grid size-9 place-items-center rounded-full text-sm tabular-nums transition-colors disabled:cursor-not-allowed disabled:opacity-30",
                  isStart || isEnd ? "bg-primary font-semibold text-primary-fg" : "hover:bg-surface-2",
                  outside && !(isStart || isEnd) && "text-subtle",
                  isToday(d) && !(isStart || isEnd) && "font-semibold text-primary",
                )}
              >
                {d.getDate()}
                {!!marked?.[key] && <span className={cn("absolute bottom-1 size-1 rounded-full", isStart || isEnd ? "bg-primary-fg" : "bg-primary")} aria-hidden />}
              </button>
            </div>
          );
        })}
      </div>
    </div>
  );
}

// ── Date picker (single day) ─────────────────────────────────

export function DatePicker({ value, onChange, marked, onMonthChange, maxDate, label, className }: {
  value: Date;
  onChange: (d: Date) => void;
  marked?: Record<string, number>;
  onMonthChange?: (month: Date) => void;
  maxDate?: Date;
  label: string;
  className?: string;
}) {
  const { t } = useTranslation();
  const btn = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const [month, setMonth] = useState(startOfMonth(value));
  useEffect(() => setMonth(startOfMonth(value)), [value]);
  useEffect(() => onMonthChange?.(month), [month]); // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <>
      <button ref={btn} type="button" aria-label={label} aria-haspopup="dialog" aria-expanded={open} onClick={() => setOpen(!open)} className={cn(triggerClass, className)}>
        <CalendarDays className="size-4 text-subtle" aria-hidden />
        <span className="flex-1 truncate font-medium">{value.toLocaleDateString(i18n.language, { weekday: "short", day: "numeric", month: "short", year: "numeric" })}</span>
        <ChevronDown className="size-4 text-subtle" aria-hidden />
      </button>
      <Popover anchor={btn} open={open} onClose={() => setOpen(false)} className="p-3">
        <div role="dialog" aria-label={label}>
          <Calendar month={month} onMonth={setMonth} start={value} end={value} marked={marked} maxDate={maxDate} onPick={(d) => { onChange(d); setOpen(false); btn.current?.focus(); }} />
          <div className="mt-2 flex justify-between border-t border-border pt-2">
            <Button variant="ghost" size="sm" onClick={() => { onChange(new Date()); setOpen(false); }}>{t("timeline.today")}</Button>
            {marked && <span className="flex items-center gap-1.5 text-xs text-subtle"><span className="size-1.5 rounded-full bg-primary" />{t("cal.hasData")}</span>}
          </div>
        </div>
      </Popover>
    </>
  );
}

// ── Range picker (dates, optionally with times) ──────────────

export type Range = { from: number; to: number; preset?: string };

export const presets: { id: string; get: () => [Date, Date] }[] = [
  { id: "today", get: () => [startOfDay(new Date()), endOfDay(new Date())] },
  { id: "yesterday", get: () => [startOfDay(subDays(new Date(), 1)), endOfDay(subDays(new Date(), 1))] },
  { id: "last7", get: () => [startOfDay(subDays(new Date(), 6)), endOfDay(new Date())] },
  { id: "last30", get: () => [startOfDay(subDays(new Date(), 29)), endOfDay(new Date())] },
  { id: "thisMonth", get: () => [startOfMonth(new Date()), endOfDay(new Date())] },
  { id: "lastMonth", get: () => [startOfMonth(subMonths(new Date(), 1)), endOfMonth(subMonths(new Date(), 1))] },
  { id: "thisYear", get: () => [startOfYear(new Date()), endOfDay(new Date())] },
  { id: "all", get: () => [new Date(2000, 0, 1), endOfDay(new Date())] },
];

export function presetRange(id: string): Range {
  const p = presets.find((x) => x.id === id) ?? presets[0];
  const [a, b] = p.get();
  return { from: a.getTime(), to: b.getTime(), preset: p.id };
}

export function rangeLabel(r: Range): string {
  if (r.preset) return i18n.t(`range.${r.preset}`);
  const a = new Date(r.from), b = new Date(r.to);
  const withTime = a.getHours() + a.getMinutes() !== 0 || !(b.getHours() === 23 && b.getMinutes() === 59);
  const d = (x: Date) => x.toLocaleDateString(i18n.language, { day: "numeric", month: "short", year: a.getFullYear() === b.getFullYear() ? undefined : "numeric" });
  const tm = (x: Date) => x.toLocaleTimeString(i18n.language, { hour: "2-digit", minute: "2-digit", hour12: cachedPrefs().clock === "12h" });
  if (isSameDay(a, b)) return withTime ? `${d(a)}, ${tm(a)} – ${tm(b)}` : d(a);
  return withTime ? `${d(a)} ${tm(a)} – ${d(b)} ${tm(b)}` : `${d(a)} – ${d(b)}`;
}

export function RangePicker({ value, onChange, marked, onMonthChange, label, className, allowAll = true }: {
  value: Range;
  onChange: (r: Range) => void;
  marked?: Record<string, number>;
  onMonthChange?: (month: Date) => void;
  label: string;
  className?: string;
  allowAll?: boolean;
}) {
  const { t } = useTranslation();
  const btn = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const [start, setStart] = useState<Date | null>(null);
  const [end, setEnd] = useState<Date | null>(null);
  const [hover, setHover] = useState<Date | null>(null);
  const [withTime, setWithTime] = useState(false);
  const [times, setTimes] = useState({ a: { h: 0, m: 0 }, b: { h: 23, m: 59 } });
  const [month, setMonth] = useState(startOfMonth(new Date(value.to)));
  useEffect(() => onMonthChange?.(month), [month]); // eslint-disable-line react-hooks/exhaustive-deps

  const openPanel = () => {
    const a = new Date(value.from), b = new Date(value.to);
    setStart(a);
    setEnd(b);
    setMonth(startOfMonth(b));
    const timed = !value.preset && (a.getHours() + a.getMinutes() !== 0 || !(b.getHours() === 23 && b.getMinutes() === 59));
    setWithTime(timed);
    setTimes({ a: { h: a.getHours(), m: a.getMinutes() }, b: { h: timed ? b.getHours() : 23, m: timed ? b.getMinutes() : 59 } });
    setOpen(true);
  };
  const pick = (d: Date) => {
    if (!start || end) {
      setStart(d);
      setEnd(null);
    } else if (isBefore(d, start)) {
      setStart(d);
    } else {
      setEnd(d);
    }
  };
  // The range the current selection would produce (used for the preview and Apply).
  const draft = (): Range | null => {
    if (!start) return null;
    const a = new Date(start), b = new Date(end ?? start);
    if (withTime) {
      a.setHours(times.a.h, times.a.m, 0, 0);
      b.setHours(times.b.h, times.b.m, 59, 999);
    } else {
      a.setHours(0, 0, 0, 0);
      b.setHours(23, 59, 59, 999);
    }
    return b.getTime() > a.getTime() ? { from: a.getTime(), to: b.getTime() } : null;
  };
  const apply = () => {
    const r = draft();
    if (!r) return;
    onChange(r);
    setOpen(false);
    btn.current?.focus();
  };
  const preview = draft();
  const invalid = !!start && !preview;

  return (
    <>
      <button ref={btn} type="button" aria-label={label} aria-haspopup="dialog" aria-expanded={open} onClick={() => (open ? setOpen(false) : openPanel())} className={cn(triggerClass, className)}>
        <CalendarDays className="size-4 shrink-0 text-subtle" aria-hidden />
        <span className="min-w-0 flex-1 truncate font-medium">{rangeLabel(value)}</span>
        <ChevronDown className="size-4 shrink-0 text-subtle" aria-hidden />
      </button>
      <Popover anchor={btn} open={open} onClose={() => setOpen(false)}>
        <div role="dialog" aria-label={label} className="flex flex-col sm:flex-row">
          <ul className="flex gap-1 overflow-x-auto border-b border-border p-2 sm:w-40 sm:flex-col sm:border-r sm:border-b-0" aria-label={t("range.presets")}>
            {presets.filter((p) => allowAll || p.id !== "all").map((p) => (
              <li key={p.id} className="shrink-0">
                <button
                  type="button"
                  onClick={() => { onChange(presetRange(p.id)); setOpen(false); }}
                  className={cn("w-full rounded-lg px-3 py-2 text-left text-sm whitespace-nowrap hover:bg-surface-2", value.preset === p.id && "bg-primary-subtle font-medium text-primary")}
                >
                  {t(`range.${p.id}`)}
                </button>
              </li>
            ))}
          </ul>
          <div className="p-3">
            <Calendar month={month} onMonth={setMonth} start={start} end={end} hover={hover} onHover={setHover} onPick={pick} marked={marked} maxDate={endOfDay(new Date())} />
            <div className="mt-3 space-y-3 border-t border-border pt-3">
              <label className="flex items-center justify-between gap-3 text-sm">
                <span className="font-medium">{t("range.includeTime")}</span>
                <Switch checked={withTime} onChange={setWithTime} label={t("range.includeTime")} />
              </label>
              {withTime && (
                <div className="grid grid-cols-2 gap-2">
                  <TimeField label={t("range.startTime")} value={times.a} onChange={(a) => setTimes({ ...times, a })} />
                  <TimeField label={t("range.endTime")} value={times.b} onChange={(b) => setTimes({ ...times, b })} />
                </div>
              )}
              <p className="text-xs text-muted" aria-live="polite">
                {!start ? t("range.pickStart") : invalid ? t("range.endBeforeStart") : preview ? rangeLabel(preview) + (end ? "" : ` · ${t("range.pickEnd")}`) : null}
              </p>
              <div className="flex justify-end gap-2">
                <Button size="sm" variant="ghost" onClick={() => setOpen(false)}>{t("common.cancel")}</Button>
                <Button size="sm" variant="primary" disabled={!preview} onClick={apply}>{t("range.apply")}</Button>
              </div>
            </div>
          </div>
        </div>
      </Popover>
    </>
  );
}

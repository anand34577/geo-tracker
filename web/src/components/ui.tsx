import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useId,
  useRef,
  useState,
  type ComponentProps,
  type InputHTMLAttributes,
  type ReactNode,
} from "react";
import { setErrorToast } from "../lib/api";
import { AlertTriangle, CheckCircle2, Eye, EyeOff, Loader2, X, type LucideIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import i18n from "../lib/i18n";

/** Long lists render a slice and grow on demand, so the page stays fast as data accumulates. */
export function useLimit(step = 25, resetKey?: unknown) {
  const [n, setN] = useState(step);
  useEffect(() => setN(step), [step, resetKey]);
  return { limit: n, more: () => setN((x) => x + step), step };
}

/** "Show N more" row under a sliced list; renders nothing when everything is visible. */
export function ShowMore({ total, limit, onMore, step }: { total: number; limit: number; onMore: () => void; step: number }) {
  const { t } = useTranslation();
  if (total <= limit) return null;
  return (
    <div className="flex items-center justify-center gap-3 border-t border-border p-3 text-sm text-muted">
      <span className="tabular-nums">{t("common.showing", { shown: limit, total })}</span>
      <Button size="sm" onClick={onMore}>{t("common.showMore", { count: Math.min(step, total - limit) })}</Button>
    </div>
  );
}

export const cn = (...xs: (string | false | null | undefined)[]) => xs.filter(Boolean).join(" ");

// ── Button ───────────────────────────────────────────────────

const variants = {
  primary: "bg-primary text-primary-fg shadow-btn hover:bg-primary-hover hover:shadow-btn-hover",
  secondary: "bg-surface text-fg border border-border-strong shadow-sm hover:bg-surface-2 hover:border-border-strong",
  ghost: "text-muted hover:text-fg hover:bg-surface-2",
  danger: "bg-danger text-white shadow-sm hover:opacity-90 dark:text-black",
};
const sizes = { sm: "h-8 px-3 text-sm gap-1.5", md: "h-10 px-4 text-sm gap-2", icon: "size-10 justify-center" };

type ButtonProps = ComponentProps<"button"> & {
  variant?: keyof typeof variants;
  size?: keyof typeof sizes;
  loading?: boolean;
  icon?: LucideIcon;
};

export function Button({ variant = "secondary", size = "md", loading, icon: Icon, className, children, disabled, ...rest }: ButtonProps) {
  return (
    <button
      className={cn(
        "inline-flex shrink-0 items-center rounded-lg font-medium whitespace-nowrap transition-[background-color,box-shadow,transform,border-color] duration-150 active:scale-[0.98] disabled:pointer-events-none disabled:opacity-50",
        variants[variant],
        sizes[size],
        className,
      )}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      {...rest}
    >
      {loading ? <Loader2 className="size-4 animate-spin" aria-hidden /> : Icon && <Icon className="size-4" aria-hidden />}
      {children}
    </button>
  );
}

// ── Form fields (persistent labels, never placeholder-only) ──

export function Field({ label, hint, error, children }: { label: string; hint?: ReactNode; error?: string; children: (id: string, describedBy?: string) => ReactNode }) {
  const id = useId();
  const hintId = hint || error ? id + "-hint" : undefined;
  return (
    <div className="flex flex-col gap-1.5">
      <label htmlFor={id} className="text-sm font-medium">
        {label}
      </label>
      {children(id, hintId)}
      {(error || hint) && (
        <p id={hintId} className={cn("text-xs", error ? "text-danger" : "text-subtle")}>
          {error || hint}
        </p>
      )}
    </div>
  );
}

const control =
  "h-10 w-full rounded-lg border border-border-strong bg-surface px-3 text-sm text-fg shadow-sm placeholder:text-subtle transition-[border-color,box-shadow] hover:border-subtle focus:border-primary focus:outline-none focus-visible:ring-4 focus-visible:ring-primary/15 disabled:opacity-60";

export function Input({ className, type, ...rest }: InputHTMLAttributes<HTMLInputElement>) {
  const [shown, setShown] = useState(false);
  // Addresses are typed, not written: phone keyboards must not autocorrect, capitalise or "smart-punctuate"
  // the "//" in https://. type=url also makes some browsers validate/normalise the value, so use a text
  // input with the URL keyboard instead.
  if (type === "url")
    return <input type="text" inputMode="url" autoCapitalize="none" autoCorrect="off" spellCheck={false} autoComplete="off" className={cn(control, className)} {...rest} />;
  if (type !== "password") return <input type={type} className={cn(control, className)} {...rest} />;
  // Secrets get a reveal toggle: typos in a password or API token are otherwise invisible.
  return (
    <div className="relative">
      <input type={shown ? "text" : "password"} className={cn(control, "pr-10", className)} {...rest} />
      <button
        type="button"
        onClick={() => setShown(!shown)}
        aria-pressed={shown}
        aria-label={i18n.t(shown ? "common.hidePassword" : "common.showPassword")}
        title={i18n.t(shown ? "common.hidePassword" : "common.showPassword")}
        className="absolute inset-y-0 right-0 grid w-10 place-items-center rounded-r-lg text-subtle hover:text-fg"
      >
        {shown ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
      </button>
    </div>
  );
}

/** Segmented control for 2–4 mutually exclusive options. */
export function Segmented<T extends string>({ value, onChange, options, label }: { value: T; onChange: (v: T) => void; options: { value: T; label: string }[]; label: string }) {
  return (
    <div role="radiogroup" aria-label={label} className="inline-flex rounded-lg border border-border bg-surface-2 p-0.5">
      {options.map((o) => (
        <button
          key={o.value}
          role="radio"
          aria-checked={value === o.value}
          onClick={() => onChange(o.value)}
          className={cn(
            "h-8 rounded-md px-3 text-sm font-medium transition-colors",
            value === o.value ? "bg-surface text-fg shadow-sm" : "text-muted hover:text-fg",
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

// ── Layout ───────────────────────────────────────────────────

/** One width and gutter for every full page, so titles stay put when you move between sections. */
export const pageClass = "mx-auto w-full max-w-6xl px-4 py-6 md:px-8 md:py-10";

export function PageHeader({ title, description, actions, icon: Icon }: { title: string; description?: string; actions?: ReactNode; icon?: LucideIcon }) {
  return (
    <div className="mb-8 flex flex-wrap items-end justify-between gap-x-6 gap-y-4">
      <div className="flex min-w-0 items-center gap-4">
        {Icon && (
          <span className="hidden size-12 shrink-0 place-items-center rounded-2xl bg-primary-subtle text-primary ring-1 ring-primary/10 sm:grid">
            <Icon className="size-6" aria-hidden />
          </span>
        )}
        <div className="min-w-0">
          <h1 className="text-[1.75rem] leading-tight font-semibold tracking-tight">{title}</h1>
          {description && <p className="mt-1 max-w-prose text-sm text-muted">{description}</p>}
        </div>
      </div>
      {actions && <div className="flex shrink-0 gap-2">{actions}</div>}
    </div>
  );
}

export function Section({ title, description, children, actions }: { title: string; description?: ReactNode; children: ReactNode; actions?: ReactNode }) {
  return (
    <section className="overflow-hidden rounded-2xl border border-border bg-surface shadow-card">
      <header className="flex flex-wrap items-start justify-between gap-3 border-b border-border bg-surface-2/50 px-5 py-4">
        <div>
          <h2 className="font-semibold tracking-tight">{title}</h2>
          {description && <p className="mt-0.5 text-sm text-muted">{description}</p>}
        </div>
        {actions}
      </header>
      <div className="p-5">{children}</div>
    </section>
  );
}

export function Skeleton({ className }: { className?: string }) {
  return <div className={cn("gt-shimmer rounded-lg bg-surface-2", className)} aria-hidden />;
}

export function Spinner({ label }: { label?: string }) {
  const { t } = useTranslation();
  return (
    <div role="status" className="flex h-full min-h-32 items-center justify-center gap-2 p-8 text-sm text-muted">
      <Loader2 className="size-5 animate-spin" aria-hidden />
      {label ?? t("common.loading")}
    </div>
  );
}

export function EmptyState({ icon: Icon, title, body, action }: { icon: LucideIcon; title: string; body?: ReactNode; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center px-6 py-14 text-center">
      <div className="relative mb-5">
        <span className="absolute -inset-3 rounded-full bg-primary/5" aria-hidden />
        <span className="absolute -inset-1.5 rounded-full bg-primary/10" aria-hidden />
        <div className="relative grid size-14 place-items-center rounded-full bg-primary text-primary-fg shadow-btn">
          <Icon className="size-6" aria-hidden />
        </div>
      </div>
      <h3 className="text-base font-semibold tracking-tight">{title}</h3>
      {body && <p className="mt-1.5 max-w-sm text-sm text-muted">{body}</p>}
      {action && <div className="mt-6">{action}</div>}
    </div>
  );
}

export function ErrorState({ error, retry }: { error: unknown; retry?: () => void }) {
  const { t } = useTranslation();
  return (
    <div role="alert" className="flex flex-col items-center gap-3 px-6 py-10 text-center">
      <AlertTriangle className="size-6 text-danger" aria-hidden />
      <p className="text-sm">
        <span className="font-medium">{t("common.errorTitle")}</span>{" "}
        <span className="text-muted">{error instanceof Error ? error.message : String(error)}</span>
      </p>
      {retry && (
        <Button size="sm" onClick={retry}>
          {t("common.retry")}
        </Button>
      )}
    </div>
  );
}

export function Notice({ tone = "info", children }: { tone?: "info" | "warning" | "danger"; children: ReactNode }) {
  return (
    <div
      className={cn(
        "rounded-lg border px-4 py-3 text-sm",
        tone === "info" && "border-primary/30 bg-primary-subtle",
        tone === "warning" && "border-warning/40 bg-warning-subtle",
        tone === "danger" && "border-danger/40 bg-danger-subtle",
      )}
    >
      {children}
    </div>
  );
}

// ── Dialog (native <dialog>: focus trap, Esc and inert background for free) ──

export function Dialog({ open, onClose, title, children, footer, wide }: { open: boolean; onClose: () => void; title: string; children: ReactNode; footer?: ReactNode; wide?: boolean }) {
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  const { t } = useTranslation();
  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) {
      d.showModal();
      // showModal focuses the close button; start in the first field instead.
      d.querySelector<HTMLElement>("input:not([type=hidden]):not([readonly]), select, textarea")?.focus();
    }
    if (!open && d.open) d.close();
  }, [open]);
  return (
    <dialog
      ref={ref}
      onClose={onClose}
      onClick={(e) => e.target === ref.current && onClose()}
      aria-labelledby={titleId}
      className={cn(
        "m-auto w-[calc(100%-2rem)] rounded-2xl border border-border bg-surface p-0 text-fg shadow-pop",
        wide ? "max-w-2xl" : "max-w-md",
      )}
    >
      {open && (
        <div className="flex max-h-[85vh] flex-col">
          <header className="flex items-center justify-between gap-4 border-b border-border px-5 py-4">
            <h2 id={titleId} className="font-semibold">
              {title}
            </h2>
            <Button variant="ghost" size="icon" className="-mr-2 size-8" onClick={onClose} aria-label={t("common.close")}>
              <X className="size-4" />
            </Button>
          </header>
          <div className="overflow-y-auto px-5 py-5">{children}</div>
          {footer && <footer className="flex justify-end gap-2 border-t border-border px-5 py-3">{footer}</footer>}
        </div>
      )}
    </dialog>
  );
}

/** Irreversible actions require typing a word (spec §6.5). */
export function ConfirmDialog({ open, onClose, onConfirm, title, body, confirmLabel, typeToConfirm, loading }: {
  open: boolean;
  onClose: () => void;
  onConfirm: () => void;
  title: string;
  body: ReactNode;
  confirmLabel: string;
  typeToConfirm?: string;
  loading?: boolean;
}) {
  const { t } = useTranslation();
  const [typed, setTyped] = useState("");
  useEffect(() => { setTyped(""); }, [open]);
  const ok = !typeToConfirm || typed.trim().toLowerCase() === typeToConfirm.toLowerCase();
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={title}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button variant="danger" disabled={!ok} loading={loading} onClick={onConfirm}>
            {confirmLabel}
          </Button>
        </>
      }
    >
      <div className="space-y-4 text-sm">
        <div className="text-muted">{body}</div>
        {typeToConfirm && (
          <Field label={t("common.typeToConfirm", { word: typeToConfirm })}>
            {(id) => <Input id={id} value={typed} onChange={(e) => setTyped(e.target.value)} autoComplete="off" />}
          </Field>
        )}
      </div>
    </Dialog>
  );
}

// ── Toasts ───────────────────────────────────────────────────

/** An optional button on a toast, e.g. "Undo" after a delete. */
type ToastAction = { label: string; run: () => void };
type Toast = { id: number; tone: "success" | "error"; text: string; action?: ToastAction };
const ToastCtx = createContext<(tone: Toast["tone"], text: string, action?: ToastAction) => void>(() => {});

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const push = useCallback((tone: Toast["tone"], text: string, action?: ToastAction) => {
    const id = Date.now() + Math.random();
    setToasts((ts) => [...ts.slice(-2), { id, tone, text, action }]);
    // Toasts with an action stay longer: there's something to decide.
    setTimeout(() => setToasts((ts) => ts.filter((t) => t.id !== id)), tone === "error" || action ? 7000 : 4000);
  }, []);
  useEffect(() => { setErrorToast((text) => push("error", text)); }, [push]);
  return (
    <ToastCtx.Provider value={push}>
      {children}
      <div aria-live="polite" className="pointer-events-none fixed inset-x-0 bottom-20 z-[500] flex flex-col items-center gap-2 px-4 md:bottom-6">
        {toasts.map((t) => (
          <div key={t.id} role={t.tone === "error" ? "alert" : "status"} className="gt-pop pointer-events-auto flex max-w-md items-center gap-2 rounded-xl border border-border bg-surface px-4 py-3 text-sm shadow-pop">
            {t.tone === "success" ? <CheckCircle2 className="size-4 shrink-0 text-success" /> : <AlertTriangle className="size-4 shrink-0 text-danger" />}
            <span className="flex-1">{t.text}</span>
            {t.action && (
              <button
                onClick={() => { t.action!.run(); setToasts((ts) => ts.filter((x) => x.id !== t.id)); }}
                className="shrink-0 rounded-md px-2 py-1 text-sm font-semibold text-primary hover:bg-primary-subtle"
              >
                {t.action.label}
              </button>
            )}
            <button onClick={() => setToasts((ts) => ts.filter((x) => x.id !== t.id))} className="-mr-1 grid size-6 shrink-0 place-items-center rounded text-subtle hover:text-fg" aria-label={i18n.t("common.close")}>
              <X className="size-3.5" />
            </button>
          </div>
        ))}
      </div>
    </ToastCtx.Provider>
  );
}

export const useToast = () => useContext(ToastCtx);

/** Initials on the person's color, used for family members on lists and the map. */
export function Avatar({ name, color, className }: { name: string; color?: string; className?: string }) {
  const initials = name.split(/\s+/).filter(Boolean).slice(0, 2).map((w) => w[0]!.toUpperCase()).join("");
  return (
    <span className={cn("grid size-9 shrink-0 place-items-center rounded-full text-sm font-semibold text-white", className)} style={{ background: color || "var(--primary)" }} aria-hidden>
      {initials || "?"}
    </span>
  );
}
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
import { AlertTriangle, CheckCircle2, Loader2, X, type LucideIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

export const cn = (...xs: (string | false | null | undefined)[]) => xs.filter(Boolean).join(" ");

// ── Button ───────────────────────────────────────────────────

const variants = {
  primary: "bg-primary text-primary-fg hover:bg-primary-hover",
  secondary: "bg-surface text-fg border border-border-strong hover:bg-surface-2",
  ghost: "text-muted hover:text-fg hover:bg-surface-2",
  danger: "bg-danger text-white hover:opacity-90 dark:text-black",
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
        "inline-flex shrink-0 items-center rounded-lg font-medium whitespace-nowrap transition-colors duration-150 disabled:pointer-events-none disabled:opacity-50",
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
  "h-10 w-full rounded-lg border border-border-strong bg-surface px-3 text-sm text-fg placeholder:text-subtle transition-colors focus:border-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-primary/30 disabled:opacity-60";

export function Input({ className, ...rest }: InputHTMLAttributes<HTMLInputElement>) {
  return <input className={cn(control, className)} {...rest} />;
}

/** Segmented control for 2–4 mutually exclusive options. */
export function Segmented<T extends string>({ value, onChange, options, label }: { value: T; onChange: (v: T) => void; options: { value: T; label: string }[]; label: string }) {
  return (
    <div role="radiogroup" aria-label={label} className="inline-flex rounded-lg border border-border-strong bg-surface-2 p-0.5">
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

export function PageHeader({ title, description, actions }: { title: string; description?: string; actions?: ReactNode }) {
  return (
    <div className="mb-6 flex flex-wrap items-end justify-between gap-4">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
        {description && <p className="mt-1 max-w-prose text-sm text-muted">{description}</p>}
      </div>
      {actions && <div className="flex gap-2">{actions}</div>}
    </div>
  );
}

export function Section({ title, description, children, actions }: { title: string; description?: ReactNode; children: ReactNode; actions?: ReactNode }) {
  return (
    <section className="rounded-xl border border-border bg-surface">
      <header className="flex flex-wrap items-start justify-between gap-3 border-b border-border px-5 py-4">
        <div>
          <h2 className="font-semibold">{title}</h2>
          {description && <p className="mt-0.5 text-sm text-muted">{description}</p>}
        </div>
        {actions}
      </header>
      <div className="p-5">{children}</div>
    </section>
  );
}

export function Skeleton({ className }: { className?: string }) {
  return <div className={cn("animate-pulse rounded-lg bg-surface-2", className)} aria-hidden />;
}

export function Spinner({ label }: { label?: string }) {
  const { t } = useTranslation();
  return (
    <div role="status" className="flex items-center justify-center gap-2 p-8 text-sm text-muted">
      <Loader2 className="size-5 animate-spin" aria-hidden />
      {label ?? t("common.loading")}
    </div>
  );
}

export function EmptyState({ icon: Icon, title, body, action }: { icon: LucideIcon; title: string; body?: ReactNode; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center px-6 py-12 text-center">
      <div className="mb-4 grid size-12 place-items-center rounded-full bg-primary-subtle text-primary">
        <Icon className="size-6" aria-hidden />
      </div>
      <h3 className="font-semibold">{title}</h3>
      {body && <p className="mt-1 max-w-sm text-sm text-muted">{body}</p>}
      {action && <div className="mt-5">{action}</div>}
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

type Toast = { id: number; tone: "success" | "error"; text: string };
const ToastCtx = createContext<(tone: Toast["tone"], text: string) => void>(() => {});

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const push = useCallback((tone: Toast["tone"], text: string) => {
    const id = Date.now() + Math.random();
    setToasts((ts) => [...ts.slice(-2), { id, tone, text }]);
    setTimeout(() => setToasts((ts) => ts.filter((t) => t.id !== id)), tone === "error" ? 7000 : 4000);
  }, []);
  useEffect(() => { setErrorToast((text) => push("error", text)); }, [push]);
  return (
    <ToastCtx.Provider value={push}>
      {children}
      <div aria-live="polite" className="pointer-events-none fixed inset-x-0 bottom-20 z-[500] flex flex-col items-center gap-2 px-4 md:bottom-6">
        {toasts.map((t) => (
          <div key={t.id} role={t.tone === "error" ? "alert" : "status"} className="pointer-events-auto flex max-w-md items-center gap-2 rounded-xl border border-border bg-surface px-4 py-3 text-sm shadow-pop">
            {t.tone === "success" ? <CheckCircle2 className="size-4 shrink-0 text-success" /> : <AlertTriangle className="size-4 shrink-0 text-danger" />}
            {t.text}
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
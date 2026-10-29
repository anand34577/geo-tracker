import { useState, type FormEvent, type ReactNode } from "react";
import { Navigate, useNavigate, useSearchParams } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { Check, KeyRound, Lock } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, queryClient, useMe, type AuthMethods, type User } from "../lib/api";
import { Button, Field, Input, Notice, Spinner } from "../components/ui";
import { Logo } from "../components/Logo";

function AuthLayout({ title, subtitle, children }: { title: string; subtitle: string; children: ReactNode }) {
  const { t } = useTranslation();
  return (
    <main className="grid min-h-dvh bg-bg lg:grid-cols-[1.05fr_1fr]">
      <BrandPanel />
      <div className="flex items-center justify-center px-4 py-10 sm:px-8">
        <div className="gt-rise w-full max-w-sm">
          <div className="mb-8 flex flex-col items-center text-center lg:items-start lg:text-left">
            <Logo className="mb-5 size-12 lg:hidden" />
            <h1 className="text-[1.75rem] leading-tight font-semibold tracking-tight">{title}</h1>
            <p className="mt-2 text-sm text-muted">{subtitle}</p>
          </div>
          <div className="rounded-2xl border border-border bg-surface p-6 shadow-pop">{children}</div>
          <p className="mt-6 flex items-center justify-center gap-1.5 text-xs text-subtle lg:justify-start">
            <Lock className="size-3" aria-hidden />
            {t("auth.privacy")}
          </p>
        </div>
      </div>
    </main>
  );
}

/** The left half of sign-in on wide screens: what the app is, drawn as a route between two places. */
function BrandPanel() {
  const { t } = useTranslation();
  const features = [t("auth.feature1"), t("auth.feature2"), t("auth.feature3")];
  return (
    <aside className="relative hidden overflow-hidden bg-side p-12 text-side-fg lg:flex lg:flex-col lg:justify-between">
      <svg className="pointer-events-none absolute inset-0 size-full" viewBox="0 0 600 800" preserveAspectRatio="xMidYMid slice" aria-hidden>
        <defs>
          <linearGradient id="gt-route" x1="0" y1="0" x2="1" y2="1">
            <stop offset="0" stopColor="var(--primary)" stopOpacity="0.15" />
            <stop offset="1" stopColor="var(--primary)" />
          </linearGradient>
          <radialGradient id="gt-glow" cx="0.75" cy="0.2" r="0.7">
            <stop offset="0" stopColor="var(--primary)" stopOpacity="0.28" />
            <stop offset="1" stopColor="var(--primary)" stopOpacity="0" />
          </radialGradient>
        </defs>
        <rect width="600" height="800" fill="url(#gt-glow)" />
        <g stroke="currentColor" strokeOpacity="0.06" fill="none">
          {Array.from({ length: 14 }, (_, i) => <path key={`h${i}`} d={`M0 ${i * 60}H600`} />)}
          {Array.from({ length: 11 }, (_, i) => <path key={`v${i}`} d={`M${i * 60} 0V800`} />)}
        </g>
        <path d="M455 730C575 660 575 560 525 500S405 440 470 330 585 215 505 100" fill="none" stroke="url(#gt-route)" strokeWidth="5" strokeLinecap="round" strokeDasharray="1 14" />
        <path d="M455 730C575 660 575 560 525 500S405 440 470 330 585 215 505 100" fill="none" stroke="url(#gt-route)" strokeWidth="2.5" strokeLinecap="round" strokeOpacity="0.55" />
        {[[455, 730], [470, 330], [505, 100]].map(([x, y], i) => (
          <g key={i}>
            <circle cx={x} cy={y} r="22" fill="var(--primary)" fillOpacity="0.14" />
            <circle cx={x} cy={y} r="8" fill="var(--primary)" />
            <circle cx={x} cy={y} r="3" fill="var(--side)" />
          </g>
        ))}
      </svg>
      <div className="relative flex items-center gap-2.5 text-[15px] font-semibold tracking-tight">
        <Logo className="size-9" />
        GeoTracker
      </div>
      <div className="relative max-w-md">
        <h2 className="text-4xl leading-[1.1] font-semibold tracking-tight">{t("auth.heroTitle")}</h2>
        <p className="mt-4 text-base text-side-muted">{t("auth.heroBody")}</p>
        <ul className="mt-8 space-y-3 text-sm">
          {features.map((f) => (
            <li key={f} className="flex items-center gap-3">
              <span className="grid size-6 shrink-0 place-items-center rounded-full bg-primary/15 text-primary"><Check className="size-3.5" aria-hidden /></span>
              {f}
            </li>
          ))}
        </ul>
      </div>
      <p className="relative text-xs text-side-muted">{t("auth.privacy")}</p>
    </aside>
  );
}

function useSubmit(fn: () => Promise<User>, onDone: () => void) {
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const u = await fn();
      queryClient.setQueryData(["me"], u);
      onDone();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };
  return { error, busy, submit };
}

/** Same-site paths only: "//host", "/\host" and control characters (tabs get stripped by browsers) could leave the app. */
function safeNext(raw: string | null): string {
  if (!raw || !raw.startsWith("/") || raw.startsWith("//")) return "/";
  for (const ch of raw) if (ch.charCodeAt(0) < 32 || ch === "\\") return "/";
  return raw;
}

export function Login() {
  const { t } = useTranslation();
  const me = useMe();
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const next = safeNext(params.get("next"));
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const { error, busy, submit } = useSubmit(() => api<User>("/auth/login", { method: "POST", body: { email, password } }), () => navigate(next, { replace: true }));
  const methods = useQuery({ queryKey: ["auth-methods"], queryFn: () => api<AuthMethods>("/auth/methods") });
  const ssoError = params.get("error");

  if (me.data) return <Navigate to={next} replace />;
  return (
    <AuthLayout title={t("auth.welcomeBack")} subtitle={t("auth.signInSubtitle")}>
      {methods.data?.oidc && (
        <>
          <a href={`/api/v1/auth/oidc/login?next=${encodeURIComponent(next)}`} className="block">
            <Button variant="primary" icon={KeyRound} className="w-full justify-center" tabIndex={-1}>
              {methods.data.oidc_label || t("auth.sso")}
            </Button>
          </a>
          <div className="my-5 flex items-center gap-3 text-xs text-subtle" aria-hidden>
            <span className="h-px flex-1 bg-border" />
            {t("auth.orPassword")}
            <span className="h-px flex-1 bg-border" />
          </div>
        </>
      )}
      <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
        {(error || ssoError) && <Notice tone="danger">{error || ssoError}</Notice>}
        <Field label={t("auth.email")}>{(id) => <Input id={id} type="email" autoComplete="username" required autoFocus value={email} onChange={(e) => setEmail(e.target.value)} />}</Field>
        <Field label={t("auth.password")} hint={t("auth.forgotHint")}>
          {(id, d) => <Input id={id} aria-describedby={d} type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />}
        </Field>
        <Button variant={methods.data?.oidc ? "secondary" : "primary"} type="submit" loading={busy} className="mt-2 justify-center">
          {t("auth.signIn")}
        </Button>
      </form>
    </AuthLayout>
  );
}

export function Setup() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const setup = useQuery({ queryKey: ["setup"], queryFn: () => api<{ needs_setup: boolean }>("/setup") });
  const [form, setForm] = useState({ name: "", email: "", password: "" });
  const { error, busy, submit } = useSubmit(
    () => api<User>("/setup", { method: "POST", body: form }),
    // Don't touch the ["setup"] cache here: flipping it re-renders this page into <Navigate to="/"> first.
    () => navigate("/settings/devices?welcome=1", { replace: true }),
  );

  if (setup.isPending) return <Spinner />;
  if (!setup.data?.needs_setup) return <Navigate to="/" replace />;
  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement>) => setForm({ ...form, [k]: e.target.value });
  return (
    <AuthLayout title={t("setup.title")} subtitle={t("setup.subtitle")}>
      <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
        {error && <Notice tone="danger">{error}</Notice>}
        <Field label={t("setup.name")}>{(id) => <Input id={id} autoComplete="name" required autoFocus value={form.name} onChange={set("name")} />}</Field>
        <Field label={t("auth.email")}>{(id) => <Input id={id} type="email" autoComplete="email" required value={form.email} onChange={set("email")} />}</Field>
        <Field label={t("auth.password")} hint={t("setup.passwordHint")}>
          {(id, d) => <Input id={id} aria-describedby={d} type="password" autoComplete="new-password" required minLength={10} value={form.password} onChange={set("password")} />}
        </Field>
        <Button variant="primary" type="submit" loading={busy} className="mt-2 justify-center">
          {t("setup.create")}
        </Button>
        <p className="text-center text-xs text-subtle">{t("setup.adminNote")}</p>
      </form>
    </AuthLayout>
  );
}

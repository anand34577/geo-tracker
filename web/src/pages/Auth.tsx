import { useState, type FormEvent, type ReactNode } from "react";
import { Navigate, useNavigate, useSearchParams } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { KeyRound, Lock } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, queryClient, useMe, type AuthMethods, type User } from "../lib/api";
import { Button, Field, Input, Notice, Spinner } from "../components/ui";
import { Logo } from "../components/Logo";

function AuthLayout({ title, subtitle, children }: { title: string; subtitle: string; children: ReactNode }) {
  const { t } = useTranslation();
  return (
    <main className="grid min-h-dvh place-items-center bg-bg px-4 py-10">
      <div className="w-full max-w-sm">
        <div className="mb-8 flex flex-col items-center text-center">
          <Logo className="mb-5 size-12" />
          <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
          <p className="mt-2 text-sm text-muted">{subtitle}</p>
        </div>
        <div className="rounded-2xl border border-border bg-surface p-6 shadow-pop">{children}</div>
        <p className="mt-6 flex items-center justify-center gap-1.5 text-xs text-subtle">
          <Lock className="size-3" aria-hidden />
          {t("auth.privacy")}
        </p>
      </div>
    </main>
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

export function Login() {
  const { t } = useTranslation();
  const me = useMe();
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const next = params.get("next")?.startsWith("/") ? params.get("next")! : "/";
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

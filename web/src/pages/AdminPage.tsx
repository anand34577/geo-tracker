import { useEffect, useRef, useState } from "react";
import { Navigate, useParams } from "react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { ScrollText, Activity, ArchiveRestore, DatabaseBackup, Download, Plus, Settings2, Trash2, Upload, Users } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, queryClient, upload, useUser, type AuditEntry, type Backup, type Settings, type System, type User } from "../lib/api";
import { bytes, dateTime, number } from "../lib/format";
import { usePrefs } from "../lib/prefs";
import { TabLayout } from "../components/Tabs";
import { Button, ConfirmDialog, Dialog, ErrorState, Field, Input, Notice, Section, Skeleton, Spinner, cn, useToast } from "../components/ui";
import { Checkbox, Select, Stepper, Switch } from "../components/controls";

export default function AdminPage() {
  const { t } = useTranslation();
  const user = useUser();
  const tab = useParams().tab ?? "users";
  if (user.role !== "admin") return <Navigate to="/" replace />;
  const tabs = [
    { id: "users", label: t("admin.users"), icon: Users },
    { id: "system", label: t("admin.system"), icon: Activity },
    { id: "settings", label: t("admin.settings"), icon: Settings2 },
    { id: "audit", label: t("admin.audit"), icon: ScrollText },
  ];
  return (
    <TabLayout title={t("nav.admin")} base="/admin" tabs={tabs}>
      {tab === "users" && <UsersTab />}
      {tab === "system" && <SystemTab />}
      {tab === "settings" && <InstanceSettings />}
      {tab === "audit" && <AuditTab />}
    </TabLayout>
  );
}

// ── Users ────────────────────────────────────────────────────

type UserForm = { id?: number; name: string; email: string; password: string; role: "admin" | "user"; disabled: boolean };
const blank: UserForm = { name: "", email: "", password: "", role: "user", disabled: false };

function UsersTab() {
  const { t } = useTranslation();
  const toast = useToast();
  const me = useUser();
  const users = useQuery({ queryKey: ["users"], queryFn: () => api<User[]>("/admin/users") });
  const [form, setForm] = useState<UserForm | null>(null);
  const [deleting, setDeleting] = useState<User | null>(null);

  const save = useMutation({
    meta: { inline: true }, // error shown in the form
    mutationFn: (f: UserForm) =>
      f.id ? api(`/admin/users/${f.id}`, { method: "PATCH", body: f }) : api("/admin/users", { method: "POST", body: f }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["users"] });
      toast("success", t("settings.saved"));
      setForm(null);
    },
  });
  const del = useMutation({
    mutationFn: (u: User) => api(`/admin/users/${u.id}`, { method: "DELETE" }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["users"] });
      toast("success", t("admin.userDeleted"));
      setDeleting(null);
    },
    onError: (e) => toast("error", e.message),
  });

  return (
    <Section title={t("admin.users")} description={t("admin.usersDesc")} actions={<Button variant="primary" size="sm" icon={Plus} onClick={() => { save.reset(); setForm(blank); }}>{t("admin.addUser")}</Button>}>
      {users.isError ? (
        <ErrorState error={users.error} retry={() => users.refetch()} />
      ) : users.isPending ? (
        <Skeleton className="h-24" />
      ) : (
        <ul className="-my-2 divide-y divide-border">
          {users.data.map((u) => (
            <li key={u.id} className="flex flex-wrap items-center gap-3 py-3">
              <span className="grid size-9 place-items-center rounded-full bg-surface-3 text-sm font-semibold" aria-hidden>{u.name.slice(0, 1).toUpperCase()}</span>
              <div className="min-w-0 flex-1">
                <p className="truncate font-medium">
                  {u.name} {u.id === me.id && <span className="text-sm font-normal text-subtle">({t("admin.you")})</span>}
                </p>
                <p className="truncate text-sm text-muted">{u.email}</p>
              </div>
              <span className={cn("rounded-full px-2.5 py-0.5 text-xs font-medium", u.disabled ? "bg-danger-subtle text-danger" : u.role === "admin" ? "bg-primary-subtle text-primary" : "bg-surface-2 text-muted")}>
                {u.disabled ? t("admin.disabled") : t(`admin.role.${u.role}`)}
              </span>
              <Button size="sm" variant="ghost" onClick={() => { save.reset(); setForm({ ...u, password: "" }); }}>{t("common.edit")}</Button>
              {u.id !== me.id && (
                <Button size="icon" variant="ghost" aria-label={t("common.delete")} onClick={() => setDeleting(u)}>
                  <Trash2 className="size-4" />
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}

      <Dialog open={!!form} onClose={() => setForm(null)} title={form?.id ? t("admin.editUser") : t("admin.addUser")}
        footer={
          <>
            <Button variant="ghost" onClick={() => setForm(null)}>{t("common.cancel")}</Button>
            <Button variant="primary" loading={save.isPending} onClick={() => form && save.mutate(form)}>{t("common.save")}</Button>
          </>
        }>
        {form && (
          <form className="grid gap-4" onSubmit={(e) => { e.preventDefault(); save.mutate(form); }}>
            {save.error && <Notice tone="danger">{save.error.message}</Notice>}
            <Field label={t("setup.name")}>{(id) => <Input id={id} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />}</Field>
            <Field label={t("auth.email")}>{(id) => <Input id={id} type="email" value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} />}</Field>
            <Field label={form.id ? t("admin.newPassword") : t("auth.password")} hint={form.id ? t("admin.passwordKeep") : t("setup.passwordHint")}>
              {(id, d) => <Input id={id} aria-describedby={d} type="password" autoComplete="new-password" value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} />}
            </Field>
            <Field label={t("admin.roleLabel")}>
              {(id) => (
                <Select id={id} label={t("admin.roleLabel")} value={form.role} onChange={(role) => setForm({ ...form, role })} options={[
                  { value: "user", label: t("admin.role.user"), hint: t("admin.roleUserHint") },
                  { value: "admin", label: t("admin.role.admin"), hint: t("admin.roleAdminHint") },
                ]} />
              )}
            </Field>
            {form.id && form.id !== me.id && (
              <Checkbox checked={form.disabled} onChange={(disabled) => setForm({ ...form, disabled })} label={t("admin.disableAccount")} />
            )}
            <button type="submit" hidden />
          </form>
        )}
      </Dialog>
      <ConfirmDialog open={!!deleting} onClose={() => setDeleting(null)} onConfirm={() => deleting && del.mutate(deleting)} loading={del.isPending} typeToConfirm="delete"
        title={t("admin.deleteUserTitle", { name: deleting?.name })} body={t("admin.deleteUserBody")} confirmLabel={t("common.delete")} />
    </Section>
  );
}

// ── System & backups ─────────────────────────────────────────

function Restarting() {
  const { t } = useTranslation();
  const [slow, setSlow] = useState(false);
  useEffect(() => {
    const start = Date.now();
    const iv = setInterval(async () => {
      if (Date.now() - start > 30_000) setSlow(true);
      try {
        if ((await fetch("/healthz", { cache: "no-store" })).ok && Date.now() - start > 2500) location.replace("/");
      } catch {
        /* still down */
      }
    }, 1500);
    return () => clearInterval(iv);
  }, []);
  return (
    <div className="fixed inset-0 z-[600] grid place-items-center bg-bg/95 p-6 text-center">
      <div className="max-w-sm">
        <Spinner label={t("admin.restarting")} />
        {slow && <p className="text-sm text-muted">{t("admin.restartSlow")}</p>}
      </div>
    </div>
  );
}

function SystemTab() {
  const { t } = useTranslation();
  const toast = useToast();
  const { clock } = usePrefs();
  const sys = useQuery({ queryKey: ["system"], queryFn: () => api<System>("/admin/system") });
  const backups = useQuery({ queryKey: ["backups"], queryFn: () => api<Backup[]>("/admin/backups") });
  const [restoring, setRestoring] = useState<Backup | File | null>(null);
  const [restarting, setRestarting] = useState(false);
  const [deleting, setDeleting] = useState<Backup | null>(null);
  const fileInput = useRef<HTMLInputElement>(null);

  const create = useMutation({
    mutationFn: () => api<{ name: string }>("/admin/backups", { method: "POST" }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["backups"] });
      queryClient.invalidateQueries({ queryKey: ["system"] });
      toast("success", t("admin.backupCreated"));
    },
    onError: (e) => toast("error", e.message),
  });
  const restore = useMutation({
    mutationFn: (target: Backup | File) =>
      target instanceof File ? upload("/admin/backups/upload", target, () => {}) : api(`/admin/backups/${encodeURIComponent(target.name)}/restore`, { method: "POST" }),
    onSuccess: () => setRestarting(true),
    onError: (e) => {
      toast("error", e.message);
      setRestoring(null);
    },
  });
  const del = useMutation({
    mutationFn: (b: Backup) => api(`/admin/backups/${encodeURIComponent(b.name)}`, { method: "DELETE" }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["backups"] });
      setDeleting(null);
    },
  });

  if (restarting) return <Restarting />;
  const s = sys.data;
  return (
    <>
      <Section title={t("admin.system")}>
        {sys.isError ? (
          <ErrorState error={sys.error} retry={() => sys.refetch()} />
        ) : !s ? (
          <Skeleton className="h-32" />
        ) : (
          <div className="space-y-4">
            {!s.https && <Notice tone="warning">{t("admin.httpsWarning")}</Notice>}
            {!s.last_backup && <Notice tone="warning">{t("admin.noBackupWarning")}</Notice>}
            <dl className="grid grid-cols-2 gap-x-6 gap-y-4 text-sm sm:grid-cols-3">
              <Info label={t("admin.version")} value={s.version} />
              <Info label={t("admin.runningSince")} value={dateTime(s.started, clock)} />
              <Info label={t("admin.dbSize")} value={bytes(s.db_size)} />
              <Info label={t("admin.usersCount")} value={number(s.counts.users)} />
              <Info label={t("admin.pointsCount")} value={number(s.counts.points)} />
              <Info label={t("admin.visitsCount")} value={number(s.counts.visits)} />
              <Info label={t("admin.baseUrl")} value={s.base_url} />
              <Info label={t("admin.dataDir")} value={s.data_dir} />
              <Info label={t("admin.runtime")} value={`${s.go} · ${s.os}`} />
            </dl>
          </div>
        )}
      </Section>

      <Section title={t("admin.backups")} description={t("admin.backupsDesc")}
        actions={
          <div className="flex gap-2">
            <input ref={fileInput} type="file" accept=".gz,.db" className="sr-only" onChange={(e) => e.target.files?.[0] && setRestoring(e.target.files[0])} />
            <Button size="sm" icon={Upload} onClick={() => fileInput.current?.click()}>{t("admin.restoreFile")}</Button>
            <Button size="sm" variant="primary" icon={DatabaseBackup} loading={create.isPending} onClick={() => create.mutate()}>{t("admin.backupNow")}</Button>
          </div>
        }>
        {backups.isPending ? (
          <Skeleton className="h-16" />
        ) : !backups.data?.length ? (
          <p className="text-sm text-muted">{t("admin.noBackups")}</p>
        ) : (
          <ul className="-my-2 divide-y divide-border">
            {backups.data.map((b) => (
              <li key={b.name} className="flex flex-wrap items-center gap-2 py-3">
                <div className="min-w-0 flex-1">
                  <p className="truncate font-mono text-sm">{b.name}</p>
                  <p className="text-xs text-muted">{dateTime(b.created, clock)} · {bytes(b.size)}</p>
                </div>
                <a href={`/api/v1/admin/backups/${encodeURIComponent(b.name)}`} download aria-label={t("admin.download")}>
                  <Button size="icon" variant="ghost" tabIndex={-1}><Download className="size-4" /></Button>
                </a>
                <Button size="sm" variant="ghost" icon={ArchiveRestore} onClick={() => setRestoring(b)}>{t("admin.restore")}</Button>
                <Button size="icon" variant="ghost" aria-label={t("common.delete")} onClick={() => setDeleting(b)}><Trash2 className="size-4" /></Button>
              </li>
            ))}
          </ul>
        )}
      </Section>

      <ConfirmDialog open={!!restoring} onClose={() => setRestoring(null)} onConfirm={() => restoring && restore.mutate(restoring)} loading={restore.isPending} typeToConfirm="restore"
        title={t("admin.restoreTitle")} body={t("admin.restoreBody", { name: restoring?.name })} confirmLabel={t("admin.restore")} />
      <ConfirmDialog open={!!deleting} onClose={() => setDeleting(null)} onConfirm={() => deleting && del.mutate(deleting)} loading={del.isPending}
        title={t("admin.deleteBackupTitle")} body={deleting?.name} confirmLabel={t("common.delete")} />
    </>
  );
}

function Info({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <dt className="text-subtle">{label}</dt>
      <dd className="mt-0.5 truncate font-medium" title={value}>{value}</dd>
    </div>
  );
}

// ── Instance settings ────────────────────────────────────────

function InstanceSettings() {
  const { t } = useTranslation();
  const toast = useToast();
  const settings = useQuery({ queryKey: ["settings"], queryFn: () => api<Settings>("/admin/settings") });
  const [form, setForm] = useState<Settings | null>(null);
  useEffect(() => {
    if (settings.data) setForm(settings.data);
  }, [settings.data]);
  const save = useMutation({
    meta: { inline: true }, // error shown in the form
    mutationFn: (s: Settings) => {
      const { oidc_callback_url: _cb, map_mbtiles_error: _err, ...body } = s;
      return api<Settings>("/admin/settings", { method: "PUT", body });
    },
    onSuccess: (s) => {
      queryClient.setQueryData(["settings"], s);
      queryClient.invalidateQueries({ queryKey: ["config"] });
      toast("success", t("settings.saved"));
    },
  });
  const testMail = useMutation({
    mutationFn: async () => {
      await save.mutateAsync(form!);
      return api("/me/notifications/test?channel=email", { method: "POST" });
    },
    onSuccess: () => toast("success", t("admin.testMailSent")),
    onError: (e) => toast("error", e.message),
  });
  if (settings.isError) return <ErrorState error={settings.error} retry={() => settings.refetch()} />;
  if (!form) return <Skeleton className="h-64" />;
  const set = (k: string) => (e: React.ChangeEvent<HTMLInputElement>) => setForm({ ...form, [k]: e.target.value });
  const val = (k: string) => (v: string) => setForm({ ...form, [k]: v });
  const flag = (k: string) => form[k] === "true";

  const basemapOptions = [
    { value: "auto", label: t("admin.bm.auto") },
    { value: "liberty", label: "OpenFreeMap Liberty" },
    { value: "bright", label: "OpenFreeMap Bright" },
    { value: "osm", label: "OpenStreetMap" },
    { value: "topo", label: "OpenTopoMap" },
    ...(form.map_mbtiles ? [{ value: "offline", label: t("admin.bm.offline") }] : []),
  ];

  return (
    <form onSubmit={(e) => { e.preventDefault(); save.mutate(form); }} className="space-y-6">
      {save.error && <Notice tone="danger">{save.error.message}</Notice>}

      <Section title={t("admin.maps")} description={t("admin.mapsDesc")}>
        <div className="grid gap-5">
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={t("admin.defaultBasemap")} hint={t("admin.defaultBasemapHint")}>
              {(id) => <Select id={id} label={t("admin.defaultBasemap")} value={form.map_default} onChange={val("map_default")} options={basemapOptions} />}
            </Field>
          </div>
          <div className="rounded-xl border border-border p-4">
            <p className="font-medium">{t("admin.offlineTitle")}</p>
            <p className="mt-1 text-sm text-muted">{t("admin.offlineDesc")}</p>
            <div className="mt-4 grid gap-4">
              <Field label={t("admin.mbtilesPath")} hint={t("admin.mbtilesHint")} error={form.map_mbtiles && settings.data?.map_mbtiles === form.map_mbtiles ? settings.data?.map_mbtiles_error : undefined}>
                {(id, d) => <Input id={id} aria-describedby={d} value={form.map_mbtiles} placeholder="/data/maps/india.mbtiles" onChange={set("map_mbtiles")} className="font-mono text-xs" />}
              </Field>
              <Field label={t("admin.glyphs")} hint={t("admin.glyphsHint")}>
                {(id, d) => <Input id={id} aria-describedby={d} value={form.map_glyphs} onChange={set("map_glyphs")} className="font-mono text-xs" />}
              </Field>
            </div>
          </div>
          <details className="group">
            <summary className="cursor-pointer text-sm font-medium text-primary">{t("admin.customStyles")}</summary>
            <div className="mt-4 grid gap-4">
              <Field label={t("admin.styleLight")}>{(id) => <Input id={id} value={form.map_style_light} onChange={set("map_style_light")} className="font-mono text-xs" />}</Field>
              <Field label={t("admin.styleDark")}>{(id) => <Input id={id} value={form.map_style_dark} onChange={set("map_style_dark")} className="font-mono text-xs" />}</Field>
            </div>
          </details>
        </div>
      </Section>

      <Section title={t("admin.geocoding")} description={t("admin.geocodingDesc")}>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={t("admin.provider")}>
            {(id) => (
              <Select id={id} label={t("admin.provider")} value={form.geocoder} onChange={val("geocoder")} options={[
                { value: "nominatim", label: "Nominatim", hint: "OpenStreetMap" },
                { value: "photon", label: "Photon", hint: "Komoot, OpenStreetMap data" },
                { value: "none", label: t("admin.geocoderOff") },
              ]} />
            )}
          </Field>
          <Field label={t("admin.geocoderUrl")} hint={t("admin.geocoderUrlHint")}>
            {(id, d) => <Input id={id} aria-describedby={d} value={form.geocoder_url} disabled={form.geocoder === "none"} onChange={set("geocoder_url")} />}
          </Field>
        </div>
      </Section>

      <Section title={t("admin.smtp")} description={t("admin.smtpDesc")}>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={t("admin.smtpHost")}>{(id) => <Input id={id} value={form.smtp_host} placeholder="smtp.example.com" onChange={set("smtp_host")} />}</Field>
          <div className="grid grid-cols-[auto_1fr] gap-4">
            <Field label={t("admin.smtpPort")}>{(id) => <Stepper id={id} label={t("admin.smtpPort")} min={1} max={65535} value={Number(form.smtp_port) || 587} onChange={(v) => setForm({ ...form, smtp_port: String(v) })} />}</Field>
            <Field label={t("admin.smtpSecurity")}>
              {(id) => (
                <Select id={id} label={t("admin.smtpSecurity")} value={form.smtp_security} onChange={val("smtp_security")} options={[
                  { value: "starttls", label: "STARTTLS", hint: t("admin.port587") },
                  { value: "tls", label: "TLS", hint: t("admin.port465") },
                  { value: "none", label: t("admin.noEncryption"), hint: t("admin.localOnly") },
                ]} />
              )}
            </Field>
          </div>
          <Field label={t("admin.smtpUser")}>{(id) => <Input id={id} value={form.smtp_user} autoComplete="off" onChange={set("smtp_user")} />}</Field>
          <Field label={t("admin.smtpPassword")}>{(id) => <Input id={id} type="password" value={form.smtp_password} autoComplete="new-password" onChange={set("smtp_password")} />}</Field>
          <Field label={t("admin.smtpFrom")} hint={t("admin.smtpFromHint")}>{(id, d) => <Input id={id} aria-describedby={d} value={form.smtp_from} placeholder="GeoTracker <geotracker@example.com>" onChange={set("smtp_from")} />}</Field>
          <div className="flex items-end">
            <Button type="button" onClick={() => testMail.mutate()} loading={testMail.isPending} disabled={!form.smtp_host || !form.smtp_from}>{t("admin.sendTestMail")}</Button>
          </div>
        </div>
      </Section>

      <Section title={t("admin.sso")} description={t("admin.ssoDesc")} actions={<Switch label={t("admin.ssoEnable")} checked={flag("oidc_enabled")} onChange={(v) => setForm({ ...form, oidc_enabled: String(v) })} />}>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={t("admin.ssoIssuer")} hint={t("admin.ssoIssuerHint")}>{(id, d) => <Input id={id} aria-describedby={d} value={form.oidc_issuer} placeholder="https://auth.example.com" onChange={set("oidc_issuer")} />}</Field>
          <Field label={t("admin.ssoLabel")}>{(id) => <Input id={id} value={form.oidc_label} onChange={set("oidc_label")} />}</Field>
          <Field label={t("admin.ssoClientId")}>{(id) => <Input id={id} value={form.oidc_client_id} autoComplete="off" onChange={set("oidc_client_id")} />}</Field>
          <Field label={t("admin.ssoClientSecret")}>{(id) => <Input id={id} type="password" value={form.oidc_client_secret} autoComplete="new-password" onChange={set("oidc_client_secret")} />}</Field>
          <div className="sm:col-span-2">
            <Field label={t("admin.ssoCallback")} hint={t("admin.ssoCallbackHint")}>
              {(id, d) => <Input id={id} aria-describedby={d} readOnly value={settings.data?.oidc_callback_url ?? ""} className="font-mono text-xs" onFocus={(e) => e.target.select()} />}
            </Field>
          </div>
          <div className="sm:col-span-2">
            <Checkbox checked={flag("oidc_auto_register")} onChange={(v) => setForm({ ...form, oidc_auto_register: String(v) })} label={t("admin.ssoAutoRegister")} description={t("admin.ssoAutoRegisterHint")} />
          </div>
        </div>
      </Section>

      <Section title={t("admin.backups")} description={t("admin.scheduleDesc")}>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={t("admin.backupHour")}>
            {(id) => (
              <Select id={id} label={t("admin.backupHour")} value={form.backup_hour} onChange={val("backup_hour")}
                options={[{ value: "-1", label: t("admin.backupOff") }, ...Array.from({ length: 24 }, (_, h) => ({ value: String(h), label: `${String(h).padStart(2, "0")}:00` }))]} />
            )}
          </Field>
          <Field label={t("admin.backupKeep")}>{(id) => <Stepper id={id} label={t("admin.backupKeep")} min={1} max={365} value={Number(form.backup_keep) || 7} onChange={(v) => setForm({ ...form, backup_keep: String(v) })} />}</Field>
        </div>
      </Section>

      <div className="sticky bottom-0 -mx-1 flex justify-end bg-gradient-to-t from-bg via-bg/90 to-transparent px-1 py-3">
        <Button type="submit" variant="primary" loading={save.isPending}>{t("common.save")}</Button>
      </div>
    </form>
  );
}

// ── Audit log ────────────────────────────────────────────────

function AuditTab() {
  const { t } = useTranslation();
  const { clock } = usePrefs();
  const [pages, setPages] = useState<AuditEntry[][]>([]);
  const first = useQuery({ queryKey: ["audit"], queryFn: () => api<AuditEntry[]>("/admin/audit") });
  const more = useMutation({
    mutationFn: (before: number) => api<AuditEntry[]>(`/admin/audit?before=${before}`),
    onSuccess: (rows) => setPages((p) => [...p, rows]),
  });
  const rows = [...(first.data ?? []), ...pages.flat()];
  const last = rows.at(-1);
  const exhausted = (pages.at(-1) ?? first.data ?? []).length < 100;
  const tone = (a: string) => (a.endsWith("failed") ? "text-danger" : a.includes("delete") || a.includes("restore") || a.includes("revoke") ? "text-warning" : "text-fg");
  return (
    <Section title={t("admin.audit")} description={t("admin.auditDesc")}>
      {first.isError ? (
        <ErrorState error={first.error} retry={() => first.refetch()} />
      ) : first.isPending ? (
        <Skeleton className="h-40" />
      ) : rows.length === 0 ? (
        <p className="text-sm text-muted">{t("admin.auditEmpty")}</p>
      ) : (
        <>
          <ol className="-my-2 divide-y divide-border">
            {rows.map((e) => (
              <li key={e.id} className="grid gap-x-4 gap-y-0.5 py-2.5 text-sm sm:grid-cols-[10rem_1fr_auto]">
                <time className="text-xs text-subtle tabular-nums sm:text-sm">{dateTime(e.ts, clock)}</time>
                <span className="min-w-0">
                  <span className="font-medium">{e.actor}</span>{" "}
                  <span className={cn("font-mono text-xs", tone(e.action))}>{e.action}</span>
                  {e.target && <span className="text-muted"> · {e.target}</span>}
                </span>
                <span className="font-mono text-xs text-subtle">{e.ip}</span>
              </li>
            ))}
          </ol>
          {!exhausted && last && (
            <div className="mt-4 flex justify-center">
              <Button size="sm" loading={more.isPending} onClick={() => more.mutate(last.id)}>{t("admin.loadMore")}</Button>
            </div>
          )}
        </>
      )}
    </Section>
  );
}
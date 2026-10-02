import { useState } from "react";
import { Link } from "react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Bell, BellRing, Hash, Mail, MessageSquare, Pencil, Play, Plus, Send, Trash2, Users, Webhook, Zap, type LucideIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, queryClient, type AutoAction, type Automation } from "../lib/api";
import { useFamily, usePlaces } from "../lib/data";
import { ago } from "../lib/format";
import { Checkbox, Select, Stepper, Switch } from "../components/controls";
import { Button, ConfirmDialog, Dialog, EmptyState, ErrorState, Field, Input, Notice, PageHeader, Skeleton, cn, useToast } from "../components/ui";

type ActionType = AutoAction["type"];
const actionTypes: { type: ActionType; icon: LucideIcon }[] = [
  { type: "notify_family", icon: Users },
  { type: "notify_me", icon: Bell },
  { type: "webhook", icon: Webhook },
  { type: "ntfy", icon: BellRing },
  { type: "telegram", icon: Send },
  { type: "discord", icon: MessageSquare },
  { type: "slack", icon: Hash },
  { type: "email", icon: Mail },
];
const iconOf = (type: ActionType) => actionTypes.find((a) => a.type === type)?.icon ?? Zap;
const variables = ["user", "place", "verb", "event", "time", "date", "lat", "lon", "map_url"];

const blank = (type: ActionType): AutoAction => ({ id: "", type, ...(type === "webhook" ? { method: "POST" } : {}), ...(type === "notify_family" ? { members: [] } : {}) });
const emptyForm = (placeId: string, first: ActionType): Form => ({ id: 0, name: "", place_id: placeId, on_arrive: true, on_leave: false, enabled: true, cooldown_min: 5, actions: [blank(first)] });
type Form = { id: number; name: string; place_id: string; on_arrive: boolean; on_leave: boolean; enabled: boolean; cooldown_min: number; actions: AutoAction[] };

const textarea =
  "w-full rounded-lg border border-border-strong bg-surface px-3 py-2 text-sm text-fg placeholder:text-subtle transition-colors focus:border-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-primary/30";

const toBody = (f: Form) => ({ name: f.name, place_id: Number(f.place_id), on_arrive: f.on_arrive, on_leave: f.on_leave, enabled: f.enabled, cooldown_min: f.cooldown_min, actions: f.actions });
const toForm = (a: Automation): Form => ({ id: a.id, name: a.name, place_id: String(a.place_id), on_arrive: a.on_arrive, on_leave: a.on_leave, enabled: a.enabled, cooldown_min: a.cooldown_min, actions: a.actions });

export default function AutomationsPage() {
  const { t } = useTranslation();
  const toast = useToast();
  const list = useQuery({ queryKey: ["automations"], queryFn: () => api<Automation[]>("/automations") });
  const places = usePlaces();
  const [form, setForm] = useState<Form | null>(null);
  const [deleting, setDeleting] = useState<Automation | null>(null);
  const refresh = () => queryClient.invalidateQueries({ queryKey: ["automations"] });

  const save = useMutation({
    mutationFn: (f: Form) => api<Automation>(f.id ? `/automations/${f.id}` : "/automations", { method: f.id ? "PUT" : "POST", body: toBody(f) }),
    onSuccess: () => {
      refresh();
      setForm(null);
      toast("success", t("auto.saved"));
    },
  });
  const toggle = useMutation({
    mutationFn: (a: Automation) => api(`/automations/${a.id}`, { method: "PUT", body: toBody({ ...toForm(a), enabled: !a.enabled }) }),
    onSuccess: refresh,
    onError: (e) => toast("error", e.message),
  });
  const remove = useMutation({
    mutationFn: (a: Automation) => api(`/automations/${a.id}`, { method: "DELETE" }),
    onSuccess: () => {
      refresh();
      setDeleting(null);
      toast("success", t("auto.deleted"));
    },
  });
  const test = useMutation({
    mutationFn: (a: Automation) => api<{ results: { type: string; ok: boolean; error?: string }[] }>(`/automations/${a.id}/test`, { method: "POST" }),
    onSuccess: ({ results }) => {
      const bad = results.filter((r) => !r.ok);
      if (bad.length === 0) toast("success", t("auto.testOk", { count: results.length }));
      else toast("error", bad.map((r) => `${t(`auto.type.${r.type}`)}: ${r.error}`).join(" · "));
    },
    onError: (e) => toast("error", e.message),
  });

  const noPlaces = places.isSuccess && places.data.length === 0;
  const family = useFamily();
  // Start with the most useful action: tell family when there is one, otherwise notify yourself.
  const create = () => setForm(emptyForm(places.data?.[0] ? String(places.data[0].id) : "", family.data?.length ? "notify_family" : "notify_me"));
  const newButton = <Button variant="primary" icon={Plus} onClick={create} disabled={noPlaces}>{t("auto.new")}</Button>;

  return (
    <div className="mx-auto w-full max-w-4xl px-4 py-6 md:px-8 md:py-10">
      <PageHeader title={t("auto.title")} description={t("auto.subtitle")} actions={newButton} />
      {list.isError ? (
        <ErrorState error={list.error} retry={() => list.refetch()} />
      ) : list.isPending ? (
        <Skeleton className="h-40" />
      ) : list.data.length === 0 ? (
        <div className="rounded-2xl border border-dashed border-border-strong">
          <EmptyState icon={Zap} title={t("auto.emptyTitle")} body={noPlaces ? t("auto.needPlace") : t("auto.emptyBody")}
            action={noPlaces ? <Link to="/places" className="text-sm font-medium text-primary">{t("auto.addPlace")}</Link> : newButton} />
        </div>
      ) : (
        <ul className="divide-y divide-border rounded-2xl border border-border bg-surface">
          {list.data.map((a) => (
            <li key={a.id} className="space-y-3 p-4">
              <div className="flex flex-wrap items-center gap-3">
                <span className={cn("grid size-10 shrink-0 place-items-center rounded-xl", a.enabled ? "bg-primary-subtle text-primary" : "bg-surface-2 text-subtle")}><Zap className="size-5" aria-hidden /></span>
                <div className="min-w-0 flex-1">
                  <p className="truncate font-medium">{a.name}</p>
                  <p className="text-sm text-muted">
                    {a.on_arrive && a.on_leave ? t("auto.whenBoth", { place: a.place_name }) : a.on_arrive ? t("auto.whenArrive", { place: a.place_name }) : t("auto.whenLeave", { place: a.place_name })}
                  </p>
                </div>
                <Switch label={t("auto.enabled")} checked={a.enabled} onChange={() => toggle.mutate(a)} />
                <Button variant="ghost" size="icon" aria-label={t("auto.test")} title={t("auto.test")} loading={test.isPending && test.variables?.id === a.id} onClick={() => test.mutate(a)}><Play className="size-4" /></Button>
                <Button variant="ghost" size="icon" aria-label={t("common.edit")} title={t("common.edit")} onClick={() => setForm(toForm(a))}><Pencil className="size-4" /></Button>
                <Button variant="ghost" size="icon" aria-label={t("common.delete")} title={t("common.delete")} onClick={() => setDeleting(a)}><Trash2 className="size-4" /></Button>
              </div>
              <div className="flex flex-wrap items-center gap-2 pl-[3.25rem] text-xs">
                {a.actions.map((x) => {
                  const I = iconOf(x.type);
                  return <span key={x.id} className="inline-flex items-center gap-1.5 rounded-full bg-surface-2 px-2.5 py-1 text-muted"><I className="size-3.5" aria-hidden />{t(`auto.type.${x.type}`)}</span>;
                })}
                {a.last_fired_at && (
                  <span className="text-subtle" title={a.last_result}>
                    {t("auto.lastRun", { when: ago(a.last_fired_at), result: a.last_result.split("; ").every((r) => r.endsWith(": ok")) ? t("auto.ok") : t("auto.failed") })}
                  </span>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}

      <Dialog open={!!form} onClose={() => { setForm(null); save.reset(); }} wide title={form?.id ? t("auto.edit") : t("auto.new")}
        footer={
          <>
            <Button variant="ghost" onClick={() => { setForm(null); save.reset(); }}>{t("common.cancel")}</Button>
            <Button variant="primary" loading={save.isPending} disabled={!form?.name.trim() || !form.place_id || !(form.on_arrive || form.on_leave)} onClick={() => form && save.mutate(form)}>{t("common.save")}</Button>
          </>
        }>
        {form && <Editor form={form} onChange={setForm} error={save.error?.message} />}
      </Dialog>
      <ConfirmDialog open={!!deleting} onClose={() => setDeleting(null)} onConfirm={() => deleting && remove.mutate(deleting)} loading={remove.isPending}
        title={t("auto.deleteTitle", { name: deleting?.name })} body={t("auto.deleteBody")} confirmLabel={t("common.delete")} />
    </div>
  );
}

function Editor({ form, onChange, error }: { form: Form; onChange: (f: Form) => void; error?: string }) {
  const { t } = useTranslation();
  const places = usePlaces();
  const set = (patch: Partial<Form>) => onChange({ ...form, ...patch });
  const setAction = (i: number, patch: Partial<AutoAction>) => set({ actions: form.actions.map((a, j) => (j === i ? { ...a, ...patch } : a)) });
  const canAdd = form.actions.length < 6;

  return (
    <div className="space-y-6">
      {error && <Notice tone="danger">{error}</Notice>}
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t("auto.name")}>
          {(id) => <Input id={id} value={form.name} maxLength={80} placeholder={t("auto.namePlaceholder")} onChange={(e) => set({ name: e.target.value })} />}
        </Field>
        <Field label={t("auto.place")}>
          {(id) => <Select id={id} label={t("auto.place")} value={form.place_id} onChange={(place_id) => set({ place_id })} options={(places.data ?? []).map((p) => ({ value: String(p.id), label: p.name }))} />}
        </Field>
      </div>

      <div>
        <p className="mb-2 text-sm font-medium">{t("auto.when")}</p>
        <div className="flex flex-wrap gap-x-8 gap-y-3">
          <Checkbox checked={form.on_arrive} onChange={(on_arrive) => set({ on_arrive })} label={t("auto.onArrive")} />
          <Checkbox checked={form.on_leave} onChange={(on_leave) => set({ on_leave })} label={t("auto.onLeave")} />
        </div>
      </div>

      <div className="space-y-3">
        <div className="flex items-center justify-between gap-3">
          <p className="text-sm font-medium">{t("auto.then")}</p>
          <Select<ActionType | "">
            value="" placeholder={t("auto.addAction")} label={t("auto.addAction")} disabled={!canAdd} className="w-48"
            onChange={(type) => type && set({ actions: [...form.actions, blank(type)] })}
            options={actionTypes.map((a) => ({ value: a.type, label: t(`auto.type.${a.type}`), icon: <a.icon className="size-4" aria-hidden /> }))}
          />
        </div>
        {form.actions.length === 0 && <p className="rounded-xl border border-dashed border-border-strong p-4 text-center text-sm text-muted">{t("auto.noActions")}</p>}
        {form.actions.map((a, i) => (
          <ActionCard key={a.id || i} action={a} onChange={(patch) => setAction(i, patch)} onRemove={() => set({ actions: form.actions.filter((_, j) => j !== i) })} />
        ))}
        <p className="text-xs text-subtle">
          {t("auto.variables")} {variables.map((v) => <code key={v} className="mx-0.5 rounded bg-surface-2 px-1 py-0.5">{`{{${v}}}`}</code>)}
        </p>
      </div>

      <div className="flex flex-wrap items-center justify-between gap-4 border-t border-border pt-4">
        <div className="flex items-center gap-2 text-sm text-muted">
          {t("auto.cooldown")}
          <Stepper label={t("auto.cooldown")} min={1} max={1440} step={5} value={form.cooldown_min} onChange={(cooldown_min) => set({ cooldown_min })} suffix="min" />
        </div>
        <div className="flex items-center gap-3 text-sm">
          {t("auto.enabled")}
          <Switch label={t("auto.enabled")} checked={form.enabled} onChange={(enabled) => set({ enabled })} />
        </div>
      </div>
    </div>
  );
}

function ActionCard({ action: a, onChange, onRemove }: { action: AutoAction; onChange: (p: Partial<AutoAction>) => void; onRemove: () => void }) {
  const { t } = useTranslation();
  const family = useFamily();
  const I = iconOf(a.type);
  const [headers, setHeaders] = useState(() => Object.entries(a.headers ?? {}).map(([k, v]) => `${k}: ${v}`).join("\n"));
  const parseHeaders = (text: string) => {
    setHeaders(text);
    const out: Record<string, string> = {};
    for (const line of text.split("\n")) {
      const at = line.indexOf(":");
      if (at > 0 && line.slice(0, at).trim()) out[line.slice(0, at).trim()] = line.slice(at + 1).trim();
    }
    onChange({ headers: out });
  };
  const text = (key: keyof AutoAction, label: string, props: { placeholder?: string; type?: string; hint?: string } = {}) => (
    <Field label={label} hint={props.hint}>
      {(id, d) => <Input id={id} aria-describedby={d} type={props.type} autoComplete="off" value={(a[key] as string) ?? ""} placeholder={props.placeholder} onChange={(e) => onChange({ [key]: e.target.value })} />}
    </Field>
  );
  const message = (
    <Field label={t("auto.message")}>
      {(id) => <textarea id={id} rows={2} maxLength={500} className={textarea} value={a.message ?? ""} placeholder={t("auto.messagePlaceholder")} onChange={(e) => onChange({ message: e.target.value })} />}
    </Field>
  );

  return (
    <div className="space-y-4 rounded-xl border border-border bg-surface-2/50 p-4">
      <div className="flex items-center gap-2">
        <I className="size-4 text-primary" aria-hidden />
        <p className="flex-1 font-medium">{t(`auto.type.${a.type}`)}</p>
        <Button variant="ghost" size="icon" aria-label={t("auto.removeAction")} title={t("auto.removeAction")} onClick={onRemove}><Trash2 className="size-4" /></Button>
      </div>
      <p className="-mt-2 text-sm text-muted">{t(`auto.typeDesc.${a.type}`)}</p>

      {a.type === "webhook" && (
        <>
          <div className="grid gap-4 sm:grid-cols-[8rem_1fr]">
            <Field label={t("auto.method")}>{(id) => <Select id={id} label={t("auto.method")} value={a.method ?? "POST"} onChange={(method) => onChange({ method })} options={["POST", "GET", "PUT"].map((m) => ({ value: m, label: m }))} />}</Field>
            {text("url", t("auto.url"), { placeholder: "http://homeassistant.local:8123/api/webhook/…", type: "url" })}
          </div>
          {(a.method ?? "POST") !== "GET" && (
            <Field label={t("auto.body")} hint={t("auto.bodyHint")}>
              {(id, d) => <textarea id={id} aria-describedby={d} rows={3} maxLength={4000} className={cn(textarea, "font-mono text-xs")} value={a.body ?? ""} placeholder={'{"event": "{{event}}", "place": "{{place}}"}'} onChange={(e) => onChange({ body: e.target.value })} />}
            </Field>
          )}
          <Field label={t("auto.headers")} hint={t("auto.headersHint")}>
            {(id, d) => <textarea id={id} aria-describedby={d} rows={2} className={cn(textarea, "font-mono text-xs")} value={headers} placeholder="Authorization: Bearer …" onChange={(e) => parseHeaders(e.target.value)} />}
          </Field>
        </>
      )}
      {a.type === "notify_family" && (
        <>
          {family.data && family.data.length > 0 ? (
            <div>
              <p className="mb-2 text-sm font-medium">{t("auto.who")}</p>
              <div className="grid gap-2.5 sm:grid-cols-2">
                {family.data.map((p) => (
                  <Checkbox key={p.user_id} label={p.name} checked={(a.members ?? []).includes(p.user_id)}
                    onChange={(on) => onChange({ members: on ? [...(a.members ?? []), p.user_id] : (a.members ?? []).filter((m) => m !== p.user_id) })} />
                ))}
              </div>
            </div>
          ) : (
            <Notice tone="warning">{t("auto.noFamily")} <Link to="/family" className="font-medium underline">{t("nav.family")}</Link></Notice>
          )}
          {message}
        </>
      )}
      {a.type === "notify_me" && message}
      {a.type === "ntfy" && (
        <>
          <div className="grid gap-4 sm:grid-cols-2">
            {text("topic", t("auto.topic"), { placeholder: "geotracker-sam" })}
            {text("url", t("auto.ntfyServer"), { placeholder: "https://ntfy.sh", type: "url" })}
          </div>
          {text("token", t("auto.ntfyToken"), { type: "password", hint: t("auto.optional") })}
          {message}
        </>
      )}
      {a.type === "telegram" && (
        <>
          <div className="grid gap-4 sm:grid-cols-2">
            {text("token", t("auto.botToken"), { type: "password", placeholder: "123456:ABC…", hint: t("auto.botHint") })}
            {text("chat_id", t("auto.chatId"), { placeholder: "123456789", hint: t("auto.chatHint") })}
          </div>
          {message}
        </>
      )}
      {(a.type === "discord" || a.type === "slack") && (
        <>
          {text("url", t("auto.hookUrl"), { type: "url", placeholder: a.type === "discord" ? "https://discord.com/api/webhooks/…" : "https://hooks.slack.com/services/…" })}
          {message}
        </>
      )}
      {a.type === "email" && (
        <>
          {text("to", t("auto.emailTo"), { type: "email", placeholder: "someone@example.com", hint: t("auto.emailHint") })}
          {message}
        </>
      )}
    </div>
  );
}

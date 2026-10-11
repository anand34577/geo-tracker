import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Check, Copy, Eye, History, Link2, Plus, Radio, Share2, Trash2 } from "lucide-react";
import { renderSVG } from "uqr";
import { useTranslation } from "react-i18next";
import { api, queryClient, type Share } from "../lib/api";
import { ago, dateTime } from "../lib/format";
import { usePrefs } from "../lib/prefs";
import { RadioCards, RangePicker, Select, presetRange, rangeLabel, type Range } from "../components/controls";
import { Button, ConfirmDialog, Dialog, EmptyState, ErrorState, Field, Input, Notice, PageHeader, Skeleton, cn, useToast } from "../components/ui";

const durations = ["1", "4", "24", "72", "168", "720"] as const;

export default function SharingPage() {
  const { t } = useTranslation();
  const toast = useToast();
  const { clock } = usePrefs();
  const shares = useQuery({ queryKey: ["shares"], queryFn: () => api<Share[]>("/shares") });
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<{ url: string; share: Share } | null>(null);
  const [revoking, setRevoking] = useState<Share | null>(null);
  const [form, setForm] = useState({ name: "", kind: "live" as "live" | "range", precision: "exact" as "exact" | "approx", hours: "4" as (typeof durations)[number], range: presetRange("today") as Range });

  const create = useMutation({
    meta: { inline: true }, // error shown in the form
    mutationFn: () =>
      api<{ url: string; share: Share }>("/shares", {
        method: "POST",
        body: { name: form.name, kind: form.kind, precision: form.precision, expires_in_hours: Number(form.hours), from: form.range.from, to: form.range.to },
      }),
    onSuccess: (r) => {
      setCreated(r);
      queryClient.invalidateQueries({ queryKey: ["shares"] });
    },
  });
  const revoke = useMutation({
    mutationFn: (s: Share) => api(`/shares/${s.id}`, { method: "DELETE" }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["shares"] });
      toast("success", t("share.revoked"));
      setRevoking(null);
    },
  });
  const close = () => {
    setCreating(false);
    setCreated(null);
    create.reset();
    setForm({ ...form, name: "" });
  };

  return (
    <div className="mx-auto w-full max-w-4xl px-4 py-6 md:px-8 md:py-10">
      <PageHeader title={t("share.title")} description={t("share.subtitle")} actions={<Button variant="primary" icon={Plus} onClick={() => setCreating(true)}>{t("share.new")}</Button>} />
      {shares.isError ? (
        <ErrorState error={shares.error} retry={() => shares.refetch()} />
      ) : shares.isPending ? (
        <Skeleton className="h-40" />
      ) : shares.data.length === 0 ? (
        <div className="rounded-2xl border border-dashed border-border-strong">
          <EmptyState icon={Share2} title={t("share.emptyTitle")} body={t("share.emptyBody")} action={<Button variant="primary" icon={Plus} onClick={() => setCreating(true)}>{t("share.new")}</Button>} />
        </div>
      ) : (
        <ul className="divide-y divide-border rounded-2xl border border-border bg-surface">
          {shares.data.map((s) => {
            const expired = s.expires_at < Date.now();
            return (
              <li key={s.id} className="flex flex-wrap items-center gap-3 p-4">
                <span className={cn("grid size-10 shrink-0 place-items-center rounded-xl", expired ? "bg-surface-2 text-subtle" : "bg-primary-subtle text-primary")}>
                  {s.kind === "live" ? <Radio className="size-5" aria-hidden /> : <History className="size-5" aria-hidden />}
                </span>
                <div className="min-w-0 flex-1">
                  <p className="truncate font-medium">{s.name}</p>
                  <p className="flex flex-wrap gap-x-3 text-sm text-muted">
                    <span>{s.kind === "live" ? t("share.liveShort") : rangeLabel({ from: s.from!, to: s.to! })}</span>
                    {s.precision === "approx" && <span>{t("share.approxShort")}</span>}
                    <span className="flex items-center gap-1"><Eye className="size-3.5" aria-hidden />{s.views}</span>
                  </p>
                </div>
                <span className={cn("rounded-full px-2.5 py-1 text-xs font-medium", expired ? "bg-surface-2 text-subtle" : "bg-primary-subtle text-primary")}>
                  {expired ? t("share.expired") : t("share.expires", { when: ago(s.expires_at) })}
                </span>
                <Button variant="ghost" size="icon" aria-label={t("share.revoke")} title={t("share.revoke")} onClick={() => setRevoking(s)}><Trash2 className="size-4" /></Button>
              </li>
            );
          })}
        </ul>
      )}

      <Dialog open={creating} onClose={close} wide title={created ? t("share.readyTitle") : t("share.new")}
        footer={created ? <Button variant="primary" onClick={close}>{t("common.done")}</Button> : (
          <>
            <Button variant="ghost" onClick={close}>{t("common.cancel")}</Button>
            <Button variant="primary" icon={Link2} loading={create.isPending} disabled={!form.name.trim()} onClick={() => create.mutate()}>{t("share.create")}</Button>
          </>
        )}>
        {created ? (
          <CreatedLink url={created.url} expires={dateTime(created.share.expires_at, clock)} />
        ) : (
          <div className="space-y-5">
            {create.error && <Notice tone="danger">{create.error.message}</Notice>}
            <Field label={t("share.name")} hint={t("share.nameHint")}>
              {(id, d) => <Input id={id} aria-describedby={d} value={form.name} maxLength={80} placeholder={t("share.namePlaceholder")} onChange={(e) => setForm({ ...form, name: e.target.value })} />}
            </Field>
            <div>
              <p className="mb-2 text-sm font-medium">{t("share.what")}</p>
              <RadioCards label={t("share.what")} value={form.kind} onChange={(kind) => setForm({ ...form, kind })} options={[
                { value: "live", label: t("share.live"), hint: t("share.liveHint"), icon: <Radio className="size-5 text-primary" /> },
                { value: "range", label: t("share.range"), hint: t("share.rangeHint"), icon: <History className="size-5 text-primary" /> },
              ]} />
            </div>
            {form.kind === "range" && (
              <Field label={t("share.period")}>{() => <RangePicker label={t("share.period")} value={form.range} onChange={(range) => setForm({ ...form, range })} allowAll={false} />}</Field>
            )}
            <div className="grid gap-5 sm:grid-cols-2">
              <Field label={t("share.expiresIn")}>
                {(id) => <Select id={id} label={t("share.expiresIn")} value={form.hours} onChange={(hours) => setForm({ ...form, hours })} options={durations.map((h) => ({ value: h, label: t(`share.dur.${h}`) }))} />}
              </Field>
              <div>
                <p className="mb-1.5 text-sm font-medium">{t("share.precision")}</p>
                <RadioCards label={t("share.precision")} columns={1} value={form.precision} onChange={(precision) => setForm({ ...form, precision })} options={[
                  { value: "exact", label: t("share.exact") },
                  { value: "approx", label: t("share.approx"), hint: t("share.approxHint") },
                ]} />
              </div>
            </div>
          </div>
        )}
      </Dialog>
      <ConfirmDialog open={!!revoking} onClose={() => setRevoking(null)} onConfirm={() => revoking && revoke.mutate(revoking)} loading={revoke.isPending}
        title={t("share.revokeTitle", { name: revoking?.name })} body={t("share.revokeBody")} confirmLabel={t("share.revoke")} />
    </div>
  );
}

function CreatedLink({ url, expires }: { url: string; expires: string }) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);
  return (
    <div className="flex flex-col items-center gap-4 text-center">
      <div className="rounded-xl bg-white p-3" style={{ width: 208, height: 208 }} role="img" aria-label={t("share.qr")} dangerouslySetInnerHTML={{ __html: renderSVG(url, { border: 1 }) }} />
      <div className="flex w-full gap-2">
        <Input readOnly value={url} className="font-mono text-xs" onFocus={(e) => e.target.select()} aria-label={t("share.link")} />
        <Button variant="primary" icon={copied ? Check : Copy} onClick={() => navigator.clipboard.writeText(url).then(() => setCopied(true))}>{copied ? t("common.copied") : t("common.copy")}</Button>
      </div>
      {/* Phones: hand the link straight to WhatsApp, Messages, etc. */}
      {"share" in navigator && (
        <Button icon={Share2} className="w-full justify-center" onClick={() => navigator.share({ url }).catch(() => {})}>{t("share.sendVia")}</Button>
      )}
      <p className="text-sm text-muted">{t("share.readyBody", { when: expires })}</p>
    </div>
  );
}

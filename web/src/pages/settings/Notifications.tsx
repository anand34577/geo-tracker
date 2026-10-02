import { useEffect, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Mail, Send } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, queryClient, useUser, type NotifyEvent, type NotifyPrefs } from "../../lib/api";
import { Button, ErrorState, Field, Input, Notice, Section, Skeleton, useToast } from "../../components/ui";
import { Slider, Stepper, Switch } from "../../components/controls";

type Resp = { prefs: NotifyPrefs; smtp_configured: boolean };
const events: NotifyEvent[] = ["place_arrive", "place_leave", "family", "device_silent", "battery_low", "import_done"];

export default function NotificationsTab() {
  const { t } = useTranslation();
  const toast = useToast();
  const user = useUser();
  const q = useQuery({ queryKey: ["notify"], queryFn: () => api<Resp>("/me/notifications") });
  const [p, setP] = useState<NotifyPrefs | null>(null);
  useEffect(() => {
    if (q.data) setP(q.data.prefs);
  }, [q.data]);

  const save = useMutation({
    mutationFn: (prefs: NotifyPrefs) => api<Resp>("/me/notifications", { method: "PUT", body: prefs }),
    onSuccess: (r) => {
      queryClient.setQueryData(["notify"], r);
      toast("success", t("settings.saved"));
    },
    onError: (e) => toast("error", e.message),
  });
  const test = useMutation({
    mutationFn: async (channel: "email" | "gotify") => {
      await api<Resp>("/me/notifications", { method: "PUT", body: p }); // test what's on screen
      return api(`/me/notifications/test?channel=${channel}`, { method: "POST" });
    },
    onSuccess: () => toast("success", t("notify.testSent")),
    onError: (e) => toast("error", e.message),
  });

  if (q.isError) return <ErrorState error={q.error} retry={() => q.refetch()} />;
  if (!p || !q.data) return <Skeleton className="h-96" />;
  const set = (patch: Partial<NotifyPrefs>) => setP({ ...p, ...patch });

  return (
    <>
      <Section title={t("notify.channels")} description={t("notify.channelsDesc")}>
        <div className="space-y-6">
          {/* Email */}
          <div className="space-y-4">
            <div className="flex items-center justify-between gap-4">
              <div>
                <p className="flex items-center gap-2 font-medium"><Mail className="size-4" aria-hidden />{t("notify.email")}</p>
                <p className="text-sm text-muted">{t("notify.emailDesc")}</p>
              </div>
              <Switch label={t("notify.email")} checked={p.email.enabled} onChange={(enabled) => set({ email: { ...p.email, enabled } })} />
            </div>
            {p.email.enabled && (
              <div className="grid gap-4 sm:grid-cols-[1fr_auto] sm:items-end">
                {!q.data.smtp_configured && <div className="sm:col-span-2"><Notice tone="warning">{t("notify.smtpMissing")}</Notice></div>}
                <Field label={t("notify.sendTo")} hint={t("notify.sendToHint", { email: user.email })}>
                  {(id, d) => <Input id={id} aria-describedby={d} type="email" value={p.email.to} placeholder={user.email} onChange={(e) => set({ email: { ...p.email, to: e.target.value } })} />}
                </Field>
                <Button icon={Send} loading={test.isPending && test.variables === "email"} disabled={!q.data.smtp_configured} onClick={() => test.mutate("email")} className="sm:mb-6">
                  {t("notify.test")}
                </Button>
              </div>
            )}
          </div>

          {/* Gotify */}
          <div className="space-y-4 border-t border-border pt-6">
            <div className="flex items-center justify-between gap-4">
              <div>
                <p className="flex items-center gap-2 font-medium"><Send className="size-4" aria-hidden />Gotify</p>
                <p className="text-sm text-muted">{t("notify.gotifyDesc")}</p>
              </div>
              <Switch label="Gotify" checked={p.gotify.enabled} onChange={(enabled) => set({ gotify: { ...p.gotify, enabled } })} />
            </div>
            {p.gotify.enabled && (
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label={t("notify.gotifyUrl")}>{(id) => <Input id={id} type="url" placeholder="https://gotify.example.com" value={p.gotify.url} onChange={(e) => set({ gotify: { ...p.gotify, url: e.target.value } })} />}</Field>
                <Field label={t("notify.gotifyToken")} hint={t("notify.gotifyTokenHint")}>
                  {(id, d) => <Input id={id} aria-describedby={d} type="password" autoComplete="off" value={p.gotify.token} onChange={(e) => set({ gotify: { ...p.gotify, token: e.target.value } })} />}
                </Field>
                <Field label={t("notify.priority", { n: p.gotify.priority })} hint={t("notify.priorityHint")}>
                  {(id) => <Slider id={id} label={t("notify.priorityLabel")} min={0} max={10} value={p.gotify.priority} onChange={(priority) => set({ gotify: { ...p.gotify, priority } })} />}
                </Field>
                <div className="flex items-start sm:justify-end">
                  <Button icon={Send} loading={test.isPending && test.variables === "gotify"} disabled={!p.gotify.url || !p.gotify.token} onClick={() => test.mutate("gotify")}>
                    {t("notify.test")}
                  </Button>
                </div>
              </div>
            )}
          </div>
        </div>
      </Section>

      <Section title={t("notify.events")} description={t("notify.eventsDesc")}>
        <ul className="-my-2 divide-y divide-border">
          {[...events, ...(user.role === "admin" ? (["backup_failed"] as NotifyEvent[]) : [])].map((e) => (
            <li key={e} className="flex items-center justify-between gap-4 py-3">
              <div>
                <p className="text-sm font-medium">{t(`notify.event.${e}`)}</p>
                <p className="text-sm text-muted">{t(`notify.eventDesc.${e}`)}</p>
                {e === "device_silent" && p.events[e] && (
                  <div className="mt-2 flex items-center gap-2 text-sm text-muted">
                    {t("notify.after")}
                    <Stepper label={t("notify.silentLabel")} min={1} max={168} value={p.silent_hours} onChange={(silent_hours) => set({ silent_hours })} suffix="h" />
                  </div>
                )}
                {e === "battery_low" && p.events[e] && (
                  <div className="mt-2 flex items-center gap-2 text-sm text-muted">
                    {t("notify.below")}
                    <Stepper label={t("notify.batteryLabel")} min={1} max={99} step={5} value={p.battery_below} onChange={(battery_below) => set({ battery_below })} suffix="%" />
                  </div>
                )}
              </div>
              <Switch label={t(`notify.event.${e}`)} checked={p.events[e]} onChange={(v) => set({ events: { ...p.events, [e]: v } })} />
            </li>
          ))}
        </ul>
      </Section>

      <div className="flex justify-end">
        <Button variant="primary" loading={save.isPending} onClick={() => save.mutate(p)}>{t("common.save")}</Button>
      </div>
    </>
  );
}

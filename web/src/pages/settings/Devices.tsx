import { useState } from "react";
import { useSearchParams } from "react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Activity, BatteryLow, BatteryMedium, Check, ChevronDown, Copy, KeyRound, MoonStar, Plus, Smartphone, Trash2, WifiOff, type LucideIcon } from "lucide-react";
import { renderSVG } from "uqr";
import { useTranslation } from "react-i18next";
import { api, queryClient, useConfig, type Device, type DeviceHealth, type QuietSpell } from "../../lib/api";
import { ago, dateTime, distance, duration, number } from "../../lib/format";
import { usePrefs } from "../../lib/prefs";
import i18n from "../../lib/i18n";
import { AreaChart } from "../../components/Chart";
import { RadioCards } from "../../components/controls";
import { Button, ConfirmDialog, Dialog, EmptyState, ErrorState, Field, Input, Notice, Section, Skeleton, cn, useToast } from "../../components/ui";

const apps = [
  { id: "colota", name: "Colota", platforms: "Android" },
  { id: "owntracks", name: "OwnTracks", platforms: "Android · iOS" },
  { id: "overland", name: "Overland", platforms: "iOS · Android" },
  { id: "gpslogger", name: "GPSLogger", platforms: "Android" },
  { id: "traccar", name: "Traccar Client", platforms: "Android · iOS" },
  { id: "homeassistant", name: "Home Assistant", platforms: "rest_command" },
  { id: "other", name: "Other / script", platforms: "HTTP API" },
] as const;
const appName = (client: string) => (client === "app" ? "GeoTracker app" : apps.find((a) => a.id === client)?.name ?? client);

const slug = (s: string) => s.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "") || "phone";
const b64 = (s: string) => btoa(String.fromCharCode(...new TextEncoder().encode(s)));

/** Everything a phone app needs, including a QR code where the app supports config links. */
function setupFor(client: string, base: string, token: string, name: string) {
  switch (client) {
    case "colota": {
      const endpoint = `${base}/ingest/colota`;
      const cfg = { endpoint, apiTemplate: "custom", httpMethod: "POST", auth: { type: "bearer", bearerToken: token } };
      return {
        qr: `colota://setup?config=${encodeURIComponent(b64(JSON.stringify(cfg)))}`,
        fields: [["Endpoint", endpoint], ["Request format", "Custom (default fields)"], ["Authentication", "Bearer token"], ["Token", token]],
        steps: "devices.stepsColota",
      };
    }
    case "homeassistant": {
      const yaml = [
        "rest_command:",
        "  geotracker:",
        `    url: ${base}/ingest/json`,
        "    method: POST",
        "    headers:",
        `      Authorization: "Bearer ${token}"`,
        '    content_type: "application/json"',
        `    payload: '[{"ts": {{ (now().timestamp() * 1000) | int }}, "lat": {{ state_attr("device_tracker.phone", "latitude") }}, "lon": {{ state_attr("device_tracker.phone", "longitude") }}, "acc": {{ state_attr("device_tracker.phone", "gps_accuracy") | default(0) }}}]'`,
      ].join("\n");
      return { fields: [["configuration.yaml", yaml]], steps: "devices.stepsHomeAssistant" };
    }
    case "owntracks": {
      const url = `${base}/ingest/owntracks`;
      const cfg = { _type: "configuration", mode: 3, url, auth: true, username: slug(name), password: token, deviceId: slug(name), tid: name.slice(0, 2).toUpperCase() };
      return {
        qr: `owntracks:///config?inline=${b64(JSON.stringify(cfg))}`,
        fields: [["Mode", "HTTP"], ["URL", url], ["Username", slug(name)], ["Password", token]],
        steps: "devices.stepsOwntracks",
      };
    }
    case "overland": {
      const url = `${base}/ingest/overland?token=${token}`;
      return {
        qr: `overland://setup?url=${encodeURIComponent(url)}&token=${token}&device_id=${slug(name)}`,
        fields: [["Receiver endpoint", url], ["Access token", token]],
        steps: "devices.stepsOverland",
      };
    }
    case "gpslogger":
      return {
        fields: [
          ["URL", `${base}/ingest/gpslogger?token=${token}&lat=%LAT&lon=%LON&acc=%ACC&alt=%ALT&spd=%SPD&dir=%DIR&batt=%BATT&timestamp=%TIMESTAMP`],
          ["HTTP method", "GET"],
        ],
        steps: "devices.stepsGpslogger",
      };
    case "traccar":
      return { fields: [["Server URL", `${base}/ingest/osmand`], ["Device identifier", token]], steps: "devices.stepsTraccar" };
    default:
      return {
        fields: [
          ["Endpoint", `${base}/ingest/json`],
          ["Header", `Authorization: Bearer ${token}`],
          ["Example", `curl -H "Authorization: Bearer ${token}" -d '[{"ts":${Date.now()},"lat":52.52,"lon":13.405}]' ${base}/ingest/json`],
        ],
        steps: "devices.stepsOther",
      };
  }
}

function CopyField({ label, value }: { label: string; value: string }) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);
  return (
    <Field label={label}>
      {(id) => (
        <div className="flex gap-2">
          {value.includes("\n") ? (
            <textarea id={id} readOnly value={value} rows={value.split("\n").length} className="w-full rounded-lg border border-border-strong bg-surface-2 p-3 font-mono text-xs whitespace-pre" onFocus={(e) => e.target.select()} />
          ) : (
            <Input id={id} readOnly value={value} className="font-mono text-xs" onFocus={(e) => e.target.select()} />
          )}
          <Button size="icon" aria-label={t("common.copy")} onClick={() => navigator.clipboard.writeText(value).then(() => { setCopied(true); setTimeout(() => setCopied(false), 1500); })}>
            {copied ? <Check className="size-4 text-success" /> : <Copy className="size-4" />}
          </Button>
        </div>
      )}
    </Field>
  );
}

function SetupView({ device, token }: { device: Device; token: string }) {
  const { t } = useTranslation();
  const { data: config } = useConfig();
  const devices = useQuery({ queryKey: ["devices"], queryFn: () => api<Device[]>("/devices"), refetchInterval: 5000 });
  const live = devices.data?.find((d) => d.id === device.id);
  const connected = live?.last_seen_at && live.last_seen_at > Date.now() - 10 * 60_000;
  const s = setupFor(device.client, config?.base_url ?? location.origin, token, device.name);
  return (
    <div className="space-y-5">
      <Notice tone="warning">{t("devices.tokenOnce")}</Notice>
      {config && !config.base_url.startsWith("https://") && <Notice tone="warning">{t("devices.httpsWarning")}</Notice>}
      <ol className="list-decimal space-y-1 pl-5 text-sm text-muted">
        {(t(s.steps, { returnObjects: true }) as string[]).map((step) => <li key={step}>{step}</li>)}
      </ol>
      {s.qr && (
        <div className="flex flex-col items-center gap-2">
          <div className="rounded-xl bg-white p-3" dangerouslySetInnerHTML={{ __html: renderSVG(s.qr, { border: 1 }) }} style={{ width: 232, height: 232 }} role="img" aria-label={t("devices.qrLabel")} />
          <a href={s.qr} className="text-sm font-medium text-primary">{t("devices.openOnPhone")}</a>
        </div>
      )}
      <div className="space-y-3">
        {s.fields.map(([label, value]) => <CopyField key={label} label={label} value={value} />)}
      </div>
      <div role="status" className={cn("flex items-center gap-2 rounded-lg px-3 py-2.5 text-sm", connected ? "bg-primary-subtle text-fg" : "bg-surface-2 text-muted")}>
        {connected ? <Check className="size-4 text-success" /> : <span className="size-2 animate-pulse rounded-full bg-primary" />}
        {connected ? t("devices.connected") : t("devices.waiting")}
      </div>
    </div>
  );
}

export default function DevicesTab() {
  const { t } = useTranslation();
  const toast = useToast();
  const [params] = useSearchParams();
  const devices = useQuery({ queryKey: ["devices"], queryFn: () => api<Device[]>("/devices") });
  const [adding, setAdding] = useState(params.get("welcome") === "1");
  const [form, setForm] = useState({ name: "", client: "colota" });
  const [created, setCreated] = useState<{ device: Device; token: string } | null>(null);
  const [deleting, setDeleting] = useState<Device | null>(null);
  const [health, setHealth] = useState<number | null>(null);

  const create = useMutation({
    meta: { inline: true }, // error shown in the form
    mutationFn: () => api<{ device: Device; token: string }>("/devices", { method: "POST", body: form }),
    onSuccess: (r) => {
      setCreated(r);
      queryClient.invalidateQueries({ queryKey: ["devices"] });
    },
  });
  const rotate = useMutation({
    mutationFn: (d: Device) => api<{ token: string }>(`/devices/${d.id}/token`, { method: "POST" }),
    onSuccess: (r, d) => {
      setCreated({ device: d, token: r.token });
      setAdding(true);
    },
  });
  const del = useMutation({
    mutationFn: (d: Device) => api(`/devices/${d.id}`, { method: "DELETE" }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["devices"] });
      toast("success", t("devices.removed"));
      setDeleting(null);
    },
  });

  const close = () => {
    setAdding(false);
    setCreated(null);
    setForm({ name: "", client: "colota" });
  };

  return (
    <>
      {params.get("welcome") === "1" && <Notice>{t("devices.welcome")}</Notice>}
      <Section title={t("settings.devices")} description={t("devices.desc")} actions={<Button variant="primary" size="sm" icon={Plus} onClick={() => setAdding(true)}>{t("devices.add")}</Button>}>
        {devices.isError ? (
          <ErrorState error={devices.error} retry={() => devices.refetch()} />
        ) : devices.isPending ? (
          <Skeleton className="h-14" />
        ) : devices.data.length === 0 ? (
          <EmptyState icon={Smartphone} title={t("devices.emptyTitle")} body={t("devices.emptyBody")} />
        ) : (
          <ul className="-my-2 divide-y divide-border">
            {devices.data.map((d) => (
              <li key={d.id} className="py-3">
              <div className="flex flex-wrap items-center gap-3">
                <Smartphone className="size-5 text-muted" aria-hidden />
                <div className="min-w-0 flex-1">
                  <p className="truncate font-medium">{d.name}</p>
                  <p className="flex flex-wrap gap-x-3 text-sm text-muted">
                    <span>{appName(d.client)}</span>
                    <span>{d.last_seen_at ? t("devices.lastSeen", { when: ago(d.last_seen_at) }) : t("devices.never")}</span>
                    {d.last_battery != null && <span className="flex items-center gap-1"><BatteryMedium className="size-3.5" aria-hidden />{d.last_battery}%</span>}
                  </p>
                </div>
                {d.last_seen_at && (
                  <Button size="sm" variant={health === d.id ? "secondary" : "ghost"} icon={Activity} aria-expanded={health === d.id} onClick={() => setHealth(health === d.id ? null : d.id)}>
                    {t("devices.health")}
                    <ChevronDown className={cn("size-3.5 transition-transform", health === d.id && "rotate-180")} aria-hidden />
                  </Button>
                )}
                <Button size="sm" variant="ghost" icon={KeyRound} loading={rotate.isPending && rotate.variables?.id === d.id} onClick={() => rotate.mutate(d)}>
                  {t("devices.newToken")}
                </Button>
                <Button size="icon" variant="ghost" aria-label={t("common.delete")} onClick={() => setDeleting(d)}>
                  <Trash2 className="size-4" />
                </Button>
              </div>
              {health === d.id && <HealthPanel device={d} />}
              </li>
            ))}
          </ul>
        )}
      </Section>

      <Dialog open={adding} onClose={close} title={created ? t("devices.setupTitle", { name: created.device.name }) : t("devices.add")} wide
        footer={created ? <Button variant="primary" onClick={close}>{t("common.done")}</Button> : (
          <>
            <Button variant="ghost" onClick={close}>{t("common.cancel")}</Button>
            <Button variant="primary" loading={create.isPending} disabled={!form.name.trim()} onClick={() => create.mutate()}>{t("common.continue")}</Button>
          </>
        )}>
        {created ? (
          <SetupView device={created.device} token={created.token} />
        ) : (
          <div className="space-y-5">
            <div>
              <p className="mb-2 text-sm font-medium">{t("devices.whichApp")}</p>
              <RadioCards
                label={t("devices.whichApp")}
                value={form.client}
                onChange={(client) => setForm({ ...form, client })}
                options={apps.map((a) => ({
                  value: a.id,
                  label: a.name,
                  hint: a.platforms,
                  badge: a.id === "colota" ? t("devices.recommendedAndroid") : a.id === "owntracks" ? t("devices.recommendedIos") : undefined,
                }))}
              />
            </div>
            <Field label={t("devices.name")} error={create.error?.message}>
              {(id) => <Input id={id} value={form.name} maxLength={60} placeholder={t("devices.namePlaceholder")} onChange={(e) => setForm({ ...form, name: e.target.value })} />}
            </Field>
          </div>
        )}
      </Dialog>

      <ConfirmDialog open={!!deleting} onClose={() => setDeleting(null)} onConfirm={() => deleting && del.mutate(deleting)} loading={del.isPending}
        title={t("devices.removeTitle", { name: deleting?.name })} body={t("devices.removeBody")} confirmLabel={t("devices.remove")} />
    </>
  );
}

const reasonIcon: Record<QuietSpell["reason"], LucideIcon> = { battery: BatteryLow, stationary: MoonStar, offline: WifiOff, silent: WifiOff };
const HEALTH_DAYS = 14;

/** Battery over two weeks and every time the phone went quiet, with the likely reason. */
function HealthPanel({ device }: { device: Device }) {
  const { t } = useTranslation();
  const { units, clock } = usePrefs();
  const [showStill, setShowStill] = useState(false);
  const q = useQuery({ queryKey: ["device-health", device.id], queryFn: () => api<DeviceHealth>(`/devices/${device.id}/health?days=${HEALTH_DAYS}`) });
  if (q.isError) return <ErrorState error={q.error} retry={() => q.refetch()} />;
  if (!q.data) return <Skeleton className="mt-3 h-40" />;
  const h = q.data;
  const still = h.spells.filter((s) => s.reason === "stationary");
  const shown = h.spells.filter((s) => showStill || s.reason !== "stationary");
  const problems = h.spells.length - still.length;
  const pct = Math.round(Math.max(0, Math.min(1, h.coverage)) * 100);
  const day = (x: number) => new Date(x).toLocaleDateString(i18n.language, { day: "numeric", month: "short" });
  return (
    <div className="gt-pop mt-3 space-y-5 rounded-xl border border-border bg-surface-2/50 p-4">
      <p className="text-sm font-semibold">{t("devices.healthTitle", { days: HEALTH_DAYS })}</p>
      <dl className="grid grid-cols-3 gap-2">
        <div className="rounded-lg bg-surface px-3 py-2" title={t("devices.coverageHint")}>
          <dt className="text-[11px] text-subtle">{t("devices.coverage")}</dt>
          <dd className={cn("font-semibold tabular-nums", pct >= 95 ? "text-success" : pct >= 80 ? "text-fg" : "text-warning")}>{pct}%</dd>
        </div>
        <div className="rounded-lg bg-surface px-3 py-2">
          <dt className="text-[11px] text-subtle">{t("devices.fixes")}</dt>
          <dd className="font-semibold tabular-nums">{number(h.fixes)}</dd>
        </div>
        <div className="rounded-lg bg-surface px-3 py-2">
          <dt className="text-[11px] text-subtle">{t("devices.quiet")}</dt>
          <dd className={cn("font-semibold tabular-nums", problems > 0 && "text-warning")}>{problems}</dd>
        </div>
      </dl>
      {h.fixes === 0 ? (
        <p className="text-sm text-muted">{t("devices.noFixes")}</p>
      ) : h.battery.length > 1 ? (
        <div className="pt-6">
          <AreaChart data={h.battery} label={t("devices.batteryChart")} yMin={0} yMax={100} height={72} formatY={(v) => `${Math.round(v)}%`} formatX={day} />
        </div>
      ) : (
        <p className="text-sm text-muted">{t("devices.noBattery")}</p>
      )}
      {h.fixes > 0 && (
        <div>
          {problems === 0 && <p className="mb-2 flex items-center gap-2 text-sm text-success"><Check className="size-4" aria-hidden />{t("devices.allGood")}</p>}
          <ol className="space-y-1.5">
            {shown.map((s) => {
              const I = reasonIcon[s.reason];
              const tone = s.reason === "stationary" ? "bg-surface-3 text-muted" : s.reason === "battery" ? "bg-danger-subtle text-danger" : "bg-warning-subtle text-warning";
              return (
                <li key={s.from} className="flex items-start gap-3 rounded-lg bg-surface px-3 py-2.5 text-sm">
                  <span className={cn("mt-0.5 grid size-7 shrink-0 place-items-center rounded-lg", tone)}><I className="size-4" aria-hidden /></span>
                  <div className="min-w-0 flex-1">
                    <p className="flex flex-wrap items-baseline justify-between gap-x-3">
                      <span className="font-medium">{t(`devices.reason.${s.reason}`)}</span>
                      <span className="text-xs text-subtle tabular-nums">{s.to ? duration(s.to - s.from) : t("devices.stillQuiet")}</span>
                    </p>
                    <p className="text-xs text-muted tabular-nums">{dateTime(s.from, clock)}{s.to ? ` – ${dateTime(s.to, clock)}` : ""}</p>
                    <p className="mt-0.5 text-xs text-subtle">
                      {t(`devices.reasonHint.${s.reason}`, { before: s.batt_before ?? "?", after: s.batt_after ?? "?", distance: distance(s.moved_m, units) })}
                    </p>
                  </div>
                </li>
              );
            })}
          </ol>
          {still.length > 0 && (
            <button onClick={() => setShowStill(!showStill)} className="mt-2 text-sm font-medium text-primary hover:underline">
              {showStill ? t("devices.hideStill") : t("devices.showStill", { count: still.length })}
            </button>
          )}
        </div>
      )}
    </div>
  );
}

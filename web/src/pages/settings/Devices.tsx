import { useState } from "react";
import { useSearchParams } from "react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { BatteryMedium, Check, Copy, KeyRound, Plus, Smartphone, Trash2 } from "lucide-react";
import { renderSVG } from "uqr";
import { useTranslation } from "react-i18next";
import { api, queryClient, useConfig, type Device } from "../../lib/api";
import { ago } from "../../lib/format";
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
              <li key={d.id} className="flex flex-wrap items-center gap-3 py-3">
                <Smartphone className="size-5 text-muted" aria-hidden />
                <div className="min-w-0 flex-1">
                  <p className="truncate font-medium">{d.name}</p>
                  <p className="flex flex-wrap gap-x-3 text-sm text-muted">
                    <span>{appName(d.client)}</span>
                    <span>{d.last_seen_at ? t("devices.lastSeen", { when: ago(d.last_seen_at) }) : t("devices.never")}</span>
                    {d.last_battery != null && <span className="flex items-center gap-1"><BatteryMedium className="size-3.5" aria-hidden />{d.last_battery}%</span>}
                  </p>
                </div>
                <Button size="sm" variant="ghost" icon={KeyRound} loading={rotate.isPending && rotate.variables?.id === d.id} onClick={() => rotate.mutate(d)}>
                  {t("devices.newToken")}
                </Button>
                <Button size="icon" variant="ghost" aria-label={t("common.delete")} onClick={() => setDeleting(d)}>
                  <Trash2 className="size-4" />
                </Button>
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

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { RotateCcw } from "lucide-react";
import type { Prefs } from "../lib/api";
import { duration, speed as fmtSpeed } from "../lib/format";
import {
  ACC_CAP, activeFilters, ALT_STOPS, BANDS, resetFilters, SPEED_CAP, SPEED_STOPS, TIME_STOPS,
  vizStats, type Extent, type Filtered, type Motion, type Stop, type Viz,
} from "../lib/viz";
import { Slider } from "./controls";
import { Button, Segmented, cn } from "./ui";

const gradient = (stops: Stop[]) => `linear-gradient(to right, ${stops.map(([, c]) => c).join(", ")})`;

function hourLabel(h: number, clock: Prefs["clock"]) {
  const hh = Math.floor(h) % 24;
  if (clock === "12h") return `${hh % 12 || 12} ${hh < 12 ? "am" : "pm"}`;
  return `${String(hh).padStart(2, "0")}:00`;
}

/** What the line colours mean right now. Renders nothing for the solid colour. */
export function Legend({ viz, ext, units, clock, className }: { viz: Viz; ext: Extent; units: Prefs["units"]; clock: Prefs["clock"]; className?: string }) {
  const { t } = useTranslation();
  if (viz.color === "solid") return null;
  let stops: Stop[], labels: string[], title: string;
  if (viz.color === "speed") {
    stops = SPEED_STOPS.slice(0, 5);
    labels = stops.map(([v]) => fmtSpeed(v, units).replace(/ (km\/h|mph)$/, ""));
    labels[labels.length - 1] += `+ ${units === "imperial" ? "mph" : "km/h"}`;
    title = t("map.viz.speed");
  } else if (viz.color === "time") {
    stops = TIME_STOPS;
    const at = (f: number) => new Date(ext.from + (ext.to - ext.from) * f);
    const fmt = (d: Date) => (ext.to - ext.from > 36 * 3600_000 ? d.toLocaleDateString(undefined, { day: "numeric", month: "short" }) : d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", hour12: clock === "12h" }));
    labels = [fmt(at(0)), "", fmt(at(0.5)), "", fmt(at(1))];
    title = t("map.viz.time");
  } else {
    if (ext.altMin == null || ext.altMax == null) return <p className={cn("text-xs text-muted", className)}>{t("map.viz.noAltitude")}</p>;
    stops = ALT_STOPS;
    const m = (f: number) => `${Math.round(ext.altMin! + (ext.altMax! - ext.altMin!) * f)} m`;
    labels = [m(0), m(0.33), m(0.66), m(1)];
    title = t("map.viz.altitude");
  }
  return (
    <div className={className} role="img" aria-label={`${title}: ${labels.filter(Boolean).join(", ")}`}>
      <div className="h-2 rounded-full" style={{ background: gradient(stops) }} />
      <div className="mt-1 flex justify-between text-[10px] text-muted tabular-nums">
        {labels.map((l, i) => <span key={i}>{l}</span>)}
      </div>
    </div>
  );
}

function Section({ title, children, aside }: { title: string; children: React.ReactNode; aside?: React.ReactNode }) {
  return (
    <section className="px-3 py-3">
      <div className="mb-2 flex items-center justify-between gap-2">
        <h3 className="text-xs font-semibold tracking-wide text-subtle uppercase">{title}</h3>
        {aside}
      </div>
      {children}
    </section>
  );
}

function Row({ label, value, children }: { label: string; value: string; children: React.ReactNode }) {
  return (
    <div className="mt-2 first:mt-0">
      <div className="flex justify-between text-sm"><span className="text-muted">{label}</span><span className="font-medium tabular-nums">{value}</span></div>
      {children}
    </div>
  );
}

/** Colour, weight and filters for the map's recorded points. */
export function VizControls({ viz, onChange, ext, units, clock, shown, total }: {
  viz: Viz;
  onChange: (v: Viz) => void;
  ext: Extent;
  units: Prefs["units"];
  clock: Prefs["clock"];
  shown: number;
  total: number;
}) {
  const { t } = useTranslation();
  const set = (p: Partial<Viz>) => onChange({ ...viz, ...p });
  const k = units === "imperial" ? 2.23694 : 3.6;
  const unit = units === "imperial" ? "mph" : "km/h";
  const n = activeFilters(viz);
  const speedText = (v: number) => `${Math.round(v * k)} ${unit}`;
  const maxText = viz.maxSpeed >= SPEED_CAP ? t("map.viz.noLimit") : speedText(viz.maxSpeed);
  const presets: { id: string; label: string; min: number; max: number }[] = [
    { id: "walk", label: t("map.viz.band.walk"), min: 0.5, max: 2.2 },
    { id: "bike", label: t("map.viz.band.bike"), min: 2.2, max: 7 },
    { id: "drive", label: t("map.viz.band.drive"), min: 7, max: SPEED_CAP },
  ];
  return (
    <div className="max-h-[min(34rem,calc(100dvh-8rem))] divide-y divide-border overflow-y-auto">
      <Section title={t("map.viz.colorBy")}>
        <Segmented
          label={t("map.viz.colorBy")}
          value={viz.color}
          onChange={(color) => set({ color })}
          options={[
            { value: "solid", label: t("map.viz.solid") },
            { value: "speed", label: t("map.viz.speed") },
            { value: "time", label: t("map.viz.time") },
            { value: "altitude", label: t("map.viz.altitude") },
          ]}
        />
        <Legend viz={viz} ext={ext} units={units} clock={clock} className="mt-3" />
      </Section>

      <Section title={t("map.viz.weight")}>
        <Row label={t("map.viz.lineWidth")} value="">
          <div className="mt-1.5">
            <Segmented
              label={t("map.viz.lineWidth")}
              value={viz.width}
              onChange={(width) => set({ width })}
              options={[
                { value: "normal", label: t("map.viz.normal") },
                { value: "bold", label: t("map.viz.bold") },
                { value: "speed", label: t("map.viz.bySpeed") },
              ]}
            />
          </div>
        </Row>
        <Row label={t("map.viz.heatWeight")} value="">
          <div className="mt-1.5">
            <Segmented
              label={t("map.viz.heatWeight")}
              value={viz.heat}
              onChange={(heat) => set({ heat })}
              options={[
                { value: "density", label: t("map.viz.density") },
                { value: "dwell", label: t("map.viz.dwell") },
                { value: "speed", label: t("map.viz.speed") },
              ]}
            />
          </div>
          <p className="mt-1.5 text-xs text-subtle">{t(`map.viz.heatHint.${viz.heat}`)}</p>
        </Row>
      </Section>

      <Section
        title={t("map.viz.filters")}
        aside={
          n > 0 ? (
            <Button variant="ghost" size="sm" icon={RotateCcw} onClick={() => onChange(resetFilters(viz))}>{t("map.viz.reset")}</Button>
          ) : undefined
        }
      >
        <p className="mb-3 text-sm tabular-nums" aria-live="polite">
          {t("map.viz.showing", { shown: shown.toLocaleString(), total: total.toLocaleString() })}
        </p>
        <div className="mb-3 flex flex-wrap gap-1.5">
          {presets.map((p) => {
            const on = viz.minSpeed === p.min && viz.maxSpeed === p.max;
            return (
              <button
                key={p.id}
                aria-pressed={on}
                onClick={() => set(on ? { minSpeed: 0, maxSpeed: SPEED_CAP } : { minSpeed: p.min, maxSpeed: p.max })}
                className={cn("h-7 rounded-full border px-3 text-xs font-medium", on ? "border-primary bg-primary-subtle text-primary" : "border-border-strong text-muted hover:text-fg")}
              >
                {p.label}
              </button>
            );
          })}
        </div>
        <Row label={t("map.viz.minSpeed")} value={speedText(viz.minSpeed)}>
          <Slider label={t("map.viz.minSpeed")} min={0} max={SPEED_CAP} step={0.5} value={viz.minSpeed} format={speedText}
            onChange={(v) => set({ minSpeed: Math.min(v, viz.maxSpeed) })} />
        </Row>
        <Row label={t("map.viz.maxSpeed")} value={maxText}>
          <Slider label={t("map.viz.maxSpeed")} min={0} max={SPEED_CAP} step={0.5} value={viz.maxSpeed} format={(v) => (v >= SPEED_CAP ? t("map.viz.noLimit") : speedText(v))}
            onChange={(v) => set({ maxSpeed: Math.max(v, viz.minSpeed) })} />
        </Row>
        <Row label={t("map.viz.hourFrom")} value={hourLabel(viz.hourFrom, clock)}>
          <Slider label={t("map.viz.hourFrom")} min={0} max={24} step={1} value={viz.hourFrom} format={(v) => hourLabel(v, clock)} onChange={(v) => set({ hourFrom: v })} />
        </Row>
        <Row label={t("map.viz.hourTo")} value={viz.hourTo >= 24 ? t("map.viz.endOfDay") : hourLabel(viz.hourTo, clock)}>
          <Slider label={t("map.viz.hourTo")} min={0} max={24} step={1} value={viz.hourTo} format={(v) => (v >= 24 ? t("map.viz.endOfDay") : hourLabel(v, clock))} onChange={(v) => set({ hourTo: v })} />
        </Row>
        <Row label={t("map.viz.maxAcc")} value={viz.maxAcc >= ACC_CAP ? t("map.viz.anyAcc") : `±${Math.round(viz.maxAcc)} m`}>
          <Slider label={t("map.viz.maxAcc")} min={5} max={ACC_CAP} step={5} value={viz.maxAcc} format={(v) => (v >= ACC_CAP ? t("map.viz.anyAcc") : `±${v} m`)} onChange={(v) => set({ maxAcc: v })} />
        </Row>
        <div className="mt-3">
          <Segmented<Motion>
            label={t("map.viz.motion")}
            value={viz.motion}
            onChange={(motion) => set({ motion })}
            options={[
              { value: "all", label: t("map.viz.all") },
              { value: "moving", label: t("map.viz.moving") },
              { value: "still", label: t("map.viz.still") },
            ]}
          />
        </div>
      </Section>
    </div>
  );
}

/** Time per speed band and activity per hour of day, for the points currently shown. */
export function Insights({ flt, units, clock }: { flt: Filtered; units: Prefs["units"]; clock: Prefs["clock"] }) {
  const { t } = useTranslation();
  const s = useMemo(() => vizStats(flt), [flt]);
  if (s.shown < 2) return null;
  const sum = s.bands.reduce((a, b) => a + b, 0) || 1;
  const peak = Math.max(1, ...s.hours);
  return (
    <div className="space-y-4">
      <dl className="grid grid-cols-2 gap-2 text-sm">
        {[
          [t("map.viz.topSpeed"), fmtSpeed(s.top, units)],
          [t("map.viz.avgSpeed"), s.avgMoving > 0 ? fmtSpeed(s.avgMoving, units) : "–"],
          [t("map.viz.movingTime"), duration(s.movingMs)],
          [t("map.viz.stillTime"), duration(s.stillMs)],
        ].map(([k, v]) => (
          <div key={k} className="rounded-lg bg-surface-2 px-2.5 py-2">
            <dt className="text-[11px] text-subtle">{k}</dt>
            <dd className="font-medium tabular-nums">{v}</dd>
          </div>
        ))}
      </dl>

      <div>
        <p className="mb-1.5 text-xs font-medium text-subtle">{t("map.viz.timeBySpeed")}</p>
        <div className="flex h-2.5 overflow-hidden rounded-full bg-surface-3" role="img"
          aria-label={BANDS.map((b, i) => `${t(`map.viz.band.${b.key}`)} ${Math.round((s.bands[i] / sum) * 100)}%`).join(", ")}>
          {BANDS.map((b, i) => s.bands[i] > 0 && <span key={b.key} style={{ width: `${(s.bands[i] / sum) * 100}%`, background: b.color }} title={`${t(`map.viz.band.${b.key}`)} · ${duration(s.bands[i])}`} />)}
        </div>
        <ul className="mt-1.5 flex flex-wrap gap-x-3 gap-y-0.5 text-[11px] text-muted">
          {BANDS.map((b, i) => s.bands[i] > 0 && (
            <li key={b.key} className="flex items-center gap-1">
              <span className="size-2 rounded-full" style={{ background: b.color }} aria-hidden />
              {t(`map.viz.band.${b.key}`)} {Math.round((s.bands[i] / sum) * 100)}%
            </li>
          ))}
        </ul>
      </div>

      <div>
        <p className="mb-1.5 text-xs font-medium text-subtle">{t("map.viz.byHour")}</p>
        <div className="flex h-10 items-end gap-px" role="img" aria-label={t("map.viz.byHour")}>
          {s.hours.map((c, h) => (
            <span key={h} className="flex-1 rounded-t-sm bg-primary" style={{ height: `${Math.max(c ? 8 : 2, (c / peak) * 100)}%`, opacity: c ? 0.4 + 0.6 * (c / peak) : 0.15 }}
              title={`${hourLabel(h, clock)} · ${c}`} />
          ))}
        </div>
        <div className="mt-1 flex justify-between text-[10px] text-subtle tabular-nums"><span>{hourLabel(0, clock)}</span><span>{hourLabel(12, clock)}</span><span>{hourLabel(23, clock)}</span></div>
      </div>
    </div>
  );
}

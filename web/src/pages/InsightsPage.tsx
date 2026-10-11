import { useMemo, useState } from "react";
import { Link, useNavigate } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { addDays, endOfMonth, endOfYear, format, parseISO, startOfDay, startOfWeek, subDays } from "date-fns";
import { BarChart3, Building2, CalendarCheck2, ChevronRight, Clock, Globe2, MapPin, Route, Sparkles, Trophy, type LucideIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, type InsightPlace, type Insights } from "../lib/api";
import { dayKey, distance, duration, longDate, modeLabel, number } from "../lib/format";
import { tz, useDays, useMonthDays } from "../lib/data";
import { usePrefs } from "../lib/prefs";
import i18n from "../lib/i18n";
import { RangePicker, presetRange, type Range } from "../components/controls";
import { PlaceIcon } from "../components/places";
import { EmptyState, ErrorState, Skeleton, cn } from "../components/ui";
import { modeIcons } from "./TimelinePage";

export default function InsightsPage() {
  const { t } = useTranslation();
  const { units } = usePrefs();
  const [range, setRange] = useState<Range>(() => presetRange("thisMonth"));
  const [month, setMonth] = useState(new Date());
  const marked = useMonthDays(month);
  const q = useQuery({
    queryKey: ["insights", range.from, range.to],
    queryFn: () => api<Insights>(`/insights?from=${range.from}&to=${range.to}&tz=${encodeURIComponent(tz)}`),
  });
  const d = q.data;

  return (
    <div className="mx-auto w-full max-w-6xl px-4 py-6 md:px-8 md:py-10">
      <div className="mb-8 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{t("insights.title")}</h1>
          <p className="mt-1 text-sm text-muted">{t("insights.subtitle")}</p>
        </div>
        <div className="flex w-full flex-wrap gap-2 sm:w-auto">
          <Link to="/insights/recap" className="inline-flex h-10 items-center gap-2 rounded-lg border border-border-strong bg-surface px-4 text-sm font-medium hover:bg-surface-2">
            <Sparkles className="size-4 text-primary" aria-hidden />
            {t("recap.open")}
          </Link>
          <RangePicker label={t("range.label")} value={range} onChange={setRange} marked={marked} onMonthChange={setMonth} className="min-w-0 flex-1 sm:w-72 sm:flex-none" />
        </div>
      </div>

      {q.isError ? (
        <ErrorState error={q.error} retry={() => q.refetch()} />
      ) : !d ? (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">{Array.from({ length: 8 }, (_, i) => <Skeleton key={i} className="h-28" />)}</div>
      ) : d.days_tracked === 0 ? (
        <EmptyState icon={BarChart3} title={t("insights.emptyTitle")} body={t("insights.emptyBody")} />
      ) : (
        <div className="space-y-6">
          <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
            <Kpi icon={Route} label={t("summary.distance")} value={distance(d.distance, units)} />
            <Kpi icon={Clock} label={t("insights.movingTime")} value={duration(d.moving_ms)} />
            <Kpi icon={MapPin} label={t("insights.placesVisited")} value={number(d.places)} sub={t("insights.visitsCount", { count: d.visits })} />
            <Kpi icon={CalendarCheck2} label={t("insights.daysTracked")} value={number(d.days_tracked)} sub={t("insights.countriesCount", { count: d.countries.length })} />
          </div>

          <Card title={t("insights.distanceOverTime")} icon={BarChart3}>
            <Bars daily={d.daily} range={range} units={units} />
          </Card>

          <div className="grid gap-6 lg:grid-cols-2">
            <Card title={t("insights.howYouMoved")} icon={Route}>
              {d.modes.length === 0 ? <p className="text-sm text-muted">{t("insights.noTrips")}</p> : (
                <ul className="space-y-4">
                  {d.modes.map((m) => {
                    const Icon = modeIcons[m.mode];
                    const pct = d.distance ? (m.distance / d.distance) * 100 : 0;
                    return (
                      <li key={m.mode}>
                        <div className="mb-1.5 flex items-center gap-2.5 text-sm">
                          <span className="grid size-8 place-items-center rounded-lg bg-surface-2"><Icon className="size-4" aria-hidden /></span>
                          <span className="font-medium">{modeLabel(m.mode)}</span>
                          <span className="ml-auto text-muted tabular-nums">{distance(m.distance, units)} · {duration(m.ms)}</span>
                        </div>
                        <div className="h-2 overflow-hidden rounded-full bg-surface-2" role="img" aria-label={`${Math.round(pct)}%`}>
                          <div className="h-full rounded-full bg-primary" style={{ width: `${Math.max(pct, 1)}%` }} />
                        </div>
                      </li>
                    );
                  })}
                </ul>
              )}
              {d.longest_trip && (
                <div className="mt-5 flex items-center gap-3 rounded-xl bg-primary-subtle p-3 text-sm">
                  <Trophy className="size-5 shrink-0 text-primary" aria-hidden />
                  <span>
                    {t("insights.longestTrip", { distance: distance(d.longest_trip.distance, units), mode: modeLabel(d.longest_trip.mode).toLowerCase() })}{" "}
                    <Link to={`/timeline/${dayKey(d.longest_trip.start)}`} className="font-medium text-primary">{longDate(new Date(d.longest_trip.start))}</Link>
                  </span>
                </div>
              )}
            </Card>

            <Card title={t("insights.topPlaces")} icon={MapPin}>
              <ul className="space-y-3">
                {d.top_places.map((p, i) => (
                  <li key={i}>
                    <Link to={placeLink(p)} title={t("insights.openPlace")} className="-mx-2 flex items-center gap-3 rounded-xl px-2 py-1.5 transition-colors hover:bg-surface-2">
                    <span className={cn("grid size-9 shrink-0 place-items-center rounded-xl", p.place_id ? "bg-primary text-primary-fg" : "bg-surface-2 text-muted")}>
                      <PlaceIcon name={p.icon} className="size-4" />
                    </span>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-baseline justify-between gap-2">
                        <p className="truncate text-sm font-medium">{p.name || t("timeline.unknownPlace")}</p>
                        <p className="shrink-0 text-xs text-muted tabular-nums">{duration(p.ms)}</p>
                      </div>
                      <div className="mt-1 h-1.5 overflow-hidden rounded-full bg-surface-2">
                        <div className="h-full rounded-full bg-primary/70" style={{ width: `${(p.ms / d.top_places[0].ms) * 100}%` }} />
                      </div>
                    </div>
                    <ChevronRight className="size-4 shrink-0 text-subtle" aria-hidden />
                    </Link>
                  </li>
                ))}
              </ul>
            </Card>
          </div>

          <div className="grid gap-6 lg:grid-cols-2">
            <Card title={t("insights.countries")} icon={Globe2}>
              {d.countries.length === 0 ? <p className="text-sm text-muted">{t("insights.noPlaceNames")}</p> : (
                <ul className="flex flex-wrap gap-2">
                  {d.countries.map((c) => (
                    <li key={c.name}>
                      <Link to={`/timeline/${dayKey(c.last)}`} title={t("insights.openLast")} className="flex items-center gap-2 rounded-full border border-border py-1 pr-3 pl-1.5 text-sm transition-colors hover:border-primary hover:bg-primary-subtle">
                        <span className="text-base leading-none" aria-hidden>{flag(c.name)}</span>
                        {countryName(c.name)}
                        <span className="text-xs text-subtle">{duration(c.ms)}</span>
                      </Link>
                    </li>
                  ))}
                </ul>
              )}
            </Card>
            <Card title={t("insights.cities")} icon={Building2}>
              {d.cities.length === 0 ? <p className="text-sm text-muted">{t("insights.noPlaceNames")}</p> : (
                <ul className="flex flex-wrap gap-2">
                  {d.cities.map((c) => (
                    <li key={c.name + c.country}>
                      <Link to={`/timeline/${dayKey(c.last)}`} title={t("insights.openLast")} className="block rounded-full bg-surface-2 px-3 py-1 text-sm transition-colors hover:bg-primary-subtle hover:text-primary">
                        {c.name} <span className="text-xs text-subtle">· {t("insights.visitsCount", { count: c.visits })}</span>
                      </Link>
                    </li>
                  ))}
                </ul>
              )}
            </Card>
          </div>

          <Card title={t("insights.activity")} icon={CalendarCheck2}>
            <ActivityGrid />
          </Card>
        </div>
      )}
    </div>
  );
}

const regionNames = (() => {
  try {
    return new Intl.DisplayNames([i18n.language], { type: "region" });
  } catch {
    return null;
  }
})();
export const countryName = (code: string) => regionNames?.of(code) ?? code;
/** "GB" → 🇬🇧 (regional indicator letters). */
export const flag = (code: string) => (/^[A-Z]{2}$/i.test(code) ? String.fromCodePoint(...[...code.toUpperCase()].map((c) => 0x1f1a5 + c.charCodeAt(0))) : "🏳️");
/** Saved places open on the Places map; other spots open the day you were last there. */
export const placeLink = (p: InsightPlace) => (p.place_id ? `/places?place=${p.place_id}` : `/timeline/${dayKey(p.last)}`);

export function Kpi({ icon: Icon, label, value, sub }: { icon: LucideIcon; label: string; value: string; sub?: string }) {
  return (
    <div className="rounded-2xl border border-border bg-surface p-4">
      <div className="flex items-center gap-2 text-sm text-muted">
        <span className="grid size-7 place-items-center rounded-lg bg-primary-subtle text-primary"><Icon className="size-4" aria-hidden /></span>
        {label}
      </div>
      <p className="mt-3 text-2xl font-semibold tracking-tight tabular-nums">{value}</p>
      {sub && <p className="mt-0.5 text-xs text-subtle">{sub}</p>}
    </div>
  );
}

export function Card({ title, icon: Icon, children }: { title: string; icon: LucideIcon; children: React.ReactNode }) {
  return (
    <section className="rounded-2xl border border-border bg-surface p-5">
      <h2 className="mb-4 flex items-center gap-2 font-semibold"><Icon className="size-4 text-subtle" aria-hidden />{title}</h2>
      {children}
    </section>
  );
}

/** Distance per day, week or month depending on the range length. */
function Bars({ daily, range, units }: { daily: Insights["daily"]; range: Range; units: "metric" | "imperial" }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [hover, setHover] = useState<number | null>(null);
  const buckets = useMemo(() => {
    const byDay = new Map(daily.map((d) => [d.day, d]));
    const first = daily[0] ? parseISO(daily[0].day) : new Date(range.from);
    const start = startOfDay(range.preset === "all" ? first : new Date(Math.max(range.from, first.getTime() - 86400e3 * 3)));
    // Calendar periods show their full length so a few days in don't fill the chart.
    const end = startOfDay(range.preset === "thisMonth" ? endOfMonth(range.from) : range.preset === "thisYear" ? endOfYear(range.from) : new Date(Math.min(range.to, Date.now())));
    const days = Math.round((end.getTime() - start.getTime()) / 86400e3) + 1;
    const unit = days <= 62 ? "day" : days <= 400 ? "week" : "month";
    const out: { key: string; label: string; distance: number; day?: string }[] = [];
    for (let d = start; d <= end; d = addDays(d, 1)) {
      const k = unit === "day" ? format(d, "yyyy-MM-dd") : unit === "week" ? format(startOfWeek(d, { weekStartsOn: 1 }), "yyyy-MM-dd") : format(d, "yyyy-MM");
      if (out.at(-1)?.key !== k) {
        out.push({ key: k, day: unit === "day" ? k : undefined, distance: 0,
          label: unit === "month" ? d.toLocaleDateString(i18n.language, { month: "short", year: "2-digit" }) : d.toLocaleDateString(i18n.language, { day: "numeric", month: "short" }) });
      }
      out.at(-1)!.distance += byDay.get(format(d, "yyyy-MM-dd"))?.distance ?? 0;
    }
    return { out, unit };
  }, [daily, range]);
  const max = Math.max(1, ...buckets.out.map((b) => b.distance));
  const W = 100 / buckets.out.length;
  const h = hover != null ? buckets.out[hover] : null;
  return (
    <div>
      <div className="mb-2 h-5 text-sm text-muted tabular-nums" aria-live="polite">
        {h ? <><span className="font-medium text-fg">{h.label}</span> · {distance(h.distance, units)}</> : t(`insights.per.${buckets.unit}`)}
      </div>
      <svg viewBox="0 0 100 40" preserveAspectRatio="none" className="h-44 w-full" role="img" aria-label={t("insights.distanceOverTime")} onPointerLeave={() => setHover(null)}>
        {[0.25, 0.5, 0.75].map((y) => <line key={y} x1="0" x2="100" y1={40 * y} y2={40 * y} stroke="var(--border)" strokeWidth="0.15" vectorEffect="non-scaling-stroke" />)}
        {buckets.out.map((b, i) => {
          const bh = (b.distance / max) * 38;
          return (
            <g key={b.key} onPointerEnter={() => setHover(i)} onClick={() => b.day && navigate(`/timeline/${b.day}`)} className={b.day ? "cursor-pointer" : undefined}>
              <rect x={i * W} y="0" width={W} height="40" fill="transparent" />
              <rect x={i * W + W * 0.15} y={40 - Math.max(bh, b.distance ? 0.6 : 0.3)} width={W * 0.7} height={Math.max(bh, b.distance ? 0.6 : 0.3)} rx="0.4"
                fill={hover === i ? "var(--primary-hover)" : b.distance ? "var(--primary)" : "var(--surface-3)"} />
            </g>
          );
        })}
      </svg>
      <div className="mt-1 flex justify-between text-[11px] text-subtle">
        <span>{buckets.out[0]?.label}</span>
        <span>{buckets.out.at(-1)?.label}</span>
      </div>
    </div>
  );
}

/** A year of tracked days, GitHub-style. Click a day to open it. */
function ActivityGrid() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const end = startOfDay(new Date());
  const start = startOfWeek(subDays(end, 364), { weekStartsOn: 1 });
  const days = useDays(start.getTime(), end.getTime() + 86400e3 - 1).data ?? {};
  const max = Math.max(1, ...Object.values(days));
  const weeks: Date[][] = [];
  for (let d = start; d <= end; d = addDays(d, 1)) {
    if (weeks.length === 0 || weeks.at(-1)!.length === 7) weeks.push([]);
    weeks.at(-1)!.push(d);
  }
  const level = (n: number) => (n ? Math.min(4, 1 + Math.floor((Math.log(n) / Math.log(max + 1)) * 4)) : 0);
  const shades = ["bg-surface-2", "bg-primary/25", "bg-primary/45", "bg-primary/70", "bg-primary"];
  return (
    <div>
      <div className="overflow-x-auto pb-2">
        <div className="flex w-max gap-[3px]" role="grid" aria-label={t("insights.activity")}>
          {weeks.map((w, i) => (
            <div key={i} className="flex flex-col gap-[3px]" role="row">
              {w.map((d) => {
                const k = format(d, "yyyy-MM-dd");
                const n = days[k] ?? 0;
                return (
                  <button
                    key={k}
                    role="gridcell"
                    title={`${longDate(d)}: ${n ? t("insights.pointsCount", { count: n }) : t("insights.noData")}`}
                    aria-label={`${longDate(d)}: ${n ? t("insights.pointsCount", { count: n }) : t("insights.noData")}`}
                    onClick={() => n && navigate(`/timeline/${k}`)}
                    className={cn("size-3 rounded-[3px] transition-transform hover:scale-125", shades[level(n)], !n && "cursor-default")}
                  />
                );
              })}
            </div>
          ))}
        </div>
      </div>
      <div className="mt-2 flex items-center justify-end gap-1.5 text-[11px] text-subtle">
        {t("insights.less")}
        {shades.map((s) => <span key={s} className={cn("size-3 rounded-[3px]", s)} />)}
        {t("insights.more")}
      </div>
    </div>
  );
}

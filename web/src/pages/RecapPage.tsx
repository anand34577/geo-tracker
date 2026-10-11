import { useMemo } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { addMonths, format, isValid, parse, startOfMonth, subMonths } from "date-fns";
import { ArrowDownRight, ArrowUpRight, CalendarDays, ChevronLeft, ChevronRight, Clock, Flame, MapPin, Route, Sparkles, Trophy, type LucideIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, type Insights } from "../lib/api";
import { dayKey, distance, duration, longDate, modeLabel, number } from "../lib/format";
import { toSegments, tz, usePoints } from "../lib/data";
import { usePrefs } from "../lib/prefs";
import i18n from "../lib/i18n";
import MapView from "../components/MapView";
import { PlaceIcon } from "../components/places";
import { Button, EmptyState, ErrorState, Skeleton, cn } from "../components/ui";
import { Card, countryName, flag, placeLink } from "./InsightsPage";
import { modeIcons } from "./TimelinePage";

const monthRange = (m: Date): [number, number] => [startOfMonth(m).getTime(), startOfMonth(addMonths(m, 1)).getTime() - 1];
const useMonthInsights = (m: Date, withNew: boolean) => {
  const [from, to] = monthRange(m);
  return useQuery({
    queryKey: ["insights", from, to, withNew],
    queryFn: () => api<Insights>(`/insights?from=${from}&to=${to}&tz=${encodeURIComponent(tz)}${withNew ? "&new=1" : ""}`),
  });
};

/** A month in review, like the recap emails of Google Timeline: totals, favourites and what was new. */
export default function RecapPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { units } = usePrefs();
  const param = useParams().month;
  const parsed = param ? parse(param, "yyyy-MM", new Date()) : null;
  // Default to last month once this one is a week old, so the 1st doesn't open an empty page.
  const month = startOfMonth(parsed && isValid(parsed) ? parsed : new Date().getDate() >= 7 ? new Date() : subMonths(new Date(), 1));
  const isCurrent = startOfMonth(new Date()).getTime() === month.getTime();
  const go = (m: Date) => navigate(`/insights/recap/${format(m, "yyyy-MM")}`);

  const q = useMonthInsights(month, true);
  const prev = useMonthInsights(subMonths(month, 1), false);
  const [from, to] = monthRange(month);
  const points = usePoints(from, to);
  const path = useMemo(() => toSegments(points.data?.points ?? []), [points.data]);
  const d = q.data;
  const title = month.toLocaleDateString(i18n.language, { month: "long", year: "numeric" });

  const busiest = d?.daily.reduce<Insights["daily"][number] | null>((best, x) => (!best || x.distance > best.distance ? x : best), null);
  const total = d ? d.modes.reduce((s, m) => s + m.distance, 0) : 0;

  return (
    <div className="mx-auto w-full max-w-5xl px-4 py-6 md:px-8 md:py-10">
      <div className="mb-6 flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="flex items-center gap-1.5 text-sm font-medium text-primary"><Sparkles className="size-4" aria-hidden />{t("recap.kicker")}</p>
          <h1 className="text-2xl font-semibold tracking-tight md:text-3xl">{t("recap.title", { month: title })}</h1>
        </div>
        <div className="flex items-center gap-2">
          <Button size="icon" onClick={() => go(subMonths(month, 1))} aria-label={t("recap.prev")} title={t("recap.prev")}><ChevronLeft className="size-5" /></Button>
          <Button size="icon" onClick={() => go(addMonths(month, 1))} disabled={isCurrent} aria-label={t("recap.next")} title={t("recap.next")}><ChevronRight className="size-5" /></Button>
        </div>
      </div>

      {q.isError ? (
        <ErrorState error={q.error} retry={() => q.refetch()} />
      ) : !d ? (
        <div className="space-y-4"><Skeleton className="h-48" /><div className="grid gap-4 md:grid-cols-2"><Skeleton className="h-64" /><Skeleton className="h-64" /></div></div>
      ) : d.days_tracked === 0 ? (
        <div className="rounded-2xl border border-dashed border-border-strong">
          <EmptyState icon={CalendarDays} title={t("recap.emptyTitle", { month: title })} body={t("recap.emptyBody")}
            action={<Button onClick={() => go(subMonths(month, 1))} icon={ChevronLeft}>{t("recap.prev")}</Button>} />
        </div>
      ) : (
        <div className="space-y-6">
          {/* Hero: the month in four numbers, compared with the month before. */}
          <section className="relative overflow-hidden rounded-3xl bg-primary p-6 text-primary-fg md:p-8">
            <div className="pointer-events-none absolute -top-24 -right-16 size-72 rounded-full bg-white/10 blur-2xl" aria-hidden />
            <div className="pointer-events-none absolute -bottom-28 -left-10 size-64 rounded-full bg-black/10 blur-2xl" aria-hidden />
            <p className="relative max-w-xl text-lg font-medium md:text-xl">
              {t("recap.headline", { distance: distance(d.distance, units), places: d.places, days: d.days_tracked })}
            </p>
            <dl className="relative mt-6 grid grid-cols-2 gap-4 md:grid-cols-4">
              <HeroStat icon={Route} label={t("summary.distance")} value={distance(d.distance, units)} now={d.distance} before={prev.data?.distance} />
              <HeroStat icon={Clock} label={t("insights.movingTime")} value={duration(d.moving_ms)} now={d.moving_ms} before={prev.data?.moving_ms} />
              <HeroStat icon={MapPin} label={t("insights.placesVisited")} value={number(d.places)} now={d.places} before={prev.data?.places} />
              <HeroStat icon={CalendarDays} label={t("insights.daysTracked")} value={number(d.days_tracked)} now={d.days_tracked} before={prev.data?.days_tracked} />
            </dl>
          </section>

          <div className="grid gap-6 lg:grid-cols-5">
            <section className="overflow-hidden rounded-2xl border border-border bg-surface lg:col-span-3">
              <MapView className="h-80 lg:h-full lg:min-h-96" label={t("recap.mapLabel", { month: title })} path={path} fit={points.isSuccess ? `recap${from}` : undefined} />
            </section>
            <div className="space-y-6 lg:col-span-2">
              <Card title={t("recap.highlights")} icon={Trophy}>
                <ul className="space-y-3 text-sm">
                  {d.top_places[0] && (
                    <Highlight icon={Flame} label={t("recap.favourite")} to={placeLink(d.top_places[0])}
                      value={d.top_places[0].name || t("timeline.unknownPlace")} sub={duration(d.top_places[0].ms)} />
                  )}
                  {busiest && busiest.distance > 0 && (
                    <Highlight icon={Route} label={t("recap.busiest")} to={`/timeline/${busiest.day}`} value={longDate(new Date(busiest.day + "T12:00"))} sub={distance(busiest.distance, units)} />
                  )}
                  {d.longest_trip && (
                    <Highlight icon={Trophy} label={t("recap.longest")} to={`/timeline/${dayKey(d.longest_trip.start)}`}
                      value={`${distance(d.longest_trip.distance, units)} · ${modeLabel(d.longest_trip.mode)}`} sub={longDate(new Date(d.longest_trip.start))} />
                  )}
                </ul>
              </Card>
              {!!d.modes.length && (
                <Card title={t("insights.howYouMoved")} icon={Route}>
                  <div className="flex h-3 overflow-hidden rounded-full bg-surface-2" role="img" aria-label={d.modes.map((m) => `${modeLabel(m.mode)} ${Math.round((m.distance / (total || 1)) * 100)}%`).join(", ")}>
                    {d.modes.map((m, i) => (
                      <span key={m.mode} className="h-full" style={{ width: `${(m.distance / (total || 1)) * 100}%`, background: `color-mix(in srgb, var(--primary) ${100 - i * 22}%, var(--surface-3))` }} />
                    ))}
                  </div>
                  <ul className="mt-4 space-y-2 text-sm">
                    {d.modes.map((m, i) => {
                      const I = modeIcons[m.mode];
                      return (
                        <li key={m.mode} className="flex items-center gap-2.5">
                          <span className="size-2.5 rounded-full" style={{ background: `color-mix(in srgb, var(--primary) ${100 - i * 22}%, var(--surface-3))` }} aria-hidden />
                          <I className="size-4 text-subtle" aria-hidden />
                          <span className="font-medium">{modeLabel(m.mode)}</span>
                          <span className="ml-auto text-muted tabular-nums">{distance(m.distance, units)} · {Math.round((m.distance / (total || 1)) * 100)}%</span>
                        </li>
                      );
                    })}
                  </ul>
                </Card>
              )}
            </div>
          </div>

          {d.new && d.new.places.length + d.new.cities.length + d.new.countries.length > 0 && (
            <Card title={t("recap.newTitle")} icon={Sparkles}>
              <p className="-mt-2 mb-4 text-sm text-muted">{t("recap.newBody", { count: d.new.places.length + d.new.cities.length + d.new.countries.length })}</p>
              <ul className="flex flex-wrap gap-2">
                {d.new.countries.map((c) => (
                  <li key={"c" + c.name}><Chip to={`/timeline/${dayKey(c.first)}`} icon={<span aria-hidden>{flag(c.name)}</span>} label={countryName(c.name)} strong /></li>
                ))}
                {d.new.cities.map((c) => (
                  <li key={"t" + c.name + c.country}><Chip to={`/timeline/${dayKey(c.first)}`} icon={<MapPin className="size-3.5" aria-hidden />} label={c.name} /></li>
                ))}
                {d.new.places.map((p) => (
                  <li key={"p" + p.name + p.lat}><Chip to={placeLink(p)} icon={<PlaceIcon name={p.icon} className="size-3.5" />} label={p.name} /></li>
                ))}
              </ul>
            </Card>
          )}

          <Card title={t("insights.topPlaces")} icon={MapPin}>
            <ol className="grid gap-x-8 gap-y-1 md:grid-cols-2">
              {d.top_places.slice(0, 8).map((p, i) => (
                <li key={i}>
                  <Link to={placeLink(p)} className="-mx-2 flex items-center gap-3 rounded-xl px-2 py-2 transition-colors hover:bg-surface-2">
                    <span className="w-5 text-right text-sm font-semibold text-subtle tabular-nums">{i + 1}</span>
                    <span className={cn("grid size-9 shrink-0 place-items-center rounded-xl", p.place_id ? "bg-primary text-primary-fg" : "bg-surface-2 text-muted")}>
                      <PlaceIcon name={p.icon} className="size-4" />
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-sm font-medium">{p.name || t("timeline.unknownPlace")}</span>
                      <span className="block text-xs text-muted">{t("insights.visitsCount", { count: p.visits })} · {duration(p.ms)}</span>
                    </span>
                    <ChevronRight className="size-4 shrink-0 text-subtle" aria-hidden />
                  </Link>
                </li>
              ))}
            </ol>
          </Card>
        </div>
      )}
    </div>
  );
}

function HeroStat({ icon: Icon, label, value, now, before }: { icon: LucideIcon; label: string; value: string; now: number; before?: number }) {
  const { t } = useTranslation();
  const pct = before ? Math.round(((now - before) / before) * 100) : null;
  return (
    <div className="rounded-2xl bg-white/12 p-3.5 backdrop-blur-sm">
      <dt className="flex items-center gap-1.5 text-xs font-medium opacity-80"><Icon className="size-3.5" aria-hidden />{label}</dt>
      <dd className="mt-1 text-2xl font-semibold tracking-tight tabular-nums">{value}</dd>
      {pct != null && pct !== 0 && (
        <dd className="mt-0.5 flex items-center gap-0.5 text-xs font-medium opacity-90" title={t("recap.vsLast")}>
          {pct > 0 ? <ArrowUpRight className="size-3.5" aria-hidden /> : <ArrowDownRight className="size-3.5" aria-hidden />}
          {t("recap.change", { pct: Math.abs(pct) })}
        </dd>
      )}
    </div>
  );
}

function Highlight({ icon: Icon, label, value, sub, to }: { icon: LucideIcon; label: string; value: string; sub: string; to: string }) {
  return (
    <li>
      <Link to={to} className="-mx-2 flex items-center gap-3 rounded-xl px-2 py-1.5 transition-colors hover:bg-surface-2">
        <span className="grid size-9 shrink-0 place-items-center rounded-xl bg-primary-subtle text-primary"><Icon className="size-4" aria-hidden /></span>
        <span className="min-w-0 flex-1">
          <span className="block text-xs text-subtle">{label}</span>
          <span className="block truncate font-medium">{value}</span>
        </span>
        <span className="shrink-0 text-xs text-muted tabular-nums">{sub}</span>
      </Link>
    </li>
  );
}

function Chip({ to, icon, label, strong }: { to: string; icon: React.ReactNode; label: string; strong?: boolean }) {
  return (
    <Link to={to} className={cn("flex items-center gap-1.5 rounded-full border px-3 py-1.5 text-sm transition-colors hover:border-primary hover:bg-primary-subtle",
      strong ? "border-primary/40 bg-primary-subtle font-medium" : "border-border")}>
      {icon}
      {label}
    </Link>
  );
}

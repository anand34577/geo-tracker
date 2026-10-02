import { useEffect, useMemo, useRef, useState } from "react";
import { useLocation, useNavigate, useParams, useSearchParams } from "react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { addDays, isValid, parseISO } from "date-fns";
import { Bike, Car, ChevronLeft, ChevronRight, CircleHelp, Footprints, History, MapPinned, Plane, Plus, TrainFront, type LucideIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, queryClient, useUser, type PointStats, type Trip, type TripMode, type Visit } from "../lib/api";
import { dayBounds, dayKey, distance, duration, longDate, modeLabel, time } from "../lib/format";
import { summarize, toSegments, useFamily, useMonthDays, usePoints, useTimeline } from "../lib/data";
import { usePrefs } from "../lib/prefs";
import MapView from "../components/MapView";
import { DatePicker, Popover, RangePicker, Select, type Range } from "../components/controls";
import { PlaceDialog, PlaceIcon, type PlaceDraft } from "../components/places";
import { PhotoStrip, usePhotos } from "../components/Photos";
import { Avatar, Button, cn, EmptyState, ErrorState, Notice, Segmented, Skeleton } from "../components/ui";

export const modeIcons: Record<TripMode, LucideIcon> = { walk: Footprints, cycle: Bike, drive: Car, train: TrainFront, flight: Plane, unknown: CircleHelp };

type Entry = { kind: "visit"; v: Visit; start: number } | { kind: "trip"; t: Trip; start: number };

export default function TimelinePage() {
  const { t } = useTranslation();
  const { units, clock } = usePrefs();
  const navigate = useNavigate();
  const location = useLocation();
  const me = useUser();
  const params = useParams();
  const [search] = useSearchParams();
  const todayKey = dayKey(new Date());

  // Two modes: a single day (/timeline/2026-09-30) or any range, with times (?from=&to=).
  const qFrom = Number(search.get("from")), qTo = Number(search.get("to"));
  const rangeMode = qFrom > 0 && qTo > qFrom;
  const day = params.date && isValid(parseISO(params.date)) ? params.date : todayKey;
  const [from, to] = rangeMode ? [qFrom, qTo] : dayBounds(day);

  const [month, setMonth] = useState(parseISO(day));
  const marked = useMonthDays(month);
  // Viewing a family member (?user=); the server limits this to what they share.
  const viewUser = Number(search.get("user")) || undefined;
  const family = useFamily();
  const viewable = (family.data ?? []).filter((p) => p.history_from != null);
  const viewed = viewable.find((p) => p.user_id === viewUser);
  const userQs = viewUser ? `user=${viewUser}` : "";
  const points = usePoints(from, to, viewUser);
  const timeline = useTimeline(from, to, viewUser);
  const photos = usePhotos(from, to, !viewUser);
  const stats = useQuery({ queryKey: ["stats"], queryFn: () => api<PointStats>("/stats") });
  const [active, setActive] = useState<number | null>(null);
  const [focus, setFocus] = useState<{ lon: number; lat: number; key: number } | null>(null);
  const [draft, setDraft] = useState<PlaceDraft | null>(null);
  const listRef = useRef<HTMLDivElement>(null);

  const goDay = (key: string) => navigate(`/timeline/${key}${userQs ? "?" + userQs : ""}`);
  const shift = (n: number) => goDay(dayKey(addDays(parseISO(day), n)));
  const setRange = (r: Range) => navigate(`/timeline?from=${r.from}&to=${r.to}${userQs ? "&" + userQs : ""}`);
  const setViewUser = (id: string) => {
    const q = new URLSearchParams(search);
    if (id === "me") q.delete("user");
    else q.set("user", id);
    navigate(`${location.pathname}?${q}`);
  };

  useEffect(() => setActive(null), [from, to]);
  useEffect(() => {
    if (rangeMode) return;
    const onKey = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement;
      if (el.closest("input, textarea, [role=grid], [role=slider], [role=spinbutton], [role=radiogroup]") || e.metaKey || e.ctrlKey) return;
      if (e.key === "ArrowLeft") shift(-1);
      if (e.key === "ArrowRight" && day < todayKey) shift(1);
      if (e.key === "t") goDay(todayKey);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  // Entries grouped by local day; in day mode that's a single group.
  const groups = useMemo(() => {
    const d = timeline.data;
    if (!d) return [];
    const entries: Entry[] = [
      ...d.visits.map((v) => ({ kind: "visit" as const, v, start: Math.max(v.start, from) })),
      ...d.trips.map((tr) => ({ kind: "trip" as const, t: tr, start: tr.start })),
    ].sort((a, b) => a.start - b.start);
    const out: { day: string; entries: Entry[] }[] = [];
    for (const e of entries) {
      const k = dayKey(e.start);
      if (out.at(-1)?.day !== k) out.push({ day: k, entries: [] });
      out.at(-1)!.entries.push(e);
    }
    return out;
  }, [timeline.data, from]);

  const path = useMemo(() => toSegments(points.data?.points ?? []), [points.data]);
  const sum = summarize(timeline.data);

  const selectVisit = (v: Visit, fromMap = false) => {
    setActive(v.id);
    setFocus({ lon: v.lon, lat: v.lat, key: Date.now() });
    if (fromMap) listRef.current?.querySelector(`[data-visit="${v.id}"]`)?.scrollIntoView({ block: "nearest", behavior: "smooth" });
  };

  const loading = timeline.isPending || points.isPending;
  const empty = !loading && groups.length === 0 && path.length === 0;

  return (
    <div className="absolute inset-0 flex flex-col md:flex-row">
      <div className="order-2 flex min-h-0 flex-1 flex-col bg-surface md:order-1 md:w-[420px] md:flex-none md:border-r md:border-border">
        <div className="space-y-3 border-b border-border p-4">
          {viewable.length > 0 && (
            <Select
              label={t("timeline.whose")}
              value={viewUser ? String(viewUser) : "me"}
              onChange={setViewUser}
              options={[
                { value: "me", label: t("timeline.mine"), icon: <Avatar name={me.name} className="size-6 text-[10px]" /> },
                ...viewable.map((p) => ({ value: String(p.user_id), label: p.name, hint: p.approx ? t("family.approxOnly") : undefined, icon: <Avatar name={p.name} color={p.color} className="size-6 text-[10px]" /> })),
              ]}
            />
          )}
          {viewed && viewed.history_from! > from && (
            <Notice tone="info">{t("timeline.limitedHistory", { name: viewed.name, date: longDate(new Date(viewed.history_from!)) })}</Notice>
          )}
          <div className="flex items-center justify-between gap-3">
            <h1 className="text-lg font-semibold tracking-tight">{viewed ? t("timeline.whoseTitle", { name: viewed.name }) : t("nav.timeline")}</h1>
            <Segmented
              label={t("timeline.view")}
              value={rangeMode ? "range" : "day"}
              onChange={(v) => (v === "day" ? goDay(dayKey(from)) : setRange({ from, to }))}
              options={[{ value: "day", label: t("timeline.dayView") }, { value: "range", label: t("timeline.rangeView") }]}
            />
          </div>
          {rangeMode ? (
            <RangePicker label={t("range.label")} value={{ from, to }} onChange={setRange} marked={marked} onMonthChange={setMonth} />
          ) : (
            <div className="flex items-center gap-2">
              <Button variant="secondary" size="icon" onClick={() => shift(-1)} aria-label={t("timeline.prevDay")} title="←"><ChevronLeft className="size-5" /></Button>
              <DatePicker label={t("timeline.pickDay")} value={parseISO(day)} onChange={(d) => goDay(dayKey(d))} marked={marked} onMonthChange={setMonth} maxDate={new Date()} className="flex-1" />
              <Button variant="secondary" size="icon" onClick={() => shift(1)} disabled={day >= todayKey} aria-label={t("timeline.nextDay")} title="→"><ChevronRight className="size-5" /></Button>
            </div>
          )}
          <div className="flex items-baseline justify-between gap-2">
            <p className="truncate text-sm font-medium">{rangeMode ? t("timeline.rangeTitle") : day === todayKey ? t("timeline.today") : longDate(parseISO(day))}</p>
            {!rangeMode && day !== todayKey && <button className="shrink-0 text-sm font-medium text-primary" onClick={() => goDay(todayKey)}>{t("timeline.today")}</button>}
          </div>
          {!empty && (
            <div className="grid grid-cols-3 gap-2">
              {loading
                ? [0, 1, 2].map((i) => <Skeleton key={i} className="h-14" />)
                : [
                    [t("summary.distance"), distance(sum.distance, units)],
                    [t("summary.places"), String(sum.places)],
                    [t("summary.moving"), duration(sum.moving)],
                  ].map(([k, v]) => (
                    <div key={k} className="rounded-xl bg-surface-2 px-3 py-2">
                      <p className="text-[11px] text-subtle">{k}</p>
                      <p className="font-semibold tabular-nums">{v}</p>
                    </div>
                  ))}
            </div>
          )}
        </div>

        <div ref={listRef} className="min-h-0 flex-1 overflow-y-auto">
          {!viewUser && photos.data && <PhotoStrip photos={photos.data} clock={clock} onLocate={(p) => setFocus({ lon: p.lon!, lat: p.lat!, key: Date.now() })} />}
          {timeline.isError ? (
            <ErrorState error={timeline.error} retry={() => timeline.refetch()} />
          ) : loading ? (
            <div className="space-y-3 p-4" aria-busy>{[0, 1, 2, 3].map((i) => <Skeleton key={i} className="h-16" />)}</div>
          ) : empty ? (
            <EmptyState
              icon={History}
              title={t("timeline.emptyTitle")}
              body={stats.data?.count ? t("timeline.emptyBodyHasData") : t("timeline.emptyBody")}
              action={stats.data?.last ? <Button size="sm" onClick={() => goDay(dayKey(stats.data!.last!))}>{t("timeline.jumpLatest")}</Button> : undefined}
            />
          ) : groups.length === 0 ? (
            <p className="p-6 text-sm text-muted">{t("timeline.processing")}</p>
          ) : (
            groups.map((g) => (
              <section key={g.day} aria-label={longDate(parseISO(g.day))}>
                {rangeMode && (
                  <button onClick={() => goDay(g.day)} className="sticky top-0 z-[1] flex w-full items-center justify-between border-b border-border bg-surface/95 px-4 py-2 text-left backdrop-blur-sm hover:text-primary">
                    <span className="text-sm font-semibold">{longDate(parseISO(g.day))}</span>
                    <span className="text-xs text-muted tabular-nums">
                      {distance(g.entries.reduce((s, e) => s + (e.kind === "trip" ? e.t.distance : 0), 0), units)}
                    </span>
                  </button>
                )}
                <ol className="p-3">
                  {g.entries.map((e) =>
                    e.kind === "visit" ? (
                      <VisitItem key={`v${e.v.id}-${g.day}`} v={e.v} bounds={rangeMode ? dayBounds(g.day) : [from, to]} active={active === e.v.id} clock={clock}
                        onSelect={() => selectVisit(e.v)} onSave={() => setDraft({ lat: e.v.lat, lon: e.v.lon, name: e.v.name })} />
                    ) : (
                      <TripItem key={`t${e.t.id}`} trip={e.t} units={units} clock={clock} editable={!viewUser} />
                    ),
                  )}
                </ol>
              </section>
            ))
          )}
        </div>
      </div>

      <MapView
        className="order-1 h-[40dvh] shrink-0 md:order-2 md:h-auto md:flex-1"
        label={t("timeline.mapLabel")}
        path={path}
        visits={timeline.data?.visits.map((v) => ({ id: v.id, lon: v.lon, lat: v.lat, active: v.id === active }))}
        fit={loading ? undefined : `${from}-${to}`}
        focus={focus}
        onVisitClick={(id) => {
          const v = timeline.data?.visits.find((x) => x.id === id);
          if (v) selectVisit(v, true);
        }}
      />
      <PlaceDialog draft={draft} onClose={() => setDraft(null)} />
    </div>
  );
}

function VisitItem({ v, bounds, active, clock, onSelect, onSave }: { v: Visit; bounds: [number, number]; active: boolean; clock: "24h" | "12h"; onSelect: () => void; onSave: () => void }) {
  const { t } = useTranslation();
  const title = v.place_name || v.name || t("timeline.unknownPlace");
  // Skip parts already shown: the title, or a city that is already in the address.
  const sub = [v.address, v.city].filter((x, i, a) => x && x !== title && !(i > 0 && a[0]?.includes(x))).join(" · ");
  // Clip to the day shown, like a paper diary: a night at home reads 00:00 – 07:50.
  const start = Math.max(v.start, bounds[0]);
  const end = Math.min(v.end, bounds[1]);
  return (
    <li data-visit={v.id}>
      <div className={cn("group flex gap-3 rounded-xl p-3 transition-colors", active ? "bg-primary-subtle" : "hover:bg-surface-2")}>
        <button onClick={onSelect} className="flex min-w-0 flex-1 items-start gap-3 text-left" aria-pressed={active}>
          <span className={cn("grid size-10 shrink-0 place-items-center rounded-xl", v.place_id ? "bg-primary text-primary-fg" : "bg-surface-3 text-muted")}>
            <PlaceIcon name={v.place_icon} className="size-5" />
          </span>
          <span className="min-w-0 flex-1">
            <span className="block truncate font-medium">{title}</span>
            {sub && <span className="block truncate text-sm text-muted">{sub}</span>}
            <span className="mt-0.5 block text-sm text-subtle tabular-nums">
              {time(start, clock)} – {end === bounds[1] ? "24:00" : time(end, clock)} · {duration(end - start)}
            </span>
          </span>
        </button>
        {!v.place_id && (
          <Button variant="ghost" size="icon" className="size-9 md:opacity-0 md:group-hover:opacity-100 md:focus:opacity-100" onClick={onSave} aria-label={t("timeline.saveAsPlace")} title={t("timeline.saveAsPlace")}>
            <Plus className="size-4" />
          </Button>
        )}
      </div>
    </li>
  );
}

function TripItem({ trip, units, clock, editable }: { trip: Trip; units: "metric" | "imperial"; clock: "24h" | "12h"; editable: boolean }) {
  const { t } = useTranslation();
  const Icon = modeIcons[trip.mode] ?? MapPinned;
  const btn = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const setMode = useMutation({
    mutationFn: (mode: TripMode) => api("/trips/mode", { method: "PUT", body: { start: trip.start, mode } }),
    onSuccess: () => ["timeline", "insights"].forEach((k) => queryClient.invalidateQueries({ queryKey: [k] })),
  });
  return (
    <li className="flex items-center gap-3 py-1.5 pr-3 pl-[1.6rem] text-sm text-muted">
      <span className="flex h-8 w-px self-stretch border-l-2 border-dashed border-border-strong" aria-hidden />
      <span className="ml-2 grid size-7 shrink-0 place-items-center rounded-lg bg-surface-2"><Icon className="size-4" aria-hidden /></span>
      <span className="min-w-0 truncate">
        {editable ? (
          <button ref={btn} onClick={() => setOpen(!open)} aria-expanded={open} title={t("timeline.changeMode")}
            className="rounded px-0.5 font-medium text-fg underline decoration-border-strong decoration-dotted underline-offset-4 hover:decoration-primary">
            {modeLabel(trip.mode)}
          </button>
        ) : (
          <span className="font-medium text-fg">{modeLabel(trip.mode)}</span>
        )}
        {" "}· {distance(trip.distance, units)} · {duration(trip.end - trip.start)}
      </span>
      <Popover anchor={btn} open={open} onClose={() => setOpen(false)} className="w-48 p-1.5">
        <p className="px-2.5 pt-1 pb-1.5 text-xs font-semibold tracking-wide text-subtle uppercase">{t("timeline.changeMode")}</p>
        {(Object.keys(modeIcons) as TripMode[]).map((m) => {
          const MIcon = modeIcons[m];
          return (
            <button key={m} onClick={() => { setOpen(false); setMode.mutate(m); }} className={cn("flex h-9 w-full items-center gap-2.5 rounded-lg px-2.5 text-left text-sm hover:bg-surface-2", m === trip.mode && "text-primary")}>
              <MIcon className="size-4" aria-hidden />
              {modeLabel(m)}
            </button>
          );
        })}
      </Popover>
      <span className="ml-auto shrink-0 text-xs text-subtle tabular-nums">{time(trip.start, clock)}</span>
    </li>
  );
}

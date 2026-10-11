import { useEffect, useMemo, useRef, useState } from "react";
import { useLocation, useNavigate, useParams, useSearchParams } from "react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { addDays, isValid, parseISO } from "date-fns";
import {
  Bike, Car, ChevronDown, ChevronLeft, ChevronRight, CircleHelp, Footprints, Gauge, History, MapPinned, Merge, MoreHorizontal, Pencil, Plane, Plus, Split,
  TrainFront, Trash2, TrendingDown, TrendingUp, type LucideIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, queryClient, useUser, type PointStats, type Trip, type TripMode, type Visit } from "../lib/api";
import { dayBounds, dayKey, distance, duration, longDate, modeLabel, speed, time } from "../lib/format";
import { summarize, toSegments, tripStats, useFamily, useMonthDays, usePlaces, usePoints, useTimeline, type TripStats } from "../lib/data";
import { usePrefs } from "../lib/prefs";
import MapView from "../components/MapView";
import { DatePicker, Popover, RangePicker, Select, type Range } from "../components/controls";
import { PlaceDialog, PlaceIcon, type PlaceDraft } from "../components/places";
import { PhotoGallery, photoUrl, usePhotos, type Photo } from "../components/Photos";
import { AreaChart } from "../components/Chart";
import { Avatar, Button, cn, Dialog, EmptyState, ErrorState, Field, Input, Notice, Segmented, Skeleton, useToast } from "../components/ui";

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
  const pointStats = useQuery({ queryKey: ["stats"], queryFn: () => api<PointStats>("/stats") });
  const [active, setActive] = useState<number | null>(null);
  const [activeTrip, setActiveTrip] = useState<number | null>(null);
  const [editing, setEditing] = useState<Visit | null>(null);
  const editVisit = useVisitEdit();
  const [focus, setFocus] = useState<{ lon: number; lat: number; zoom?: number; key: number } | null>(null);
  const [located, setLocated] = useState<(Photo & { key: number }) | null>(null); // photo whose spot is highlighted on the map
  // pinStart: the visit to pin to the place once it's saved (from the edit dialog).
  const [draft, setDraft] = useState<(PlaceDraft & { pinStart?: number }) | null>(null);
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

  useEffect(() => {
    setActive(null);
    setActiveTrip(null);
    setLocated(null);
  }, [from, to]);
  const locatePhoto = (p: Photo) => {
    setLocated({ ...p, key: Date.now() });
    setFocus({ lon: p.lon!, lat: p.lat!, zoom: 17, key: Date.now() });
  };
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
      // Clamped like visits: a trip that began before midnight belongs to the day shown, not a stray group.
      ...d.trips.map((tr) => ({ kind: "trip" as const, t: tr, start: Math.max(tr.start, from) })),
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

  const rows = points.data?.points;
  const trip = timeline.data?.trips.find((x) => x.id === activeTrip);
  const stats = useMemo(() => (trip && rows ? tripStats(rows, trip.start, trip.end) : null), [trip, rows]);
  const toggleTrip = (id: number) => {
    setActive(null);
    setActiveTrip((cur) => (cur === id ? null : id));
  };
  const visits = timeline.data?.visits ?? [];
  const nextOf = (v: Visit) => visits[visits.findIndex((x) => x.id === v.id) + 1];

  const selectVisit = (v: Visit, fromMap = false) => {
    setActiveTrip(null);
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
          {!viewUser && photos.data && <PhotoGallery photos={photos.data} clock={clock} onLocate={locatePhoto} locatedId={located?.id} />}
          {timeline.isError ? (
            <ErrorState error={timeline.error} retry={() => timeline.refetch()} />
          ) : loading ? (
            <div className="space-y-3 p-4" aria-busy>{[0, 1, 2, 3].map((i) => <Skeleton key={i} className="h-16" />)}</div>
          ) : empty ? (
            <EmptyState
              icon={History}
              title={t("timeline.emptyTitle")}
              body={pointStats.data?.count ? t("timeline.emptyBodyHasData") : t("timeline.emptyBody")}
              action={pointStats.data?.last ? <Button size="sm" onClick={() => goDay(dayKey(pointStats.data!.last!))}>{t("timeline.jumpLatest")}</Button> : undefined}
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
                        editable={!viewUser}
                        onSelect={() => selectVisit(e.v)}
                        onSave={() => setDraft({ lat: e.v.lat, lon: e.v.lon, name: e.v.name })}
                        onEdit={() => setEditing(e.v)}
                        onMerge={nextOf(e.v) ? () => editVisit(e.v, { merge_to: nextOf(e.v).end }, t("timeline.merged")) : undefined}
                        onDelete={() => editVisit(e.v, { hidden: true }, t("timeline.deleted"))} />
                    ) : (
                      <TripItem key={`t${e.t.id}`} trip={e.t} units={units} clock={clock} editable={!viewUser}
                        open={activeTrip === e.t.id} onToggle={() => toggleTrip(e.t.id)} stats={activeTrip === e.t.id ? stats : null} />
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
        highlight={stats && trip ? { path: stats.path, key: `trip${trip.id}` } : null}
        fit={loading ? undefined : `${from}-${to}`}
        focus={focus}
        pin={located && { lon: located.lon!, lat: located.lat!, image: photoUrl(located.id), label: t("photos.pinLabel", { time: time(located.ts, clock) }), key: located.key }}
        onPinClick={() => setLocated(null)}
        onVisitClick={(id) => {
          const v = timeline.data?.visits.find((x) => x.id === id);
          if (v) selectVisit(v, true);
        }}
      />
      <PlaceDialog draft={draft} onClose={() => setDraft(null)} onSaved={(p) => draft?.pinStart && editVisit({ start: draft.pinStart } as Visit, { place_id: p.id })} />
      <VisitDialog visit={editing} onClose={() => setEditing(null)}
        onNewPlace={(v) => { setEditing(null); setDraft({ lat: v.lat, lon: v.lon, name: v.custom_name || v.name, pinStart: v.start }); }} />
    </div>
  );
}

type VisitPatch = { name?: string; place_id?: number; hidden?: boolean; merge_to?: number };

/** Saves a correction to a visit; with `done`, shows a toast that offers to undo it. */
function useVisitEdit() {
  const { t } = useTranslation();
  const toast = useToast();
  const m = useMutation({
    mutationFn: ({ start, patch }: { start: number; patch: VisitPatch }) => api("/visits", { method: "PATCH", body: { start, ...patch } }),
    onSuccess: () => ["timeline", "insights", "places", "visit-search"].forEach((k) => queryClient.invalidateQueries({ queryKey: [k] })),
  });
  return (v: Visit, patch: VisitPatch, done?: string) => {
    // The inverse of this patch, from what the visit looks like now.
    const undo: VisitPatch = {};
    if ("hidden" in patch) undo.hidden = false;
    if ("merge_to" in patch) undo.merge_to = v.merged_to ?? 0;
    if ("name" in patch) undo.name = v.custom_name ?? "";
    if ("place_id" in patch) undo.place_id = v.no_place ? -1 : (v.pinned_place ?? 0);
    m.mutate({ start: v.start, patch }, {
      onSuccess: () => done && toast("success", done, { label: t("timeline.undo"), run: () => m.mutate({ start: v.start, patch: undo }) }),
    });
  };
}

function VisitItem({ v, bounds, active, clock, editable, onSelect, onSave, onEdit, onMerge, onDelete }: {
  v: Visit;
  bounds: [number, number];
  active: boolean;
  clock: "24h" | "12h";
  editable: boolean;
  onSelect: () => void;
  onSave: () => void;
  onEdit: () => void;
  onMerge?: () => void;
  onDelete: () => void;
}) {
  const { t } = useTranslation();
  const editVisit = useVisitEdit();
  const menuBtn = useRef<HTMLButtonElement>(null);
  const [menu, setMenu] = useState(false);
  const title = v.custom_name || v.place_name || v.name || t("timeline.unknownPlace");
  // Skip parts already shown: the title, or a city that is already in the address.
  const sub = [v.address, v.city].filter((x, i, a) => x && x !== title && !(i > 0 && a[0]?.includes(x))).join(" · ");
  // Clip to the day shown, like a paper diary: a night at home reads 00:00 – 07:50.
  const start = Math.max(v.start, bounds[0]);
  const end = Math.min(v.end, bounds[1]);
  const item = (Icon: LucideIcon, label: string, run: () => void, danger = false) => (
    <button onClick={() => { setMenu(false); run(); }} className={cn("flex h-9 w-full items-center gap-2.5 rounded-lg px-2.5 text-left text-sm hover:bg-surface-2", danger && "text-danger")}>
      <Icon className="size-4" aria-hidden />
      {label}
    </button>
  );
  const reveal = "md:opacity-0 md:group-hover:opacity-100 md:focus:opacity-100";
  return (
    <li data-visit={v.id}>
      <div className={cn("group flex gap-1 rounded-xl p-3 transition-colors", active ? "bg-primary-subtle" : "hover:bg-surface-2")}>
        <button onClick={onSelect} className="flex min-w-0 flex-1 items-start gap-3 text-left" aria-pressed={active}>
          <span className={cn("grid size-10 shrink-0 place-items-center rounded-xl", v.place_id ? "bg-primary text-primary-fg" : "bg-surface-3 text-muted")}>
            <PlaceIcon name={v.place_icon} className="size-5" />
          </span>
          <span className="min-w-0 flex-1">
            <span className="flex items-center gap-1.5">
              <span className="truncate font-medium">{title}</span>
              {v.edited && <Pencil className="size-3 shrink-0 text-subtle" aria-label={t("timeline.edited")} />}
            </span>
            {sub && <span className="block truncate text-sm text-muted">{sub}</span>}
            <span className="mt-0.5 block text-sm text-subtle tabular-nums">
              {time(start, clock)} – {end === bounds[1] ? (clock === "12h" ? "12:00 AM" : "24:00") : time(end, clock)} · {duration(end - start)}
            </span>
          </span>
        </button>
        {!v.place_id && (
          <Button variant="ghost" size="icon" className={cn("size-9", reveal)} onClick={onSave} aria-label={t("timeline.saveAsPlace")} title={t("timeline.saveAsPlace")}>
            <Plus className="size-4" />
          </Button>
        )}
        {editable && (
          <>
            <Button ref={menuBtn} variant="ghost" size="icon" aria-expanded={menu} className={cn("size-9", reveal, menu && "md:opacity-100")}
              onClick={() => setMenu(!menu)} aria-label={t("timeline.visitMenu")} title={t("timeline.visitMenu")}>
              <MoreHorizontal className="size-4" />
            </Button>
            <Popover anchor={menuBtn} open={menu} onClose={() => setMenu(false)} align="end" className="w-60 p-1.5">
              {item(Pencil, t("timeline.rename"), onEdit)}
              {onMerge && item(Merge, t("timeline.merge"), onMerge)}
              {!!v.merged_to && item(Split, t("timeline.unmerge"), () => editVisit(v, { merge_to: 0 }, t("timeline.unmerged")))}
              <div className="my-1 h-px bg-border" />
              {item(Trash2, t("timeline.delete"), onDelete, true)}
            </Popover>
          </>
        )}
      </div>
    </li>
  );
}

/** Rename a visit and/or say which saved place it really was. */
function VisitDialog({ visit, onClose, onNewPlace }: { visit: Visit | null; onClose: () => void; onNewPlace: (v: Visit) => void }) {
  const { t } = useTranslation();
  const places = usePlaces();
  const editVisit = useVisitEdit();
  const [name, setName] = useState("");
  const [place, setPlace] = useState("auto");
  useEffect(() => {
    if (!visit) return;
    setName(visit.custom_name ?? "");
    setPlace(visit.no_place ? "none" : visit.pinned_place ? String(visit.pinned_place) : "auto");
  }, [visit]);
  const save = () => {
    if (!visit) return;
    editVisit(visit, { name: name.trim(), place_id: place === "auto" ? 0 : place === "none" ? -1 : Number(place) }, t("timeline.saved"));
    onClose();
  };
  const sorted = [...(places.data ?? [])].sort((a, b) => a.name.localeCompare(b.name));
  return (
    <Dialog
      open={!!visit}
      onClose={onClose}
      title={t("timeline.editVisit")}
      footer={
        <>
          <Button variant="ghost" icon={Plus} className="mr-auto" onClick={() => visit && onNewPlace(visit)}>{t("timeline.newPlace")}</Button>
          <Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button>
          <Button variant="primary" onClick={save}>{t("common.save")}</Button>
        </>
      }
    >
      {visit && (
        <form className="space-y-5" onSubmit={(e) => { e.preventDefault(); save(); }}>
          <Field label={t("timeline.visitName")} hint={t("timeline.visitNameHint")}>
            {(id, d) => <Input id={id} aria-describedby={d} value={name} maxLength={80} placeholder={visit.place_name || visit.name || t("timeline.unknownPlace")} onChange={(e) => setName(e.target.value)} />}
          </Field>
          <Field label={t("timeline.whichPlace")}>
            {(id) => (
              <Select id={id} label={t("timeline.whichPlace")} value={place} onChange={setPlace} options={[
                { value: "auto", label: t("timeline.placeAuto"), hint: t("timeline.placeAutoHint"), icon: <MapPinned className="size-4 text-subtle" /> },
                ...sorted.map((p) => ({ value: String(p.id), label: p.name, icon: <PlaceIcon name={p.icon} className="size-4 text-primary" /> })),
                { value: "none", label: t("timeline.placeNone"), hint: t("timeline.placeNoneHint"), icon: <CircleHelp className="size-4 text-subtle" /> },
              ]} />
            )}
          </Field>
          <p className="text-xs text-subtle">{t("timeline.editNote")}</p>
          <button type="submit" hidden />
        </form>
      )}
    </Dialog>
  );
}

function TripItem({ trip, units, clock, editable, open, onToggle, stats }: {
  trip: Trip;
  units: "metric" | "imperial";
  clock: "24h" | "12h";
  editable: boolean;
  open: boolean;
  onToggle: () => void;
  stats: TripStats | null;
}) {
  const { t } = useTranslation();
  const Icon = modeIcons[trip.mode] ?? MapPinned;
  const btn = useRef<HTMLButtonElement>(null);
  const [menu, setMenu] = useState(false);
  const setMode = useMutation({
    mutationFn: (mode: TripMode) => api("/trips/mode", { method: "PUT", body: { start: trip.start, mode } }),
    onSuccess: () => ["timeline", "insights"].forEach((k) => queryClient.invalidateQueries({ queryKey: [k] })),
  });
  return (
    <li className={cn("rounded-xl transition-colors", open && "my-1 bg-surface-2/60 ring-1 ring-border")}>
      <div className="flex items-center gap-3 py-1.5 pr-2 pl-[1.6rem] text-sm text-muted">
        <span className={cn("flex h-8 w-px self-stretch border-l-2 border-dashed", open ? "border-highlight" : "border-border-strong")} aria-hidden />
        <span className={cn("ml-2 grid size-7 shrink-0 place-items-center rounded-lg", open ? "bg-highlight text-white" : "bg-surface-2")}><Icon className="size-4" aria-hidden /></span>
        <span className="min-w-0 truncate">
          {editable ? (
            <button ref={btn} onClick={() => setMenu(!menu)} aria-expanded={menu} title={t("timeline.changeMode")}
              className="rounded px-0.5 font-medium text-fg underline decoration-border-strong decoration-dotted underline-offset-4 hover:decoration-primary">
              {modeLabel(trip.mode)}
            </button>
          ) : (
            <span className="font-medium text-fg">{modeLabel(trip.mode)}</span>
          )}
          {" "}· {distance(trip.distance, units)} · {duration(trip.end - trip.start)}
        </span>
        <Popover anchor={btn} open={menu} onClose={() => setMenu(false)} className="w-48 p-1.5">
          <p className="px-2.5 pt-1 pb-1.5 text-xs font-semibold tracking-wide text-subtle uppercase">{t("timeline.changeMode")}</p>
          {(Object.keys(modeIcons) as TripMode[]).map((m) => {
            const MIcon = modeIcons[m];
            return (
              <button key={m} onClick={() => { setMenu(false); setMode.mutate(m); }} className={cn("flex h-9 w-full items-center gap-2.5 rounded-lg px-2.5 text-left text-sm hover:bg-surface-2", m === trip.mode && "text-primary")}>
                <MIcon className="size-4" aria-hidden />
                {modeLabel(m)}
              </button>
            );
          })}
        </Popover>
        <button onClick={onToggle} aria-expanded={open} aria-label={t("timeline.tripDetails")} title={t("timeline.tripDetails")}
          className="ml-auto flex shrink-0 items-center gap-1 rounded-md px-1.5 py-1 text-xs text-subtle tabular-nums hover:bg-surface-3 hover:text-fg">
          {time(trip.start, clock)}
          <ChevronDown className={cn("size-3.5 transition-transform", open && "rotate-180")} aria-hidden />
        </button>
      </div>
      {open && <TripDetails trip={trip} stats={stats} units={units} clock={clock} />}
    </li>
  );
}

function TripDetails({ trip, stats, units, clock }: { trip: Trip; stats: TripStats | null; units: "metric" | "imperial"; clock: "24h" | "12h" }) {
  const { t } = useTranslation();
  const ms = trip.end - trip.start;
  const alt = (m: number) => (units === "imperial" ? `${Math.round(m * 3.28084)} ft` : `${Math.round(m)} m`);
  const tiles: [LucideIcon, string, string][] = [
    [Gauge, t("timeline.avgSpeed"), speed(ms > 0 ? trip.distance / (ms / 1000) : 0, units)],
    [Gauge, t("timeline.topSpeed"), stats?.speed.length ? speed(stats.topSpeed, units) : "–"],
  ];
  if (stats?.elevation.length) tiles.push([TrendingUp, t("timeline.climb"), alt(stats.climb)], [TrendingDown, t("timeline.descent"), alt(stats.descent)]);
  // Opened near the bottom of the list (or on a phone): bring the charts into view.
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => { box.current?.scrollIntoView({ block: "nearest", behavior: "smooth" }); }, [!!stats]);
  return (
    <div ref={box} className="gt-pop scroll-mb-4 space-y-4 px-4 pt-1 pb-4">
      <dl className={cn("grid grid-cols-2 gap-2", tiles.length === 4 && "sm:grid-cols-4")}>
        {tiles.map(([I, k, v]) => (
          <div key={k} className="rounded-lg bg-surface px-2.5 py-2">
            <dt className="flex items-center gap-1 text-[11px] text-subtle"><I className="size-3" aria-hidden />{k}</dt>
            <dd className="font-semibold tabular-nums">{v}</dd>
          </div>
        ))}
      </dl>
      {!stats ? (
        <Skeleton className="h-24" />
      ) : stats.speed.length < 2 ? (
        <p className="text-sm text-muted">{t("timeline.noProfile")}</p>
      ) : (
        <div className="space-y-5 pt-4">
          <AreaChart data={stats.speed} label={t("timeline.speedChart")} yMin={0} formatY={(v) => speed(v, units)} formatX={(x) => time(x, clock)} />
          {stats.elevation.length > 1 && <AreaChart data={stats.elevation} label={t("timeline.elevationChart")} formatY={alt} formatX={(x) => time(x, clock)} />}
        </div>
      )}
      <p className="flex items-center gap-1.5 text-xs text-subtle"><span className="h-1 w-4 rounded-full bg-highlight" aria-hidden />{t("timeline.showOnMap")}</p>
    </div>
  );
}

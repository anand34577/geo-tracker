import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { BatteryMedium, CalendarDays, Clock3, Copy, Crosshair, Gauge, LocateFixed, Mountain, Radio, Smartphone, Upload, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, rowToPoint, type FamilyPerson, type PointRow, type PointStats } from "../lib/api";
import { ago, dateTime, dayKey, distance, duration, speed } from "../lib/format";
import { summarize, toSegments, useFamily, useLatest, useMonthDays, usePlaces, usePoints, useTimeline } from "../lib/data";
import { usePrefs } from "../lib/prefs";
import MapView, { defaultLayers, type LayerToggles, type MapPerson } from "../components/MapView";
import { RangePicker, presetRange, type Range } from "../components/controls";
import { Playback } from "../components/Playback";
import { AddressSearch } from "../components/AddressSearch";
import { Avatar, Button, Skeleton, cn, useToast } from "../components/ui";

const LAYERS_KEY = "gt-layers";
function savedLayers(): LayerToggles {
  try {
    return { ...defaultLayers, ...JSON.parse(localStorage.getItem(LAYERS_KEY) ?? "{}") };
  } catch {
    return defaultLayers;
  }
}

export default function MapPage() {
  const { t } = useTranslation();
  const toast = useToast();
  const { units, clock } = usePrefs();
  const [range, setRange] = useState<Range>(() => presetRange("today"));
  const [month, setMonth] = useState(new Date());
  const marked = useMonthDays(month);
  const [layers, setLayersState] = useState<LayerToggles>(savedLayers);
  const setLayers = (l: LayerToggles) => {
    setLayersState(l);
    try {
      localStorage.setItem(LAYERS_KEY, JSON.stringify(l));
    } catch {
      /* private mode */
    }
  };
  const stats = useQuery({ queryKey: ["stats"], queryFn: () => api<PointStats>("/stats") });
  const latest = useLatest();
  const points = usePoints(range.from, range.to);
  const timeline = useTimeline(range.from, range.to);
  const places = usePlaces();
  const [focus, setFocus] = useState<{ lon: number; lat: number; zoom?: number; key: number } | null>(null);
  const [head, setHead] = useState<{ lon: number; lat: number } | null>(null);
  const [picked, setPicked] = useState<PointRow | null>(null);
  const family = useFamily();
  const [params, setParams] = useSearchParams();
  const [personId, setPersonId] = useState<number | null>(() => Number(params.get("person")) || null);
  const people = useMemo<MapPerson[]>(
    () => (family.data ?? []).filter((p) => p.live && p.point).map((p) => ({ id: p.user_id, name: p.name, color: p.color, lon: p.point!.lon, lat: p.point!.lat, stale: Date.now() - p.point!.ts > 30 * 60_000 })),
    [family.data],
  );
  const person = family.data?.find((p) => p.user_id === personId) ?? null;
  // Arriving from the Family page (?person=): fly to them once their position is known.
  const focusedFromUrl = useRef(false);
  useEffect(() => {
    if (focusedFromUrl.current || !person?.point) return;
    focusedFromUrl.current = true;
    setFocus({ lon: person.point.lon, lat: person.point.lat, zoom: 15, key: Date.now() });
    setParams({}, { replace: true });
  }, [person]); // eslint-disable-line react-hooks/exhaustive-deps
  const selectPerson = (id: number) => {
    const p = family.data?.find((x) => x.user_id === id);
    setPersonId(id);
    setPicked(null);
    if (p?.point) setFocus({ lon: p.point.lon, lat: p.point.lat, zoom: 15, key: Date.now() });
  };
  const sendLocation = useSendLocation();

  const rows = points.data?.points ?? [];
  const path = useMemo(() => toSegments(rows), [rows]);
  const isToday = range.preset === "today";
  const me = isToday && latest.data ? { lon: latest.data.lon, lat: latest.data.lat, acc: latest.data.acc } : null;
  const sum = summarize(timeline.data);
  const empty = stats.data?.count === 0;
  const currentVisit = timeline.data?.visits.at(-1);
  const atVisit = currentVisit && latest.data && latest.data.ts - currentVisit.end < 15 * 60_000;
  const p = picked ? rowToPoint(picked) : null;

  return (
    <div className="absolute inset-0">
      <MapView
        className="h-full"
        label={t("map.label")}
        path={path}
        points={rows}
        visits={timeline.data?.visits.map((v) => ({ id: v.id, lon: v.lon, lat: v.lat }))}
        places={places.data}
        me={me}
        marker={head}
        people={people}
        onPersonClick={selectPerson}
        layers={layers}
        onLayersChange={setLayers}
        onPointClick={setPicked}
        fit={points.isSuccess ? `${range.from}-${range.to}` : undefined}
        // Keep fitted content out from under the floating panels (left column on md+, top bar and bottom card on phones).
        fitPadding={window.innerWidth >= 768 ? { top: 80, bottom: 120, left: 400, right: 80 } : { top: 130, bottom: 340, left: 40, right: 40 }}
        focus={focus}
      />

      {/* Toolbar: search + time range */}
      <div className="absolute top-3 right-16 left-3 z-20 flex flex-col gap-2 md:right-auto md:w-[22rem]">
        <AddressSearch onPick={(h) => setFocus({ lon: h.lon, lat: h.lat, zoom: 16, key: Date.now() })} />
        <RangePicker label={t("range.label")} value={range} onChange={(r) => { setRange(r); setPicked(null); }} marked={marked} onMonthChange={setMonth} className="shadow-pop" />
      </div>

      {/* Phones: playback stacks above the card in one column; md+: `contents` lets each float on its own. */}
      <div className="absolute inset-x-3 bottom-3 z-10 flex flex-col gap-2 md:contents">
        {/* Playback */}
        {rows.length > 1 && (
          <div className="md:absolute md:top-[6.75rem] md:left-3 md:z-10 md:w-[22rem] lg:top-auto lg:right-16 lg:bottom-6 lg:left-[24rem] lg:w-auto">
            <div className="mx-auto max-w-xl">
              <Playback points={rows} clock={clock} onPosition={setHead} />
            </div>
          </div>
        )}
        {/* Status / summary card */}
        <aside className="md:absolute md:bottom-6 md:left-3 md:z-10 md:w-[22rem]">
          {person ? (
            <PersonCard person={person} clock={clock} onClose={() => setPersonId(null)} />
          ) : p ? (
            <PointCard p={p} units={units} clock={clock} onClose={() => setPicked(null)} onCopy={() => navigator.clipboard.writeText(`${p.lat.toFixed(6)}, ${p.lon.toFixed(6)}`).then(() => toast("success", t("common.copied")))} />
          ) : (
            <div className="rounded-2xl border border-border bg-surface/95 p-4 shadow-pop backdrop-blur-sm">
              {latest.isPending || stats.isPending ? (
                <div className="space-y-3" aria-hidden>
                  <Skeleton className="h-5 w-40" />
                  <Skeleton className="h-12 w-full" />
                </div>
              ) : empty ? (
                <div>
                  <div className="flex items-center gap-2">
                    <span className="relative flex size-2.5">
                      <span className="absolute inline-flex size-full animate-ping rounded-full bg-primary opacity-60" />
                      <span className="relative inline-flex size-2.5 rounded-full bg-primary" />
                    </span>
                    <h2 className="font-semibold">{t("map.waitingTitle")}</h2>
                  </div>
                  <p className="mt-2 text-sm text-muted">{t("map.waitingBody")}</p>
                  <div className="mt-4 flex flex-wrap gap-2">
                    <Link to="/settings/devices"><Button variant="primary" size="sm" icon={Smartphone} tabIndex={-1}>{t("map.connectPhone")}</Button></Link>
                    <Link to="/settings/data"><Button size="sm" icon={Upload} tabIndex={-1}>{t("map.importHistory")}</Button></Link>
                  </div>
                </div>
              ) : (
                <div>
                  {isToday && (
                    <div className="mb-4 flex items-start justify-between gap-3 border-b border-border pb-4">
                      <div className="min-w-0">
                        <p className="text-xs font-semibold tracking-wide text-subtle uppercase">{t("map.lastSeen")}</p>
                        <h2 className="mt-0.5 truncate font-semibold">{atVisit ? currentVisit.place_name || currentVisit.name || t("timeline.unknownPlace") : t("map.onTheMove")}</h2>
                        {latest.data && (
                          <p className="flex flex-wrap gap-x-3 text-sm text-muted">
                            <span>{ago(latest.data.ts)}</span>
                            {latest.data.batt != null && <span className="flex items-center gap-1"><BatteryMedium className="size-3.5" aria-hidden />{latest.data.batt}%</span>}
                          </p>
                        )}
                      </div>
                      {me && (
                        <Button variant="ghost" size="icon" onClick={() => setFocus({ ...me, key: Date.now() })} aria-label={t("map.center")} title={t("map.center")}>
                          <Crosshair className="size-5" />
                        </Button>
                      )}
                    </div>
                  )}
                  <dl className="grid grid-cols-3 gap-2 text-center">
                    <Stat label={t("summary.distance")} value={distance(sum.distance, units)} />
                    <Stat label={t("summary.places")} value={String(sum.places)} />
                    <Stat label={t("summary.moving")} value={duration(sum.moving)} />
                  </dl>
                  {points.data && points.data.step > 1 && <p className="mt-3 text-center text-xs text-subtle">{t("map.thinned", { total: points.data.total.toLocaleString(), step: points.data.step })}</p>}
                  {people.length > 0 && (
                    <div className="mt-4 flex items-center gap-2 border-t border-border pt-3">
                      <span className="text-xs font-medium text-subtle">{t("nav.family")}</span>
                      <div className="flex -space-x-1.5">
                        {people.map((x) => (
                          <button key={x.id} onClick={() => selectPerson(x.id)} title={x.name} aria-label={x.name} className="rounded-full ring-2 ring-surface transition-transform hover:z-10 hover:scale-110">
                            <Avatar name={x.name} color={x.color} className={cn("size-8 text-xs", x.stale && "opacity-60")} />
                          </button>
                        ))}
                      </div>
                    </div>
                  )}
                  <div className="mt-3 grid grid-cols-2 gap-2">
                    <Link to={`/timeline/${dayKey(range.from)}`} className="flex items-center justify-center gap-1.5 rounded-lg py-2 text-sm font-medium text-primary hover:bg-primary-subtle">
                      <CalendarDays className="size-4" aria-hidden />
                      {isToday ? t("map.openToday") : t("map.openTimeline")}
                    </Link>
                    {sendLocation.supported && (
                      <button onClick={sendLocation.send} disabled={sendLocation.busy} className="flex items-center justify-center gap-1.5 rounded-lg py-2 text-sm font-medium text-primary hover:bg-primary-subtle disabled:opacity-50">
                        <LocateFixed className={cn("size-4", sendLocation.busy && "animate-pulse")} aria-hidden />
                        {t("map.sendLocation")}
                      </button>
                    )}
                  </div>
                </div>
              )}
            </div>
          )}
        </aside>
      </div>
    </div>
  );
}

function PointCard({ p, units, clock, onClose, onCopy }: { p: ReturnType<typeof rowToPoint>; units: "metric" | "imperial"; clock: "24h" | "12h"; onClose: () => void; onCopy: () => void }) {
  const { t } = useTranslation();
  return (
    <div className="rounded-2xl border border-border bg-surface/95 p-4 shadow-pop backdrop-blur-sm" role="dialog" aria-label={t("map.pointDetails")}>
      <div className="flex items-start justify-between gap-3">
        <div>
          <p className="text-xs font-semibold tracking-wide text-subtle uppercase">{t("map.pointDetails")}</p>
          <p className="mt-0.5 font-semibold tabular-nums">{dateTime(p.ts, clock)}</p>
        </div>
        <Button variant="ghost" size="icon" className="size-8" onClick={onClose} aria-label={t("common.close")}><X className="size-4" /></Button>
      </div>
      <button onClick={onCopy} className="mt-2 flex items-center gap-1.5 font-mono text-xs text-muted hover:text-fg" title={t("common.copy")}>
        {p.lat.toFixed(6)}, {p.lon.toFixed(6)} <Copy className="size-3" aria-hidden />
      </button>
      <dl className="mt-3 grid grid-cols-2 gap-2 text-sm">
        {p.acc != null && <Detail icon={Radio} label={t("map.accuracy")} value={`±${Math.round(p.acc)} m`} />}
        {p.speed != null && <Detail icon={Gauge} label={t("map.speed")} value={speed(p.speed, units)} />}
        {p.alt != null && <Detail icon={Mountain} label={t("map.altitude")} value={`${Math.round(p.alt)} m`} />}
        {p.batt != null && <Detail icon={BatteryMedium} label={t("map.battery")} value={`${p.batt}%`} />}
      </dl>
    </div>
  );
}

function Detail({ icon: Icon, label, value, title }: { icon: typeof Radio; label: string; value: string; title?: string }) {
  return (
    <div className="flex items-center gap-2 rounded-lg bg-surface-2 px-2.5 py-2" title={title}>
      <Icon className="size-4 text-subtle" aria-hidden />
      <div className="min-w-0">
        <dt className="text-[11px] text-subtle">{label}</dt>
        <dd className="font-medium tabular-nums">{value}</dd>
      </div>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-xs text-subtle">{label}</dt>
      <dd className="mt-0.5 font-semibold tabular-nums">{value}</dd>
    </div>
  );
}

function PersonCard({ person, clock, onClose }: { person: FamilyPerson; clock: "24h" | "12h"; onClose: () => void }) {
  const { t } = useTranslation();
  return (
    <div className="rounded-2xl border border-border bg-surface/95 p-4 shadow-pop backdrop-blur-sm" role="dialog" aria-label={person.name}>
      <div className="flex items-start gap-3">
        <Avatar name={person.name} color={person.color} className="size-11" />
        <div className="min-w-0 flex-1">
          <p className="truncate font-semibold">{person.name}</p>
          <p className="truncate text-sm text-muted">{person.place || t("family.somewhere")}</p>
        </div>
        <Button variant="ghost" size="icon" className="size-8" onClick={onClose} aria-label={t("common.close")}><X className="size-4" /></Button>
      </div>
      {person.point && (
        <dl className="mt-3 grid grid-cols-2 gap-2 text-sm">
          <Detail icon={Clock3} label={t("map.lastSeen")} value={`${ago(person.point.ts)}`} title={dateTime(person.point.ts, clock)} />
          {person.point.batt != null && <Detail icon={BatteryMedium} label={t("map.battery")} value={`${person.point.batt}%`} />}
          {person.approx && <Detail icon={Radio} label={t("map.accuracy")} value={t("family.approxShort")} />}
        </dl>
      )}
      {person.history_from != null && (
        <Link to={`/timeline?user=${person.user_id}`} className="mt-3 flex items-center justify-center gap-1.5 rounded-lg py-2 text-sm font-medium text-primary hover:bg-primary-subtle">
          <CalendarDays className="size-4" aria-hidden />
          {t("family.viewTimeline", { name: person.name.split(" ")[0] })}
        </Link>
      )}
    </div>
  );
}

/** One-tap "here I am" from the browser (also works as an installed PWA). */
function useSendLocation() {
  const { t } = useTranslation();
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const supported = typeof navigator !== "undefined" && "geolocation" in navigator && window.isSecureContext;
  const send = () => {
    setBusy(true);
    navigator.geolocation.getCurrentPosition(
      async (pos) => {
        try {
          await api("/points", { method: "POST", body: { ts: pos.timestamp, lat: pos.coords.latitude, lon: pos.coords.longitude, acc: pos.coords.accuracy, alt: pos.coords.altitude ?? undefined, speed: pos.coords.speed ?? undefined } });
          toast("success", t("map.locationSent"));
        } catch (e) {
          toast("error", (e as Error).message);
        } finally {
          setBusy(false);
        }
      },
      (err) => {
        setBusy(false);
        toast("error", err.code === err.PERMISSION_DENIED ? t("map.locationDenied") : t("map.locationFailed"));
      },
      { enableHighAccuracy: true, timeout: 20_000, maximumAge: 30_000 },
    );
  };
  return { supported, busy, send };
}
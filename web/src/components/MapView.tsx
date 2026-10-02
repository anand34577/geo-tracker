import { useEffect, useRef, useState } from "react";
import { AttributionControl, LngLatBounds, Map as MLMap, Marker, NavigationControl, setWorkerUrl, type GeoJSONSource } from "maplibre-gl";
import workerUrl from "maplibre-gl/dist/maplibre-gl-worker.mjs?worker&url";
import { Check, Layers, WifiOff } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useConfig, useMe, type PointRow } from "../lib/api";
import { useIsDark, usePrefs, useSavePrefs } from "../lib/prefs";
import { Popover, Switch } from "./controls";
import { cn, Skeleton } from "./ui";

// MapLibre resolves its worker next to its own module file, which does not exist once
// bundled; Vite builds the worker separately and hands us its URL.
setWorkerUrl(workerUrl);

type LonLat = [number, number];

export type LayerToggles = { path: boolean; points: boolean; heatmap: boolean; visits: boolean; places: boolean; family: boolean };
export const defaultLayers: LayerToggles = { path: true, points: false, heatmap: false, visits: true, places: true, family: true };

export type MapPerson = { id: number; name: string; color: string; lon: number; lat: number; stale?: boolean };

type Props = {
  /** Line segments of [lon, lat]; split at long gaps so we don't draw straight lines across them. */
  path?: LonLat[][];
  /** Raw points, used by the points and heatmap layers. */
  points?: PointRow[];
  visits?: { id: number; lon: number; lat: number; active?: boolean }[];
  places?: { id: number; lon: number; lat: number; radius: number }[];
  me?: { lon: number; lat: number; acc?: number } | null;
  /** Playback head. */
  marker?: { lon: number; lat: number } | null;
  /** Family members, drawn as avatar markers. */
  people?: MapPerson[];
  onPersonClick?: (id: number) => void;
  layers?: LayerToggles;
  /** When set, layer switches are shown in the map menu. */
  onLayersChange?: (l: LayerToggles) => void;
  /** Change this key to zoom to fit all content once it is available. */
  fit?: string;
  /** Space (px) kept clear when fitting, e.g. for panels floating over the map. */
  fitPadding?: { top: number; bottom: number; left: number; right: number };
  /** Change `key` to fly to a location. */
  focus?: { lon: number; lat: number; zoom?: number; key: number } | null;
  /** A spotlighted location (e.g. where a photo was taken): a large pin with a pulsing ring. Change `key` to replace it. */
  pin?: { lon: number; lat: number; image?: string; label?: string; key: number } | null;
  onPinClick?: () => void;
  onVisitClick?: (id: number) => void;
  onPointClick?: (row: PointRow) => void;
  onMapClick?: (lon: number, lat: number) => void;
  className?: string;
  label: string;
  /** Unauthenticated pages (share links) use the public default style. */
  publicMode?: boolean;
};

const EMPTY = { type: "FeatureCollection" as const, features: [] };
const PUBLIC_STYLES = { light: "https://tiles.openfreemap.org/styles/positron", dark: "https://tiles.openfreemap.org/styles/dark" };

function circle(lon: number, lat: number, r: number, n = 48): LonLat[] {
  const out: LonLat[] = [];
  const kx = 111_320 * Math.cos((lat * Math.PI) / 180);
  for (let i = 0; i <= n; i++) {
    const a = (i / n) * 2 * Math.PI;
    out.push([lon + (r * Math.cos(a)) / kx, lat + (r * Math.sin(a)) / 110_540]);
  }
  return out;
}

const fc = <T,>(items: T[] | undefined, f: (x: T, i: number) => object) => ({ type: "FeatureCollection" as const, features: (items ?? []).map(f) as never[] });

function collections(d: Props) {
  const me = d.me;
  return {
    path: fc(d.path?.filter((s) => s.length > 1), (s) => ({ type: "Feature", properties: {}, geometry: { type: "LineString", coordinates: s } })),
    pts: fc(d.points, (r, i) => ({ type: "Feature", properties: { i }, geometry: { type: "Point", coordinates: [r[2], r[1]] } })),
    visits: fc(d.visits, (v) => ({ type: "Feature", properties: { id: v.id, active: !!v.active }, geometry: { type: "Point", coordinates: [v.lon, v.lat] } })),
    places: fc(d.places, (p) => ({ type: "Feature", properties: { id: p.id }, geometry: { type: "Polygon", coordinates: [circle(p.lon, p.lat, p.radius)] } })),
    me: me
      ? fc(["acc", "dot"], (kind) =>
          kind === "acc"
            ? { type: "Feature", properties: { kind }, geometry: { type: "Polygon", coordinates: [circle(me.lon, me.lat, Math.max(me.acc ?? 0, 5))] } }
            : { type: "Feature", properties: { kind }, geometry: { type: "Point", coordinates: [me.lon, me.lat] } })
      : EMPTY,
    head: fc(d.marker ? [d.marker] : [], (m) => ({ type: "Feature", properties: {}, geometry: { type: "Point", coordinates: [m.lon, m.lat] } })),
  };
}

const layerGroups: Record<keyof LayerToggles, string[]> = {
  path: ["path-casing", "path"],
  points: ["pts"],
  heatmap: ["heat"],
  visits: ["visits"],
  places: ["places-fill", "places-line"],
  family: [], // DOM markers, toggled separately
};

function personElement(p: MapPerson, onClick: () => void): HTMLElement {
  const el = document.createElement("button");
  el.type = "button";
  el.setAttribute("aria-label", p.name);
  el.title = p.name;
  el.className = "grid size-10 place-items-center rounded-full border-[3px] border-white text-sm font-semibold text-white shadow-lg transition-transform hover:scale-110";
  el.style.background = p.color;
  el.style.opacity = p.stale ? "0.55" : "1";
  el.textContent = p.name.split(/\s+/).filter(Boolean).slice(0, 2).map((w) => w[0]!.toUpperCase()).join("");
  el.addEventListener("click", (e) => {
    e.stopPropagation();
    onClick();
  });
  return el;
}

/** A big, unmissable pin: the photo in a round frame on a stalk, with a pulsing ring on the ground. */
function pinElement(image: string | undefined, label: string | undefined, onClick: () => void): HTMLElement {
  const el = document.createElement("button");
  el.type = "button";
  el.className = "gt-pin";
  if (label) {
    el.title = label;
    el.setAttribute("aria-label", label);
  }
  const ring = document.createElement("span");
  ring.className = "gt-pin-ring";
  const head = document.createElement("span");
  head.className = "gt-pin-head";
  if (image) head.style.backgroundImage = `url("${image}")`;
  el.append(ring, head);
  el.addEventListener("click", (e) => {
    e.stopPropagation();
    onClick();
  });
  return el;
}

function addLayers(map: MLMap) {
  const css = getComputedStyle(document.documentElement);
  const primary = css.getPropertyValue("--primary").trim();
  const surface = css.getPropertyValue("--surface").trim();
  for (const id of ["places", "path", "pts", "visits", "me", "head"]) map.addSource(id, { type: "geojson", data: EMPTY });
  map.addLayer({ id: "places-fill", type: "fill", source: "places", paint: { "fill-color": primary, "fill-opacity": 0.1 } });
  map.addLayer({ id: "places-line", type: "line", source: "places", paint: { "line-color": primary, "line-width": 1.5, "line-dasharray": [2, 2] } });
  map.addLayer({
    id: "heat",
    type: "heatmap",
    source: "pts",
    paint: {
      "heatmap-radius": ["interpolate", ["linear"], ["zoom"], 4, 6, 12, 18, 16, 28],
      "heatmap-intensity": ["interpolate", ["linear"], ["zoom"], 4, 0.6, 15, 1.4],
      "heatmap-opacity": 0.85,
      "heatmap-color": ["interpolate", ["linear"], ["heatmap-density"], 0, "rgba(0,0,0,0)", 0.15, "#2dd4bf", 0.4, "#22c55e", 0.65, "#facc15", 0.85, "#f97316", 1, "#ef4444"],
    },
  });
  map.addLayer({ id: "path-casing", type: "line", source: "path", layout: { "line-join": "round", "line-cap": "round" }, paint: { "line-color": surface, "line-width": 7, "line-opacity": 0.9 } });
  map.addLayer({ id: "path", type: "line", source: "path", layout: { "line-join": "round", "line-cap": "round" }, paint: { "line-color": primary, "line-width": 3.5 } });
  map.addLayer({
    id: "pts",
    type: "circle",
    source: "pts",
    paint: { "circle-radius": ["interpolate", ["linear"], ["zoom"], 8, 2, 16, 5], "circle-color": primary, "circle-stroke-color": surface, "circle-stroke-width": 1 },
  });
  map.addLayer({
    id: "visits",
    type: "circle",
    source: "visits",
    paint: {
      "circle-radius": ["case", ["get", "active"], 10, 7],
      "circle-color": ["case", ["get", "active"], primary, surface],
      "circle-stroke-color": ["case", ["get", "active"], surface, primary],
      "circle-stroke-width": 3,
    },
  });
  map.addLayer({ id: "me-acc", type: "fill", source: "me", filter: ["==", ["get", "kind"], "acc"], paint: { "fill-color": "#2563eb", "fill-opacity": 0.12 } });
  map.addLayer({ id: "me-halo", type: "circle", source: "me", filter: ["==", ["get", "kind"], "dot"], paint: { "circle-radius": 13, "circle-color": "#2563eb", "circle-opacity": 0.2 } });
  map.addLayer({ id: "me-dot", type: "circle", source: "me", filter: ["==", ["get", "kind"], "dot"], paint: { "circle-radius": 6.5, "circle-color": "#2563eb", "circle-stroke-color": "#fff", "circle-stroke-width": 2.5 } });
  map.addLayer({ id: "head-halo", type: "circle", source: "head", paint: { "circle-radius": 16, "circle-color": "#f97316", "circle-opacity": 0.25 } });
  map.addLayer({ id: "head", type: "circle", source: "head", paint: { "circle-radius": 7, "circle-color": "#f97316", "circle-stroke-color": "#fff", "circle-stroke-width": 2.5 } });
}

export default function MapView(props: Props) {
  const { t } = useTranslation();
  const me = useMe();
  const signedIn = !!me.data && !props.publicMode;
  const { data: config } = useConfig(signedIn);
  const dark = useIsDark();
  const prefs = usePrefs();
  const savePrefs = useSavePrefs();
  const el = useRef<HTMLDivElement>(null);
  const mapRef = useRef<MLMap | null>(null);
  const ready = useRef(false);
  const lastFit = useRef<string | undefined>(undefined);
  const propsRef = useRef(props);
  propsRef.current = props;
  const [failed, setFailed] = useState(false);
  const layersBtn = useRef<HTMLButtonElement>(null);
  const [menu, setMenu] = useState(false);
  const markers = useRef(new Map<number, Marker>());

  const basemaps = config?.basemaps ?? [];
  const bm = basemaps.find((b) => b.id === prefs.basemap) ?? basemaps.find((b) => b.id === config?.basemap_default) ?? basemaps[0];
  const styleUrl = signedIn ? (bm ? (dark ? bm.dark : bm.light) : null) : dark ? PUBLIC_STYLES.dark : PUBLIC_STYLES.light;
  const layers = props.layers ?? defaultLayers;

  const sync = () => {
    const map = mapRef.current;
    if (!map || !ready.current) return;
    const p = propsRef.current;
    for (const [id, data] of Object.entries(collections(p))) (map.getSource(id) as GeoJSONSource | undefined)?.setData(data);
    const l = p.layers ?? defaultLayers;
    for (const [group, ids] of Object.entries(layerGroups)) {
      for (const id of ids) if (map.getLayer(id)) map.setLayoutProperty(id, "visibility", l[group as keyof LayerToggles] ? "visible" : "none");
    }
    if (p.fit !== undefined && p.fit !== lastFit.current) {
      const b = new LngLatBounds();
      (p.path ?? []).flat().forEach((pt) => b.extend(pt));
      if (!p.path?.length) (p.points ?? []).forEach((r) => b.extend([r[2], r[1]]));
      (p.visits ?? []).forEach((v) => b.extend([v.lon, v.lat]));
      if (!p.path?.length && !p.visits?.length) (p.places ?? []).forEach((pl) => b.extend([pl.lon, pl.lat]));
      if (p.me && !p.path?.length) b.extend([p.me.lon, p.me.lat]);
      if ((p.layers ?? defaultLayers).family) (p.people ?? []).forEach((x) => b.extend([x.lon, x.lat]));
      if (!b.isEmpty()) {
        lastFit.current = p.fit;
        map.fitBounds(b, { padding: p.fitPadding ?? { top: 80, bottom: 80, left: 64, right: 64 }, maxZoom: 16, duration: 600 });
      }
    }
  };

  // Theme, accent or basemap changed: swap the style in place, keeping the view.
  const styleKey = `${styleUrl}|${prefs.accent}`;
  const appliedStyle = useRef(styleKey);
  useEffect(() => {
    const map = mapRef.current;
    if (!map || !styleUrl || appliedStyle.current === styleKey) return;
    appliedStyle.current = styleKey;
    ready.current = false;
    map.setStyle(styleUrl, { diff: false });
  }, [styleKey]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!el.current || !styleUrl) return;
    appliedStyle.current = styleKey;
    const map = new MLMap({ container: el.current, style: styleUrl, center: [10, 30], zoom: 1.5, attributionControl: false, dragRotate: false });
    mapRef.current = map;
    map.addControl(new NavigationControl({ showCompass: false }), "bottom-right");
    map.addControl(new AttributionControl({ compact: true }), "bottom-left");
    map.on("style.load", () => {
      addLayers(map);
      ready.current = true;
      setFailed(false);
      sync();
    });
    map.on("error", (e) => {
      if (!ready.current && String(e.error?.message ?? "").match(/style|Failed to fetch/i)) setFailed(true);
    });
    const clickable = ["visits", "pts"];
    map.on("click", (e) => {
      const f = map.queryRenderedFeatures(e.point, { layers: clickable.filter((l) => map.getLayer(l)) })[0];
      const p = propsRef.current;
      if (f?.layer.id === "visits") return p.onVisitClick?.(Number(f.properties.id));
      if (f?.layer.id === "pts" && p.points) return p.onPointClick?.(p.points[Number(f.properties.i)]);
      p.onMapClick?.(e.lngLat.lng, e.lngLat.lat);
    });
    for (const l of clickable) {
      map.on("mouseenter", l, () => (map.getCanvas().style.cursor = "pointer"));
      map.on("mouseleave", l, () => (map.getCanvas().style.cursor = propsRef.current.onMapClick ? "crosshair" : ""));
    }
    return () => {
      map.remove();
      mapRef.current = null;
      ready.current = false;
    };
  }, [!!styleUrl]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => { sync(); }, [props.path, props.points, props.visits, props.places, props.me, props.marker, props.fit, layers]); // eslint-disable-line react-hooks/exhaustive-deps

  // Family avatars are DOM markers: crisp, clickable, and independent of the map style.
  useEffect(() => {
    const map = mapRef.current;
    if (!map) return;
    const show = layers.family ? (props.people ?? []) : [];
    const keep = new Set(show.map((p) => p.id));
    for (const [id, m] of markers.current) if (!keep.has(id)) { m.remove(); markers.current.delete(id); }
    for (const p of show) {
      markers.current.get(p.id)?.remove();
      const m = new Marker({ element: personElement(p, () => propsRef.current.onPersonClick?.(p.id)) }).setLngLat([p.lon, p.lat]).addTo(map);
      markers.current.set(p.id, m);
    }
  }, [props.people, layers.family, !!styleUrl]); // eslint-disable-line react-hooks/exhaustive-deps

  // The spotlight pin (photo location). Replaced whenever its key changes, removed when cleared.
  const pinMarker = useRef<Marker | null>(null);
  useEffect(() => {
    const map = mapRef.current;
    pinMarker.current?.remove();
    pinMarker.current = null;
    const p = props.pin;
    if (!map || !p) return;
    pinMarker.current = new Marker({ element: pinElement(p.image, p.label, () => propsRef.current.onPinClick?.()), anchor: "bottom" }).setLngLat([p.lon, p.lat]).addTo(map);
    return () => {
      pinMarker.current?.remove();
      pinMarker.current = null;
    };
  }, [props.pin?.key, !!styleUrl]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (props.onMapClick && mapRef.current) mapRef.current.getCanvas().style.cursor = "crosshair";
  }, [props.onMapClick]);

  useEffect(() => {
    const f = props.focus;
    if (f && mapRef.current) mapRef.current.flyTo({ center: [f.lon, f.lat], zoom: f.zoom ?? Math.max(mapRef.current.getZoom(), 15), duration: 700 });
  }, [props.focus?.key]); // eslint-disable-line react-hooks/exhaustive-deps

  const showMenu = signedIn && (basemaps.length > 1 || !!props.onLayersChange);
  const layerNames: (keyof LayerToggles)[] = ["path", "points", "heatmap", "visits", "places", ...(props.people?.length ? (["family"] as const) : [])];

  return (
    <div className={cn("relative overflow-hidden bg-surface-2", props.className)}>
      {/* sized with h-full, not absolute: MapLibre's CSS forces position:relative on this element */}
      <div ref={el} className="h-full w-full" role="region" aria-label={props.label} />
      {!styleUrl && <Skeleton className="absolute inset-0 rounded-none" />}
      {showMenu && (
        <div className="absolute top-3 right-3 z-10">
          <button
            ref={layersBtn}
            onClick={() => setMenu(!menu)}
            aria-expanded={menu}
            className="grid size-10 place-items-center rounded-xl border border-border bg-surface text-fg shadow-pop hover:bg-surface-2"
            aria-label={t("map.layers")}
            title={t("map.layers")}
          >
            <Layers className="size-5" />
          </button>
          <Popover anchor={layersBtn} open={menu} onClose={() => setMenu(false)} align="end" className="w-72 p-1.5">
            {basemaps.length > 1 && (
              <>
                <p className="px-3 pt-1.5 pb-1.5 text-xs font-semibold tracking-wide text-subtle uppercase">{t("map.basemap")}</p>
                <div role="radiogroup" aria-label={t("map.basemap")}>
                  {basemaps.map((b) => (
                    <button
                      key={b.id}
                      role="radio"
                      aria-checked={b.id === bm?.id}
                      onClick={() => savePrefs({ basemap: b.id })}
                      className="flex h-10 w-full items-center gap-2.5 rounded-lg px-3 text-left text-sm hover:bg-surface-2"
                    >
                      <span className="grid size-4 place-items-center text-primary">{b.id === bm?.id && <Check className="size-4" />}</span>
                      <span className="min-w-0 flex-1 truncate">{b.name}</span>
                      {b.offline && <WifiOff className="size-4 text-subtle" aria-label={t("map.offline")} />}
                    </button>
                  ))}
                </div>
              </>
            )}
            {props.onLayersChange && (
              <>
                <p className="mt-1 border-t border-border px-3 pt-3 pb-1.5 text-xs font-semibold tracking-wide text-subtle uppercase">{t("map.show")}</p>
                {layerNames.map((k) => (
                  <div key={k} className="flex h-10 items-center justify-between gap-3 rounded-lg px-3 text-sm hover:bg-surface-2">
                    {t(`map.layer.${k}`)}
                    <Switch checked={layers[k]} onChange={(v) => props.onLayersChange!({ ...layers, [k]: v })} label={t(`map.layer.${k}`)} />
                  </div>
                ))}
              </>
            )}
          </Popover>
        </div>
      )}
      {failed && <div className="absolute inset-x-4 top-16 rounded-lg border border-border bg-surface px-4 py-3 text-sm shadow-pop">{t("map.styleFailed")}</div>}
    </div>
  );
}

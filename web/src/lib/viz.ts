import { useState } from "react";
import type { PointRow } from "./api";
import { haversine } from "./data";

// How recorded points are drawn and filtered on the map. Everything here is derived on the client from
// the points the server already sent, so it updates the moment a live fix arrives.

export type ColorBy = "solid" | "speed" | "time" | "altitude";
export type WidthBy = "normal" | "bold" | "speed";
export type HeatBy = "density" | "dwell" | "speed";
export type Motion = "all" | "moving" | "still";

export type Viz = {
  color: ColorBy;
  width: WidthBy;
  heat: HeatBy;
  /** m/s. `maxSpeed` at SPEED_CAP means "no upper limit". */
  minSpeed: number;
  maxSpeed: number;
  /** Local hours, 0..24. from > to wraps past midnight (22 → 6). */
  hourFrom: number;
  hourTo: number;
  /** Meters. ACC_CAP means "any accuracy". */
  maxAcc: number;
  motion: Motion;
};

export const SPEED_CAP = 70; // 252 km/h
export const ACC_CAP = 500;
export const MOVING = 0.5; // m/s: below this a fix counts as standing still
const GAP_MS = 60 * 60 * 1000;
const MAX_DT = 10 * 60 * 1000; // time credited to one segment, so a long silence isn't "moving"

export const defaultViz: Viz = { color: "speed", width: "normal", heat: "density", minSpeed: 0, maxSpeed: SPEED_CAP, hourFrom: 0, hourTo: 24, maxAcc: ACC_CAP, motion: "all" };

const KEY = "gt-viz";

function load(): Viz {
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) ?? "{}") as Partial<Viz>;
    const v = { ...defaultViz, ...raw };
    const num = (x: unknown, d: number) => (typeof x === "number" && isFinite(x) ? x : d);
    return {
      color: (["solid", "speed", "time", "altitude"] as const).includes(v.color) ? v.color : defaultViz.color,
      width: (["normal", "bold", "speed"] as const).includes(v.width) ? v.width : defaultViz.width,
      heat: (["density", "dwell", "speed"] as const).includes(v.heat) ? v.heat : defaultViz.heat,
      minSpeed: Math.max(0, num(v.minSpeed, 0)),
      maxSpeed: Math.min(SPEED_CAP, num(v.maxSpeed, SPEED_CAP)),
      hourFrom: Math.min(24, Math.max(0, num(v.hourFrom, 0))),
      hourTo: Math.min(24, Math.max(0, num(v.hourTo, 24))),
      maxAcc: Math.min(ACC_CAP, Math.max(5, num(v.maxAcc, ACC_CAP))),
      motion: (["all", "moving", "still"] as const).includes(v.motion) ? v.motion : "all",
    };
  } catch {
    return defaultViz;
  }
}

/** Visualization settings, remembered per browser and shared by the map and timeline. */
export function useViz(): [Viz, (v: Viz) => void] {
  const [viz, set] = useState<Viz>(load);
  return [
    viz,
    (v) => {
      set(v);
      try {
        localStorage.setItem(KEY, JSON.stringify(v));
      } catch {
        /* private mode */
      }
    },
  ];
}

/** Number of filters that currently hide something. */
export function activeFilters(v: Viz): number {
  return (
    (v.minSpeed > 0 || v.maxSpeed < SPEED_CAP ? 1 : 0) +
    (v.hourFrom > 0 || v.hourTo < 24 ? 1 : 0) +
    (v.maxAcc < ACC_CAP ? 1 : 0) +
    (v.motion !== "all" ? 1 : 0)
  );
}

export const resetFilters = (v: Viz): Viz => ({ ...v, minSpeed: 0, maxSpeed: SPEED_CAP, hourFrom: 0, hourTo: 24, maxAcc: ACC_CAP, motion: "all" });

/** Points that survived the filters, with the speed at each (m/s) and their index in the unfiltered list. */
export type Filtered = { rows: PointRow[]; speed: number[]; src: number[]; total: number };

/** Speed at every fix: the phone's own reading when it has one, otherwise derived from the previous fix. */
export function speedsOf(rows: PointRow[]): number[] {
  const out = new Array<number>(rows.length).fill(0);
  for (let i = 0; i < rows.length; i++) {
    const r = rows[i];
    let v = r[4] != null && r[4] >= 0 ? r[4] : NaN;
    if (isNaN(v)) {
      const a = i > 0 ? rows[i - 1] : r, b = i > 0 ? r : rows[i + 1];
      const dt = b ? (b[0] - a[0]) / 1000 : 0;
      v = b && dt > 0 && dt < 3600 ? haversine(a[1], a[2], b[1], b[2]) / dt : 0;
    }
    out[i] = v < 350 ? v : 0; // faster than an airliner is a GPS glitch
  }
  return out;
}

export function filterRows(rows: PointRow[], v: Viz): Filtered {
  const speed = speedsOf(rows);
  const hours = v.hourFrom > 0 || v.hourTo < 24;
  const out: Filtered = { rows: [], speed: [], src: [], total: rows.length };
  for (let i = 0; i < rows.length; i++) {
    const r = rows[i], s = speed[i];
    if (s < v.minSpeed || (v.maxSpeed < SPEED_CAP && s > v.maxSpeed)) continue;
    if (v.motion === "moving" && s < MOVING) continue;
    if (v.motion === "still" && s >= MOVING) continue;
    if (v.maxAcc < ACC_CAP && r[3] != null && r[3] > v.maxAcc) continue;
    if (hours) {
      const d = new Date(r[0]);
      const h = d.getHours() + d.getMinutes() / 60;
      const inside = v.hourFrom <= v.hourTo ? h >= v.hourFrom && h < v.hourTo : h >= v.hourFrom || h < v.hourTo;
      if (!inside) continue;
    }
    out.rows.push(r);
    out.speed.push(s);
    out.src.push(i);
  }
  return out;
}

/** Whether filtered fixes i-1 and i followed each other in the recording and close enough in time to join. */
const joined = (f: Filtered, i: number) => f.src[i] === f.src[i - 1] + 1 && f.rows[i][0] - f.rows[i - 1][0] <= GAP_MS;

/** [lon, lat] line segments, broken wherever tracking paused or a filter removed fixes in between. */
export function pathSegments(f: Filtered): [number, number][][] {
  const segs: [number, number][][] = [];
  let cur: [number, number][] = [];
  for (let i = 0; i < f.rows.length; i++) {
    if (i > 0 && !joined(f, i) && cur.length) {
      segs.push(cur);
      cur = [];
    }
    cur.push([f.rows[i][2], f.rows[i][1]]);
  }
  if (cur.length) segs.push(cur);
  return segs;
}

export type Extent = { from: number; to: number; altMin: number | null; altMax: number | null; vMax: number };

export function extentOf(f: Filtered): Extent {
  let altMin: number | null = null, altMax: number | null = null, vMax = 0;
  for (let i = 0; i < f.rows.length; i++) {
    const a = f.rows[i][5];
    if (a != null) {
      altMin = altMin == null ? a : Math.min(altMin, a);
      altMax = altMax == null ? a : Math.max(altMax, a);
    }
    vMax = Math.max(vMax, f.speed[i]);
  }
  return { from: f.rows[0]?.[0] ?? 0, to: f.rows.at(-1)?.[0] ?? 0, altMin, altMax, vMax };
}

// ── Colour scales (shared by the map layers and the legend) ──

export type Stop = [number, string];
export const SPEED_STOPS: Stop[] = [[0, "#6366f1"], [1.4, "#22c55e"], [4, "#eab308"], [14, "#f97316"], [33, "#ef4444"], [70, "#c026d3"]];
export const TIME_STOPS: Stop[] = [[0, "#6366f1"], [0.25, "#06b6d4"], [0.5, "#22c55e"], [0.75, "#eab308"], [1, "#f97316"]];
export const ALT_STOPS: Stop[] = [[0, "#0ea5e9"], [0.33, "#22c55e"], [0.66, "#eab308"], [1, "#b45309"]];
export const MISSING = "#94a3b8";

/** Speed bands for the breakdown bar: upper bound (m/s), colour. */
export const BANDS: { key: "still" | "walk" | "bike" | "drive" | "fast"; max: number; color: string }[] = [
  { key: "still", max: MOVING, color: "#6366f1" },
  { key: "walk", max: 2.2, color: "#22c55e" },
  { key: "bike", max: 7, color: "#eab308" },
  { key: "drive", max: 33, color: "#f97316" },
  { key: "fast", max: Infinity, color: "#ef4444" },
];

export type VizStats = {
  shown: number;
  total: number;
  length: number;
  top: number;
  avgMoving: number;
  movingMs: number;
  stillMs: number;
  /** Time spent in each band (ms), in BANDS order. */
  bands: number[];
  /** Fixes recorded in each local hour of day. */
  hours: number[];
};

export function vizStats(f: Filtered): VizStats {
  const bands = BANDS.map(() => 0);
  const hours = new Array<number>(24).fill(0);
  let length = 0, movingMs = 0, stillMs = 0, movingDist = 0;
  for (let i = 0; i < f.rows.length; i++) {
    hours[new Date(f.rows[i][0]).getHours()]++;
    if (i === 0 || !joined(f, i)) continue;
    const a = f.rows[i - 1], b = f.rows[i];
    const d = haversine(a[1], a[2], b[1], b[2]);
    const dt = Math.min(b[0] - a[0], MAX_DT);
    const v = f.speed[i];
    length += d;
    bands[BANDS.findIndex((x) => v < x.max)] += dt;
    if (v >= MOVING) {
      movingMs += dt;
      movingDist += d;
    } else stillMs += dt;
  }
  const sorted = [...f.speed].sort((a, b) => a - b);
  return {
    shown: f.rows.length,
    total: f.total,
    length,
    top: sorted[Math.floor(sorted.length * 0.95)] ?? 0, // 95th percentile: one bad fix isn't a top speed
    avgMoving: movingMs > 0 ? movingDist / (movingMs / 1000) : 0,
    movingMs,
    stillMs,
    bands,
    hours,
  };
}

/** GeoJSON line features, one per pair of consecutive fixes, carrying what the colour scales read. */
export function coloredFeatures(f: Filtered, ext: Extent) {
  const span = Math.max(1, ext.to - ext.from);
  const altSpan = ext.altMin != null && ext.altMax != null ? Math.max(1, ext.altMax - ext.altMin) : 0;
  const features: object[] = [];
  for (let i = 1; i < f.rows.length; i++) {
    if (!joined(f, i)) continue;
    const a = f.rows[i - 1], b = f.rows[i];
    const alt = b[5] ?? a[5];
    features.push({
      type: "Feature",
      properties: {
        i,
        v: f.speed[i],
        f: (b[0] - ext.from) / span,
        e: alt != null && ext.altMin != null ? (alt - ext.altMin) / altSpan : -1,
      },
      geometry: { type: "LineString", coordinates: [[a[2], a[1]], [b[2], b[1]]] },
    });
  }
  return { type: "FeatureCollection" as const, features };
}

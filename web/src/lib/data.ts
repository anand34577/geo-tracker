import { useQuery } from "@tanstack/react-query";
import { api, type FamilyPerson, type Group, type Place, type Point, type PointRow, type PointsResponse, type Timeline } from "./api";

const GAP_MS = 60 * 60 * 1000;

/** Splits point rows into [lon, lat] segments wherever tracking paused for over an hour. */
export function toSegments(rows: PointRow[]): [number, number][][] {
  const segs: [number, number][][] = [];
  let cur: [number, number][] = [];
  let last = 0;
  for (const r of rows) {
    if (last && r[0] - last > GAP_MS && cur.length) {
      segs.push(cur);
      cur = [];
    }
    cur.push([r[2], r[1]]);
    last = r[0];
  }
  if (cur.length) segs.push(cur);
  return segs;
}

/** user: view a family member (the server enforces what they share). */
export const usePoints = (from: number, to: number, user?: number) =>
  useQuery({ queryKey: ["points", from, to, user], queryFn: () => api<PointsResponse>(`/points?from=${from}&to=${to}${user ? `&user=${user}` : ""}`) });

export const useTimeline = (from: number, to: number, user?: number) =>
  useQuery({ queryKey: ["timeline", from, to, user], queryFn: () => api<Timeline>(`/timeline?from=${from}&to=${to}${user ? `&user=${user}` : ""}`) });

export const useFamily = () => useQuery({ queryKey: ["family"], queryFn: () => api<FamilyPerson[]>("/family"), refetchInterval: 60_000 });
export const useGroups = () => useQuery({ queryKey: ["groups"], queryFn: () => api<Group[]>("/groups") });

export const usePlaces = () => useQuery({ queryKey: ["places"], queryFn: () => api<Place[]>("/places") });

export const useLatest = () => useQuery({ queryKey: ["latest"], queryFn: () => api<Point | null>("/points/latest") });

export function summarize(tl: Timeline | undefined) {
  const trips = tl?.trips ?? [];
  return {
    distance: trips.reduce((s, t) => s + t.distance, 0),
    moving: trips.reduce((s, t) => s + (t.end - t.start), 0),
    places: new Set((tl?.visits ?? []).map((v) => v.place_id ?? `${v.lat.toFixed(3)},${v.lon.toFixed(3)}`)).size,
  };
}

export const tz = Intl.DateTimeFormat().resolvedOptions().timeZone;

/** Points per local day (yyyy-MM-dd → count) for calendar dots and the activity heatmap. */
export const useDays = (from: number, to: number, enabled = true) =>
  useQuery({
    queryKey: ["days", from, to],
    queryFn: () => api<Record<string, number>>(`/days?from=${from}&to=${to}&tz=${encodeURIComponent(tz)}`),
    staleTime: 60_000,
    enabled,
  });

/** Days with data around a displayed month (±1 month so leading/trailing grid days get dots too). */
export function useMonthDays(month: Date) {
  const from = new Date(month.getFullYear(), month.getMonth() - 1, 1).getTime();
  const to = new Date(month.getFullYear(), month.getMonth() + 2, 0, 23, 59, 59).getTime();
  return useDays(from, to).data;
}
/** Great-circle distance in meters. */
export function haversine(lat1: number, lon1: number, lat2: number, lon2: number): number {
  const r = Math.PI / 180, a = Math.sin(((lat2 - lat1) * r) / 2) ** 2 + Math.cos(lat1 * r) * Math.cos(lat2 * r) * Math.sin(((lon2 - lon1) * r) / 2) ** 2;
  return 12_742_000 * Math.asin(Math.sqrt(a));
}

export type TripStats = {
  path: [number, number][];
  /** [ts, m/s], smoothed over 3 fixes. */
  speed: [number, number][];
  /** [ts, meters], only when the phone reported altitude. */
  elevation: [number, number][];
  topSpeed: number;
  climb: number;
  descent: number;
};

/** Speed and elevation profile of the points recorded during a trip. */
export function tripStats(rows: PointRow[], start: number, end: number): TripStats {
  const pts = rows.filter((r) => r[0] >= start && r[0] <= end);
  const raw: [number, number][] = [];
  for (let i = 1; i < pts.length; i++) {
    const a = pts[i - 1], b = pts[i], dt = (b[0] - a[0]) / 1000;
    // The phone's own speed reading beats one derived from two fixes.
    const v = b[4] ?? (dt > 0 ? haversine(a[1], a[2], b[1], b[2]) / dt : 0);
    if (v < 350) raw.push([b[0], v]); // faster than an airliner is a GPS glitch
  }
  const speed = raw.map(([t], i) => {
    const w = raw.slice(Math.max(0, i - 1), i + 2);
    return [t, w.reduce((s, x) => s + x[1], 0) / w.length] as [number, number];
  });
  const elevation = pts.filter((r) => r[5] != null).map((r) => [r[0], r[5]!] as [number, number]);
  // Climb counts only changes over 3 m, so GPS altitude jitter doesn't add up to a mountain.
  let climb = 0, descent = 0, ref = elevation[0]?.[1];
  for (const [, alt] of elevation) {
    if (alt - ref! >= 3) { climb += alt - ref!; ref = alt; }
    else if (ref! - alt >= 3) { descent += ref! - alt; ref = alt; }
  }
  const sorted = speed.map((s) => s[1]).sort((a, b) => a - b);
  return {
    path: pts.map((r) => [r[2], r[1]]),
    speed,
    elevation,
    topSpeed: sorted[Math.floor(sorted.length * 0.95)] ?? 0, // 95th percentile: one bad fix isn't a top speed
    climb,
    descent,
  };
}

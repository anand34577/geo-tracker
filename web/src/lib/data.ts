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
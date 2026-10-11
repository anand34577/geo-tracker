import { format as fmt } from "date-fns";
import type { Prefs, TripMode } from "./api";
import i18n from "./i18n";
import { cachedPrefs } from "./prefs";

// Callers without the prefs at hand still get the user's 12h/24h choice.
const clockPref = (): Prefs["clock"] => cachedPrefs().clock ?? "24h";

export function distance(m: number, units: Prefs["units"] = "metric"): string {
  if (units === "imperial") {
    const mi = m / 1609.344;
    return mi < 0.1 ? `${Math.round(m * 3.28084)} ft` : `${mi < 10 ? mi.toFixed(1) : Math.round(mi)} mi`;
  }
  return m < 1000 ? `${Math.round(m)} m` : `${m < 10_000 ? (m / 1000).toFixed(1) : Math.round(m / 1000)} km`;
}

export function speed(ms: number, units: Prefs["units"] = "metric"): string {
  return units === "imperial" ? `${Math.round(ms * 2.23694)} mph` : `${Math.round(ms * 3.6)} km/h`;
}

export function duration(ms: number): string {
  const mins = Math.max(0, Math.round(ms / 60_000));
  if (mins < 60) return `${mins} min`;
  const h = Math.floor(mins / 60);
  if (h < 48) return mins % 60 ? `${h} h ${mins % 60} min` : `${h} h`;
  return `${Math.round(h / 24)} d`;
}

export function time(ts: number, clock: Prefs["clock"] = clockPref()): string {
  return fmt(ts, clock === "12h" ? "h:mm a" : "HH:mm");
}

export function dateTime(ts: number, clock: Prefs["clock"] = clockPref()): string {
  return fmt(ts, clock === "12h" ? "d MMM yyyy, h:mm a" : "d MMM yyyy, HH:mm");
}

export function longDate(d: Date): string {
  return d.toLocaleDateString(i18n.language, { weekday: "long", day: "numeric", month: "long", year: "numeric" });
}

const rtf = () => new Intl.RelativeTimeFormat(i18n.language, { numeric: "auto" });

export function ago(ts: number): string {
  const s = Math.round((ts - Date.now()) / 1000);
  const abs = Math.abs(s);
  if (abs < 45) return i18n.t("common.justNow");
  if (abs < 3600) return rtf().format(Math.round(s / 60), "minute");
  if (abs < 86400) return rtf().format(Math.round(s / 3600), "hour");
  return rtf().format(Math.round(s / 86400), "day");
}

export function bytes(n: number): string {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  return `${n < 10 && i > 0 ? n.toFixed(1) : Math.round(n)} ${units[i]}`;
}

export const number = (n: number) => n.toLocaleString(i18n.language);

export const modeLabel = (m: TripMode) => i18n.t(`mode.${m}`);

/** yyyy-MM-dd for the local day of ts. */
export const dayKey = (d: Date | number) => fmt(d, "yyyy-MM-dd");

/** Local-day bounds [start, end] in ms for a yyyy-MM-dd key. */
export function dayBounds(key: string): [number, number] {
  const [y, m, d] = key.split("-").map(Number);
  const start = new Date(y, m - 1, d).getTime();
  const end = new Date(y, m - 1, d + 1).getTime() - 1;
  return [start, end];
}

import { MutationCache, QueryClient, useQuery } from "@tanstack/react-query";

// ── Types (mirror the Go structs) ────────────────────────────

export type Prefs = {
  theme?: "system" | "light" | "dark";
  accent?: "teal" | "blue" | "rose" | "amber" | "green" | "slate";
  units?: "metric" | "imperial";
  clock?: "24h" | "12h";
  basemap?: string;
};

export type User = {
  id: number;
  email: string;
  name: string;
  role: "admin" | "user";
  prefs: Prefs;
  disabled: boolean;
  created_at: number;
};

export type Basemap = { id: string; name: string; light: string; dark: string; offline?: boolean };
export type Config = { version: string; base_url: string; basemaps: Basemap[]; basemap_default: string; geocoder: boolean };
export type AuthMethods = { password: boolean; oidc: boolean; oidc_label: string };
export type GeocodeHit = { name: string; address: string; lat: number; lon: number };

export type NotifyEvent = "place_arrive" | "place_leave" | "family" | "device_silent" | "battery_low" | "import_done" | "backup_failed";
export type NotifyPrefs = {
  email: { enabled: boolean; to: string };
  gotify: { enabled: boolean; url: string; token: string; priority: number };
  ntfy: { enabled: boolean; url: string; topic: string; token: string; priority: number };
  telegram: { enabled: boolean; token: string; chat_id: string };
  events: Record<NotifyEvent, boolean>;
  silent_hours: number;
  battery_below: number;
};

export type AutoAction = {
  id: string;
  type: "webhook" | "notify_me" | "notify_family" | "ntfy" | "telegram" | "discord" | "slack" | "email";
  message?: string;
  url?: string;
  method?: string;
  body?: string;
  headers?: Record<string, string>;
  topic?: string;
  token?: string;
  chat_id?: string;
  to?: string;
  members?: number[];
};
export type Automation = {
  id: number;
  place_id: number;
  place_name: string;
  name: string;
  on_arrive: boolean;
  on_leave: boolean;
  enabled: boolean;
  cooldown_min: number;
  actions: AutoAction[];
  last_fired_at: number | null;
  last_event: string;
  last_result: string;
  created_at: number;
};

export type Point = { ts: number; lat: number; lon: number; acc?: number; speed?: number; alt?: number; batt?: number };
/** Compact row: [ts, lat, lon, acc, speed, alt, batt] */
export type PointRow = [number, number, number, number | null, number | null, number | null, number | null];
export type PointsResponse = { total: number; step: number; points: PointRow[] };

export type Visit = {
  id: number;
  start: number;
  end: number;
  lat: number;
  lon: number;
  radius: number;
  points: number;
  name?: string;
  address?: string;
  city?: string;
  country?: string;
  place_id?: number;
  place_name?: string;
  place_icon?: string;
};
export type TripMode = "walk" | "cycle" | "drive" | "train" | "flight" | "unknown";
export type Trip = { id: number; start: number; end: number; distance: number; mode: TripMode; confidence: number; corrected?: boolean };

export type Member = {
  user_id: number;
  name: string;
  email: string;
  role: "owner" | "member";
  status: "invited" | "active";
  share_live: boolean;
  share_history_days: number;
  precision: "exact" | "approx";
  paused_until: number | null;
  color: string;
  joined_at: number | null;
};
export type Group = { id: number; name: string; created_at: number; members: Member[] };
export type FamilyPerson = {
  user_id: number;
  name: string;
  color: string;
  live: boolean;
  approx: boolean;
  history_from: number | null;
  paused: boolean;
  point: Point | null;
  place?: string;
};
export type AlertRule = { id: number; subject_id: number; subject_name: string; place_id: number; place_name: string; on_arrive: boolean; on_leave: boolean };
export type SessionInfo = { id: string; user_agent: string; ip: string; created_at: number; expires_at: number; current: boolean };
export type AuditEntry = { id: number; ts: number; actor: string; action: string; target: string; ip: string };
export type Timeline = { visits: Visit[]; trips: Trip[] };

export type Place = {
  id: number;
  name: string;
  icon: string;
  lat: number;
  lon: number;
  radius: number;
  private: boolean;
  created_at: number;
  visits: number;
  total_ms: number;
  last_visit: number | null;
};

export type Device = {
  id: number;
  name: string;
  client: string;
  last_seen_at: number | null;
  last_battery: number | null;
  created_at: number;
};

export type Import = {
  id: number;
  filename: string;
  format: string;
  status: "queued" | "running" | "done" | "failed";
  added: number;
  duplicates: number;
  rejected: number;
  error?: string;
  created_at: number;
  finished_at: number | null;
};

export type DataExport = {
  id: number;
  format: "native" | "gpx" | "geojson" | "csv";
  from: number | null;
  to: number | null;
  status: "queued" | "running" | "done" | "failed";
  size: number;
  error?: string;
  created_at: number;
  finished_at: number | null;
};

export type Backup = { name: string; size: number; created: number };
export type Settings = Record<string, string> & { oidc_callback_url?: string; map_mbtiles_error?: string };
export type System = {
  version: string;
  go: string;
  os: string;
  started: number;
  db_size: number;
  counts: { users: number; points: number; visits: number; trips: number };
  data_dir: string;
  base_url: string;
  https: boolean;
  last_backup: Backup | null;
  backups: number;
};
export type PointStats = { count: number; first: number | null; last: number | null };

// ── Fetch wrapper ────────────────────────────────────────────

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
  }
}

export async function api<T = unknown>(path: string, init: { method?: string; body?: unknown } = {}): Promise<T> {
  const res = await fetch("/api/v1" + path, {
    method: init.method ?? "GET",
    headers: init.body !== undefined ? { "Content-Type": "application/json" } : undefined,
    body: init.body !== undefined ? JSON.stringify(init.body) : undefined,
    credentials: "same-origin",
  });
  if (!res.ok) {
    let msg = res.statusText;
    try {
      msg = (await res.json()).title ?? msg;
    } catch {
      /* not JSON */
    }
    if (res.status === 401 && path !== "/me" && path !== "/auth/login") {
      queryClient.setQueryData(["me"], null); // session expired: the app shell shows the login screen
    }
    throw new ApiError(res.status, msg);
  }
  if (res.status === 204 || res.status === 202) {
    const text = await res.text();
    return (text ? JSON.parse(text) : undefined) as T;
  }
  return res.json();
}

/** Upload a file with progress (fetch cannot report upload progress). */
export function upload<T>(path: string, file: File, onProgress: (fraction: number) => void): Promise<T> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("POST", "/api/v1" + path);
    xhr.upload.onprogress = (e) => e.lengthComputable && onProgress(e.loaded / e.total);
    xhr.onload = () => {
      let body: any = {};
      try {
        body = JSON.parse(xhr.responseText);
      } catch {
        /* empty */
      }
      if (xhr.status >= 200 && xhr.status < 300) resolve(body);
      else reject(new ApiError(xhr.status, body.title ?? xhr.statusText));
    };
    xhr.onerror = () => reject(new ApiError(0, "Network error"));
    const form = new FormData();
    form.append("file", file);
    xhr.send(form);
  });
}

let errorToast: (message: string) => void = () => {};
/** The toast provider registers here, so a failed action is never silent. */
export const setErrorToast = (fn: (message: string) => void) => {
  errorToast = fn;
};

export const queryClient = new QueryClient({
  // Mutations that show their own error (onError, or inline via meta.inline) are skipped.
  mutationCache: new MutationCache({
    onError: (err, _vars, _ctx, m) => {
      if (!m.options.onError && !m.meta?.inline) errorToast(err.message);
    },
  }),
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: (count, err) => !(err instanceof ApiError && err.status < 500) && count < 2,
    },
  },
});

// ── Shared queries ───────────────────────────────────────────

export function useMe() {
  return useQuery({
    queryKey: ["me"],
    queryFn: async () => {
      try {
        return await api<User>("/me");
      } catch (e) {
        if (e instanceof ApiError && e.status === 401) return null;
        throw e;
      }
    },
    staleTime: Infinity,
  });
}

/** The signed-in user. Inside the authenticated shell it is always set. */
export function useUser(): User {
  return useMe().data!;
}

export type Insights = {
  distance: number;
  moving_ms: number;
  visits: number;
  trips: number;
  places: number;
  days_tracked: number;
  daily: { day: string; distance: number; moving_ms: number; visits: number; points: number }[];
  modes: { mode: TripMode; distance: number; ms: number; trips: number }[];
  top_places: { name: string; city?: string; country?: string; lat: number; lon: number; place_id?: number; icon?: string; visits: number; ms: number }[];
  countries: { name: string; visits: number; ms: number; first: number }[];
  cities: { name: string; country?: string; visits: number; ms: number; first: number }[];
  longest_trip: Trip | null;
};

export type Share = {
  id: number;
  name: string;
  kind: "live" | "range";
  from: number | null;
  to: number | null;
  precision: "exact" | "approx";
  expires_at: number;
  views: number;
  created_at: number;
};

export function useConfig(enabled = true) {
  return useQuery({ queryKey: ["config"], queryFn: () => api<Config>("/config"), staleTime: Infinity, enabled });
}

export function rowToPoint(r: PointRow): Point {
  return { ts: r[0], lat: r[1], lon: r[2], acc: r[3] ?? undefined, speed: r[4] ?? undefined, alt: r[5] ?? undefined, batt: r[6] ?? undefined };
}

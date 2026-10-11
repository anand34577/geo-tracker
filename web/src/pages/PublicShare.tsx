import { useEffect, useMemo, useRef, useState } from "react";
import { useParams } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { Clock, LinkIcon, Radio } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, type PointRow } from "../lib/api";
import { ago, dateTime, time } from "../lib/format";
import { applyPrefs, cachedPrefs } from "../lib/prefs";
import { toSegments } from "../lib/data";
import { rangeLabel } from "../components/controls";
import MapView from "../components/MapView";
import { Logo } from "../components/Logo";
import { EmptyState, Spinner } from "../components/ui";

type PublicShareData = {
  name: string;
  owner: string;
  kind: "live" | "range";
  precision: "exact" | "approx";
  expires_at: number;
  from: number | null;
  to: number | null;
  points: [number, number, number, number | null][];
  latest?: [number, number, number, number | null];
  visits?: { start: number; end: number; lat: number; lon: number; name: string; city: string }[];
};

/** What someone sees when they open a share link. No account needed. */
export default function PublicShare() {
  const { t } = useTranslation();
  const { token } = useParams();
  useEffect(() => { applyPrefs(cachedPrefs()); }, []);
  const opened = useRef(false); // the first load counts as a view, live refreshes don't
  const q = useQuery({
    queryKey: ["public-share", token],
    queryFn: async () => {
      const d = await api<PublicShareData>(`/public/shares/${token}${opened.current ? "?poll=1" : ""}`);
      opened.current = true;
      return d;
    },
    refetchInterval: (query) => (query.state.data?.kind === "live" ? 30_000 : false),
    retry: false,
  });
  const [, tick] = useState(0);
  useEffect(() => {
    const id = setInterval(() => tick((n) => n + 1), 30_000);
    return () => clearInterval(id);
  }, []);
  const d = q.data;
  const rows = useMemo(() => (d?.points ?? []).map((p) => [p[0], p[1], p[2], p[3], null, null, null] as PointRow), [d]);
  const path = useMemo(() => toSegments(rows), [rows]);

  if (q.isPending) return <Spinner />;
  if (!d) {
    return (
      <main className="grid min-h-dvh place-items-center bg-bg p-6">
        <EmptyState icon={LinkIcon} title={t("share.publicGone")} body={q.error?.message} />
      </main>
    );
  }
  const latest = d.latest ? { lat: d.latest[1], lon: d.latest[2], acc: d.latest[3] ?? (d.precision === "approx" ? 1000 : undefined) } : null;
  return (
    <main className="relative h-dvh">
      <MapView
        publicMode
        className="h-full"
        label={t("share.mapLabel", { name: d.owner })}
        path={path}
        visits={d.visits?.map((v, i) => ({ id: i, lat: v.lat, lon: v.lon }))}
        me={latest}
        fit={d.kind === "live" ? (latest ? "live" : undefined) : "range"}
        // The info card floats top-left: keep the route and the live dot clear of it.
        fitPadding={window.innerWidth >= 640 ? { top: 60, bottom: 60, left: 430, right: 60 } : { top: d.visits?.length ? 300 : 190, bottom: 60, left: 40, right: 40 }}
      />
      <div className="absolute top-3 left-3 z-10 w-[min(24rem,calc(100%-1.5rem))] rounded-2xl border border-border bg-surface/95 p-4 shadow-pop backdrop-blur-sm">
        <div className="flex items-center gap-2.5">
          <Logo className="size-7" />
          <div className="min-w-0">
            <h1 className="truncate font-semibold">{d.name}</h1>
            <p className="text-sm text-muted">{t("share.sharedBy", { name: d.owner })}</p>
          </div>
        </div>
        <div className="mt-3 space-y-1.5 border-t border-border pt-3 text-sm">
          {d.kind === "live" ? (
            <p className="flex items-center gap-2">
              <Radio className="size-4 text-primary" aria-hidden />
              {d.latest ? t("share.lastUpdate", { when: ago(d.latest[0]) }) : t("share.noLocationYet")}
            </p>
          ) : (
            <p className="flex items-center gap-2"><Clock className="size-4 text-primary" aria-hidden />{rangeLabel({ from: d.from!, to: d.to! })}</p>
          )}
          <p className="text-xs text-subtle">
            {t("share.linkExpires", { when: dateTime(d.expires_at) })}
            {d.precision === "approx" && ` · ${t("share.approxShort")}`}
          </p>
        </div>
        {!!d.visits?.length && (
          <ol className="mt-3 max-h-48 space-y-1.5 overflow-y-auto border-t border-border pt-3 text-sm">
            {d.visits.map((v, i) => (
              <li key={i} className="flex justify-between gap-3">
                <span className="truncate">{v.name || v.city || t("timeline.unknownPlace")}</span>
                <span className="shrink-0 text-subtle tabular-nums">{time(v.start)} – {time(v.end)}</span>
              </li>
            ))}
          </ol>
        )}
      </div>
    </main>
  );
}

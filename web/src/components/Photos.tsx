import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight, Image as ImageIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api } from "../lib/api";
import { dateTime, time } from "../lib/format";
import { Button, Dialog } from "./ui";

export type Photo = { id: string; ts: number; lat?: number; lon?: number };

export const usePhotos = (from: number, to: number, enabled: boolean) =>
  useQuery({ queryKey: ["photos", from, to], queryFn: () => api<Photo[]>(`/photos?from=${from}&to=${to}`), enabled, staleTime: 5 * 60_000, retry: false });

const thumb = (id: string, size: "thumbnail" | "preview" = "thumbnail") => `/api/v1/photos/${id}/thumb?size=${size}`;

/** Photos taken during the shown period (from Immich), with a simple viewer. */
export function PhotoStrip({ photos, clock, onLocate }: { photos: Photo[]; clock: "24h" | "12h"; onLocate: (p: Photo) => void }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState<number | null>(null);
  const cur = open != null ? photos[open] : null;
  useEffect(() => {
    if (open == null) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "ArrowRight") setOpen((i) => Math.min((i ?? 0) + 1, photos.length - 1));
      if (e.key === "ArrowLeft") setOpen((i) => Math.max((i ?? 0) - 1, 0));
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, photos.length]);
  if (!photos.length) return null;
  return (
    <section className="border-b border-border px-4 py-3" aria-label={t("photos.title")}>
      <p className="mb-2 flex items-center gap-1.5 text-xs font-semibold tracking-wide text-subtle uppercase">
        <ImageIcon className="size-3.5" aria-hidden />
        {t("photos.count", { count: photos.length })}
      </p>
      <ul className="-mx-1 flex gap-2 overflow-x-auto px-1 pb-1">
        {photos.map((p, i) => (
          <li key={p.id} className="shrink-0">
            <button onClick={() => setOpen(i)} className="block overflow-hidden rounded-lg ring-primary focus-visible:ring-2" aria-label={t("photos.open", { time: time(p.ts, clock) })}>
              <img src={thumb(p.id)} alt="" loading="lazy" className="size-16 object-cover transition-transform hover:scale-105" />
            </button>
          </li>
        ))}
      </ul>
      <Dialog open={!!cur} onClose={() => setOpen(null)} wide title={cur ? dateTime(cur.ts, clock) : ""}
        footer={cur && (
          <>
            {cur.lat != null && <Button variant="ghost" onClick={() => { onLocate(cur); setOpen(null); }}>{t("photos.showOnMap")}</Button>}
            <span className="flex-1" />
            <Button size="icon" variant="ghost" disabled={open === 0} onClick={() => setOpen((i) => (i ?? 1) - 1)} aria-label={t("photos.prev")}><ChevronLeft className="size-5" /></Button>
            <span className="self-center text-sm text-muted tabular-nums">{(open ?? 0) + 1} / {photos.length}</span>
            <Button size="icon" variant="ghost" disabled={open === photos.length - 1} onClick={() => setOpen((i) => (i ?? 0) + 1)} aria-label={t("photos.next")}><ChevronRight className="size-5" /></Button>
          </>
        )}>
        {cur && <img src={thumb(cur.id, "preview")} alt={dateTime(cur.ts, clock)} className="mx-auto max-h-[65vh] rounded-lg object-contain" />}
      </Dialog>
    </section>
  );
}

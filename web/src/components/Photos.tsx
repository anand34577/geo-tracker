import { useEffect, useRef, useState, type TouchEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight, Image as ImageIcon, ImageOff, MapPin, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api } from "../lib/api";
import { dateTime, time } from "../lib/format";
import { Button, cn } from "./ui";

export type Photo = { id: string; ts: number; lat?: number; lon?: number };

export const usePhotos = (from: number, to: number, enabled: boolean) =>
  useQuery({ queryKey: ["photos", from, to], queryFn: () => api<Photo[]>(`/photos?from=${from}&to=${to}`), enabled, staleTime: 5 * 60_000, retry: false });

export const photoUrl = (id: string, size: "thumbnail" | "preview" = "thumbnail") => `/api/v1/photos/${id}/thumb?size=${size}`;

const PREVIEW_TILES = 6; // tiles shown in the card before "+N more"

/** Photos taken during the shown period (from Immich): a tidy mosaic that opens a full-screen viewer. */
export function PhotoGallery({ photos, clock, onLocate, locatedId }: { photos: Photo[]; clock: "24h" | "12h"; onLocate: (p: Photo) => void; locatedId?: string | null }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState<number | null>(null);
  if (!photos.length) return null;
  const more = photos.length - PREVIEW_TILES;
  const tiles = photos.slice(0, PREVIEW_TILES);
  return (
    <section className="border-b border-border px-4 py-4" aria-label={t("photos.title")}>
      <div className="mb-3 flex items-center justify-between gap-3">
        <h2 className="flex items-center gap-1.5 text-xs font-semibold tracking-wide text-subtle uppercase">
          <ImageIcon className="size-3.5" aria-hidden />
          {t("photos.title")} · {t("photos.count", { count: photos.length })}
        </h2>
        {photos.length > 1 && <button onClick={() => setOpen(0)} className="text-sm font-medium text-primary hover:underline">{t("photos.viewAll")}</button>}
      </div>
      <ul className="grid grid-cols-3 gap-1.5">
        {tiles.map((p, i) => {
          const last = i === PREVIEW_TILES - 1 && more > 0;
          return (
            <li key={p.id} className={cn("relative aspect-square overflow-hidden rounded-xl bg-surface-3", i === 0 && photos.length >= 3 && "col-span-2 row-span-2")}>
              <button onClick={() => setOpen(i)} className="group block size-full ring-primary focus-visible:ring-2" aria-label={t("photos.open", { time: time(p.ts, clock) })}>
                <img src={photoUrl(p.id, i === 0 && photos.length >= 3 ? "preview" : "thumbnail")} alt="" loading="lazy" className="size-full object-cover transition-transform duration-300 group-hover:scale-105" />
                <span className="absolute inset-x-0 bottom-0 flex items-center gap-1 bg-gradient-to-t from-black/60 to-transparent px-2 pt-5 pb-1.5 text-[11px] font-medium text-white tabular-nums">
                  {p.lat != null && <MapPin className={cn("size-3", locatedId === p.id && "fill-current")} aria-hidden />}
                  {time(p.ts, clock)}
                </span>
                {last && <span className="absolute inset-0 grid place-items-center bg-black/55 text-lg font-semibold text-white">{t("photos.more", { count: more + 1 })}</span>}
              </button>
            </li>
          );
        })}
      </ul>
      <Viewer photos={photos} index={open} clock={clock} onIndex={setOpen} onLocate={(p) => { setOpen(null); onLocate(p); }} />
    </section>
  );
}

function Viewer({ photos, index, clock, onIndex, onLocate }: { photos: Photo[]; index: number | null; clock: "24h" | "12h"; onIndex: (i: number | null) => void; onLocate: (p: Photo) => void }) {
  const { t } = useTranslation();
  const ref = useRef<HTMLDialogElement>(null);
  const strip = useRef<HTMLUListElement>(null);
  const touchX = useRef<number | null>(null);
  const [loaded, setLoaded] = useState<string | null>(null);
  const [broken, setBroken] = useState<string | null>(null);
  const cur = index != null ? photos[index] : null;
  const go = (i: number) => onIndex(Math.min(Math.max(i, 0), photos.length - 1));

  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (cur && !d.open) d.showModal();
    if (!cur && d.open) d.close();
  }, [!cur]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (index == null) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "ArrowRight") go(index + 1);
      if (e.key === "ArrowLeft") go(index - 1);
    };
    window.addEventListener("keydown", onKey);
    // Warm the neighbours so paging feels instant.
    for (const n of [photos[index + 1], photos[index - 1]]) if (n) new Image().src = photoUrl(n.id, "preview");
    strip.current?.querySelector(`[data-i="${index}"]`)?.scrollIntoView({ block: "nearest", inline: "center", behavior: "smooth" });
    return () => window.removeEventListener("keydown", onKey);
  }, [index]); // eslint-disable-line react-hooks/exhaustive-deps

  const onTouchEnd = (e: TouchEvent) => {
    const start = touchX.current;
    touchX.current = null;
    if (start == null || index == null) return;
    const dx = e.changedTouches[0]!.clientX - start;
    if (Math.abs(dx) > 50) go(index + (dx < 0 ? 1 : -1));
  };

  return (
    <dialog
      ref={ref}
      onClose={() => onIndex(null)}
      onClick={(e) => e.target === ref.current && onIndex(null)}
      aria-label={t("photos.title")}
      className="m-0 size-full max-h-none max-w-none bg-transparent p-0 text-white backdrop:bg-black/90 backdrop:backdrop-blur-md"
    >
      {cur && index != null && (
        <div className="flex size-full flex-col" onClick={(e) => e.target === e.currentTarget && onIndex(null)}>
          <header className="flex items-center justify-between gap-3 px-4 py-3">
            <div className="min-w-0">
              <p className="truncate font-medium tabular-nums">{dateTime(cur.ts, clock)}</p>
              <p className="text-xs text-white/60 tabular-nums">{index + 1} / {photos.length}</p>
            </div>
            <div className="flex items-center gap-2">
              {cur.lat != null && cur.lon != null ? (
                <Button variant="primary" size="sm" icon={MapPin} onClick={() => onLocate(cur)}>{t("photos.showOnMap")}</Button>
              ) : (
                <span className="text-xs text-white/60">{t("photos.noLocation")}</span>
              )}
              <button onClick={() => onIndex(null)} aria-label={t("common.close")} className="grid size-9 place-items-center rounded-full bg-white/10 hover:bg-white/20">
                <X className="size-5" />
              </button>
            </div>
          </header>

          <div
            className="relative flex min-h-0 flex-1 items-center justify-center px-2 sm:px-16"
            onClick={(e) => e.target === e.currentTarget && onIndex(null)}
            onTouchStart={(e) => (touchX.current = e.touches[0]!.clientX)}
            onTouchEnd={onTouchEnd}
          >
            {broken === cur.id ? (
              <div className="flex flex-col items-center gap-2 text-white/70"><ImageOff className="size-10" aria-hidden />{t("photos.loadFailed")}</div>
            ) : (
              <>
                {loaded !== cur.id && <div className="absolute size-16 animate-pulse rounded-2xl bg-white/10" aria-hidden />}
                <img
                  key={cur.id}
                  src={photoUrl(cur.id, "preview")}
                  alt={dateTime(cur.ts, clock)}
                  onLoad={() => setLoaded(cur.id)}
                  onError={() => setBroken(cur.id)}
                  className={cn("max-h-full max-w-full rounded-xl object-contain shadow-2xl transition-opacity duration-300", loaded === cur.id ? "opacity-100" : "opacity-0")}
                />
              </>
            )}
            <button onClick={() => go(index - 1)} disabled={index === 0} aria-label={t("photos.prev")}
              className="absolute left-2 grid size-11 place-items-center rounded-full bg-white/10 hover:bg-white/20 disabled:invisible sm:left-4">
              <ChevronLeft className="size-6" />
            </button>
            <button onClick={() => go(index + 1)} disabled={index === photos.length - 1} aria-label={t("photos.next")}
              className="absolute right-2 grid size-11 place-items-center rounded-full bg-white/10 hover:bg-white/20 disabled:invisible sm:right-4">
              <ChevronRight className="size-6" />
            </button>
          </div>

          {photos.length > 1 && (
            <ul ref={strip} className="flex gap-2 overflow-x-auto px-4 py-3" aria-label={t("photos.title")}>
              {photos.map((p, i) => (
                <li key={p.id} data-i={i} className="shrink-0">
                  <button onClick={() => go(i)} aria-label={t("photos.open", { time: time(p.ts, clock) })} aria-current={i === index}
                    className={cn("block overflow-hidden rounded-lg ring-2 transition", i === index ? "opacity-100 ring-white" : "opacity-50 ring-transparent hover:opacity-90")}>
                    <img src={photoUrl(p.id)} alt="" loading="lazy" className="size-14 object-cover" />
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </dialog>
  );
}

import { useEffect, useId, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Loader2, MapPin, Search, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, useConfig, type GeocodeHit } from "../lib/api";
import { cn } from "./ui";

/** Address / place search (forward geocoding) with keyboard navigation. */
export function AddressSearch({ onPick, className }: { onPick: (hit: GeocodeHit) => void; className?: string }) {
  const { t } = useTranslation();
  const { data: config } = useConfig();
  const [q, setQ] = useState("");
  const [debounced, setDebounced] = useState("");
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const listId = useId();
  const box = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const id = setTimeout(() => setDebounced(q.trim()), 400);
    return () => clearTimeout(id);
  }, [q]);
  useEffect(() => {
    const close = (e: MouseEvent) => !box.current?.contains(e.target as Node) && setOpen(false);
    document.addEventListener("mousedown", close);
    return () => document.removeEventListener("mousedown", close);
  }, []);

  const search = useQuery({
    queryKey: ["geocode", debounced],
    queryFn: () => api<GeocodeHit[]>(`/geocode/search?q=${encodeURIComponent(debounced)}`),
    enabled: debounced.length >= 3,
    staleTime: 5 * 60_000,
  });
  const hits = search.data ?? [];

  if (config && !config.geocoder) return null;

  const pick = (h: GeocodeHit) => {
    onPick(h);
    setQ(h.name);
    setOpen(false);
  };

  return (
    <div ref={box} className={cn("relative", className)}>
      <div className="relative">
        <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-subtle" aria-hidden />
        <input
          type="search"
          role="combobox"
          aria-expanded={open && hits.length > 0}
          aria-controls={listId}
          aria-activedescendant={open && hits[active] ? `${listId}-${active}` : undefined}
          aria-label={t("search.label")}
          placeholder={t("search.placeholder")}
          value={q}
          onChange={(e) => {
            setQ(e.target.value);
            setOpen(true);
            setActive(0);
          }}
          onFocus={() => setOpen(true)}
          onKeyDown={(e) => {
            if (e.key === "ArrowDown") { e.preventDefault(); setActive((a) => Math.min(a + 1, hits.length - 1)); }
            if (e.key === "ArrowUp") { e.preventDefault(); setActive((a) => Math.max(a - 1, 0)); }
            if (e.key === "Enter" && hits[active]) { e.preventDefault(); pick(hits[active]); }
            if (e.key === "Escape") setOpen(false);
          }}
          className="h-10 w-full rounded-xl border border-border-strong bg-surface pr-9 pl-9 text-sm shadow-pop placeholder:text-subtle focus:border-primary focus:outline-none [&::-webkit-search-cancel-button]:hidden"
        />
        {search.isFetching ? (
          <Loader2 className="absolute top-1/2 right-3 size-4 -translate-y-1/2 animate-spin text-subtle" aria-hidden />
        ) : q && (
          <button className="absolute top-1/2 right-2 grid size-6 -translate-y-1/2 place-items-center rounded text-subtle hover:text-fg" onClick={() => { setQ(""); setDebounced(""); }} aria-label={t("common.clear")}>
            <X className="size-4" />
          </button>
        )}
      </div>
      {open && debounced.length >= 3 && !search.isFetching && (
        <ul id={listId} role="listbox" className="absolute inset-x-0 top-full z-30 mt-1.5 overflow-hidden rounded-xl border border-border bg-surface py-1 shadow-pop">
          {search.isError ? (
            <li className="px-3 py-2.5 text-sm text-danger">{search.error.message}</li>
          ) : hits.length === 0 ? (
            <li className="px-3 py-2.5 text-sm text-muted">{t("search.none")}</li>
          ) : (
            hits.map((h, i) => (
              <li
                key={`${h.lat},${h.lon},${i}`}
                id={`${listId}-${i}`}
                role="option"
                aria-selected={i === active}
                onMouseEnter={() => setActive(i)}
                onMouseDown={(e) => { e.preventDefault(); pick(h); }}
                className={cn("flex cursor-pointer items-start gap-2.5 px-3 py-2", i === active && "bg-surface-2")}
              >
                <MapPin className="mt-0.5 size-4 shrink-0 text-subtle" aria-hidden />
                <span className="min-w-0">
                  <span className="block truncate text-sm font-medium">{h.name}</span>
                  <span className="block truncate text-xs text-muted">{h.address}</span>
                </span>
              </li>
            ))
          )}
        </ul>
      )}
    </div>
  );
}

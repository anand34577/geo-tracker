import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { EyeOff, MapPin, MousePointerClick, Pencil, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, queryClient, type Place } from "../lib/api";
import { ago, duration } from "../lib/format";
import { usePlaces } from "../lib/data";
import MapView from "../components/MapView";
import { PlaceDialog, PlaceIcon, type PlaceDraft } from "../components/places";
import { Button, ConfirmDialog, EmptyState, ErrorState, Skeleton, useToast } from "../components/ui";
import { AddressSearch } from "../components/AddressSearch";

export default function PlacesPage() {
  const { t } = useTranslation();
  const toast = useToast();
  const places = usePlaces();
  const [draft, setDraft] = useState<PlaceDraft | null>(null);
  const [deleting, setDeleting] = useState<Place | null>(null);
  const [focus, setFocus] = useState<{ lon: number; lat: number; key: number } | null>(null);

  const del = useMutation({
    mutationFn: (p: Place) => api(`/places/${p.id}`, { method: "DELETE" }),
    onSuccess: (_, p) => {
      queryClient.invalidateQueries({ queryKey: ["places"] });
      queryClient.invalidateQueries({ queryKey: ["timeline"] });
      toast("success", t("places.deleted", { name: p.name }));
      setDeleting(null);
    },
  });

  const list = [...(places.data ?? [])].sort((a, b) => b.total_ms - a.total_ms);

  return (
    <div className="absolute inset-0 flex flex-col md:flex-row">
      <div className="order-2 flex min-h-0 flex-1 flex-col bg-surface md:order-1 md:w-[400px] md:flex-none md:border-r md:border-border">
        <div className="border-b border-border p-4">
          <h1 className="text-lg font-semibold">{t("places.title")}</h1>
          <p className="mt-1 flex items-center gap-1.5 text-sm text-muted">
            <MousePointerClick className="size-4 shrink-0" aria-hidden />
            {t("places.addHint")}
          </p>
          <AddressSearch className="mt-3" onPick={(h) => { setFocus({ lon: h.lon, lat: h.lat, key: Date.now() }); setDraft({ lon: h.lon, lat: h.lat, name: h.name }); }} />
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto">
          {places.isError ? (
            <ErrorState error={places.error} retry={() => places.refetch()} />
          ) : places.isPending ? (
            <div className="space-y-3 p-4">{[0, 1, 2].map((i) => <Skeleton key={i} className="h-16" />)}</div>
          ) : list.length === 0 ? (
            <EmptyState icon={MapPin} title={t("places.emptyTitle")} body={t("places.emptyBody")} />
          ) : (
            <ul className="p-3">
              {list.map((p) => (
                <li key={p.id} className="group flex items-center gap-3 rounded-xl p-3 hover:bg-surface-2">
                  <button className="flex min-w-0 flex-1 items-center gap-3 text-left" onClick={() => setFocus({ lon: p.lon, lat: p.lat, key: Date.now() })}>
                    <span className="grid size-10 shrink-0 place-items-center rounded-full bg-primary text-primary-fg">
                      <PlaceIcon name={p.icon} className="size-5" />
                    </span>
                    <span className="min-w-0">
                      <span className="flex items-center gap-1.5 truncate font-medium">
                        {p.name}
                        {p.private && <span className="flex items-center gap-1 rounded-full bg-surface-3 px-2 py-0.5 text-[11px] font-medium text-muted"><EyeOff className="size-3" aria-hidden />{t("places.privateBadge")}</span>}
                      </span>
                      <span className="block truncate text-sm text-muted">
                        {p.visits
                          ? t("places.stats", { count: p.visits, time: duration(p.total_ms), last: p.last_visit ? ago(p.last_visit) : "" })
                          : t("places.noVisits")}
                      </span>
                    </span>
                  </button>
                  <div className="flex md:opacity-0 md:group-hover:opacity-100 md:focus-within:opacity-100">
                    <Button variant="ghost" size="icon" className="size-9" aria-label={t("places.edit")} onClick={() => setDraft(p)}>
                      <Pencil className="size-4" />
                    </Button>
                    <Button variant="ghost" size="icon" className="size-9" aria-label={t("common.delete")} onClick={() => setDeleting(p)}>
                      <Trash2 className="size-4" />
                    </Button>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
      <MapView
        className="order-1 h-[42dvh] shrink-0 md:order-2 md:h-auto md:flex-1"
        label={t("places.mapLabel")}
        places={places.data}
        fit={places.data?.length ? "places" : undefined}
        focus={focus}
        onMapClick={(lon, lat) => setDraft({ lon, lat })}
      />
      <PlaceDialog draft={draft} onClose={() => setDraft(null)} />
      <ConfirmDialog
        open={!!deleting}
        onClose={() => setDeleting(null)}
        onConfirm={() => deleting && del.mutate(deleting)}
        loading={del.isPending}
        title={t("places.deleteTitle", { name: deleting?.name })}
        body={t("places.deleteBody")}
        confirmLabel={t("common.delete")}
      />
    </div>
  );
}

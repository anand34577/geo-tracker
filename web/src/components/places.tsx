import { useEffect, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import {
  Briefcase, Coffee, Dumbbell, GraduationCap, Heart, Home, MapPin, ShoppingCart, Stethoscope, Trees, Users, Utensils, type LucideIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, queryClient, type Place } from "../lib/api";
import { Button, cn, Dialog, Field, Input, useToast } from "./ui";
import { Checkbox, Slider } from "./controls";

export const placeIcons: Record<string, LucideIcon> = {
  "map-pin": MapPin, home: Home, briefcase: Briefcase, school: GraduationCap, gym: Dumbbell, shop: ShoppingCart,
  coffee: Coffee, food: Utensils, family: Users, health: Stethoscope, park: Trees, heart: Heart,
};

export const PlaceIcon = ({ name, className }: { name?: string; className?: string }) => {
  const Icon = placeIcons[name ?? ""] ?? MapPin;
  return <Icon className={className} aria-hidden />;
};

export type PlaceDraft = Partial<Pick<Place, "id" | "name" | "icon" | "radius" | "private">> & { lat: number; lon: number };

export function PlaceDialog({ draft, onClose }: { draft: PlaceDraft | null; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const [form, setForm] = useState({ name: "", icon: "map-pin", radius: 75, private: false });
  useEffect(() => {
    if (!draft) return;
    setForm({ name: draft.name ?? "", icon: draft.icon ?? "map-pin", radius: draft.radius ?? 75, private: draft.private ?? false });
    if (draft.name || draft.id) return;
    // Suggest a name for a spot clicked on the map (reverse geocoding); never overwrite typing.
    let alive = true;
    api<{ name?: string }>(`/geocode/reverse?lat=${draft.lat}&lon=${draft.lon}`)
      .then((r) => alive && r.name && setForm((f) => (f.name ? f : { ...f, name: r.name! })))
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, [draft]);

  const save = useMutation({
    meta: { inline: true }, // error shown in the form
    mutationFn: () =>
      api<Place>(draft?.id ? `/places/${draft.id}` : "/places", {
        method: draft?.id ? "PUT" : "POST",
        body: { ...form, lat: draft!.lat, lon: draft!.lon },
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["places"] });
      queryClient.invalidateQueries({ queryKey: ["timeline"] });
      toast("success", t("places.saved", { name: form.name }));
      onClose();
    },
  });

  return (
    <Dialog
      open={!!draft}
      onClose={onClose}
      title={draft?.id ? t("places.edit") : t("places.add")}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button>
          <Button variant="primary" loading={save.isPending} disabled={!form.name.trim()} onClick={() => save.mutate()}>
            {t("common.save")}
          </Button>
        </>
      }
    >
      <form className="space-y-5" onSubmit={(e) => { e.preventDefault(); if (form.name.trim()) save.mutate(); }}>
        <Field label={t("places.name")} error={save.error?.message}>
          {(id) => <Input id={id} autoFocus value={form.name} maxLength={80} placeholder={t("places.namePlaceholder")} onChange={(e) => setForm({ ...form, name: e.target.value })} />}
        </Field>
        <fieldset>
          <legend className="mb-2 text-sm font-medium">{t("places.icon")}</legend>
          <div className="grid grid-cols-6 gap-2">
            {Object.entries(placeIcons).map(([key, Icon]) => (
              <button
                key={key}
                type="button"
                aria-label={key}
                aria-pressed={form.icon === key}
                onClick={() => setForm({ ...form, icon: key })}
                className={cn("grid h-11 place-items-center rounded-lg border", form.icon === key ? "border-primary bg-primary-subtle text-primary" : "border-border text-muted hover:bg-surface-2")}
              >
                <Icon className="size-5" />
              </button>
            ))}
          </div>
        </fieldset>
        <Field label={t("places.radius", { m: form.radius })} hint={t("places.radiusHint")}>
          {(id) => <Slider id={id} label={t("places.radiusLabel")} min={25} max={1000} step={25} value={form.radius} format={(v) => `${v} m`} onChange={(radius) => setForm({ ...form, radius })} />}
        </Field>
        <Checkbox checked={form.private} onChange={(v) => setForm({ ...form, private: v })} label={t("places.private")} description={t("places.privateHint")} />
        <button type="submit" hidden />
      </form>
    </Dialog>
  );
}

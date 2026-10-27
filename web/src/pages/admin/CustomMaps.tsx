import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { CheckCircle2, Grid3x3, Layers, Pencil, Plus, Server, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, type CustomMap } from "../../lib/api";
import { Button, ConfirmDialog, Dialog, Field, Input, Notice } from "../../components/ui";
import { Checkbox, Select, Stepper } from "../../components/controls";

export function parseCustomMaps(raw: string | undefined): CustomMap[] {
  try {
    const v = JSON.parse(raw || "[]");
    return Array.isArray(v) ? v : [];
  } catch {
    return [];
  }
}

const blank: CustomMap = { id: "", name: "", kind: "raster", url: "", attribution: "", min_zoom: 0, max_zoom: 19, tile_size: 256, proxy: false };

const presets: { label: string; map: Partial<CustomMap> }[] = [
  { label: "OpenStreetMap", map: { name: "OpenStreetMap (direct)", url: "https://tile.openstreetmap.org/{z}/{x}/{y}.png", attribution: "© OpenStreetMap contributors", max_zoom: 19 } },
  { label: "Esri World Imagery", map: { name: "Satellite (Esri)", url: "https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}", attribution: "Tiles © Esri, Maxar, Earthstar Geographics", max_zoom: 19 } },
  { label: "CyclOSM", map: { name: "CyclOSM", url: "https://{s}.tile-cyclosm.openstreetmap.fr/cyclosm/{z}/{x}/{y}.png", attribution: "© OpenStreetMap contributors, CyclOSM", max_zoom: 20 } },
];

/**
 * Admin-defined base maps (XYZ/TMS/WMS tile templates or MapLibre styles). Changes are saved
 * immediately so a new map can be picked as the default right away.
 */
export function CustomMaps({ value, onSaved }: { value: string; onSaved: (settings: Record<string, string>) => void }) {
  const { t } = useTranslation();
  const list = parseCustomMaps(value);
  const [edit, setEdit] = useState<CustomMap | null>(null);
  const [removing, setRemoving] = useState<CustomMap | null>(null);

  const persist = useMutation({
    meta: { inline: true },
    mutationFn: (next: CustomMap[]) => api<Record<string, string>>("/admin/settings", { method: "PUT", body: { map_custom: JSON.stringify(next) } }),
    onSuccess: (s) => {
      onSaved(s);
      setEdit(null);
      setRemoving(null);
    },
  });

  const save = (m: CustomMap) => persist.mutate(m.id ? list.map((x) => (x.id === m.id ? m : x)) : [...list, m]);

  return (
    <div className="rounded-xl border border-border p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="font-medium">{t("admin.cm.title")}</p>
          <p className="mt-1 text-sm text-muted">{t("admin.cm.desc")}</p>
        </div>
        <Button size="sm" icon={Plus} onClick={() => { persist.reset(); setEdit({ ...blank }); }} disabled={list.length >= 30}>{t("admin.cm.add")}</Button>
      </div>
      {persist.error && !edit && <div className="mt-3"><Notice tone="danger">{persist.error.message}</Notice></div>}
      {list.length > 0 && (
        <ul className="mt-4 divide-y divide-border border-t border-border">
          {list.map((m) => (
            <li key={m.id} className="flex items-center gap-3 py-3">
              <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-surface-2 text-muted">
                {m.kind === "style" ? <Layers className="size-4" aria-hidden /> : <Grid3x3 className="size-4" aria-hidden />}
              </span>
              <div className="min-w-0 flex-1">
                <p className="flex items-center gap-2 truncate text-sm font-medium">
                  {m.name}
                  {m.proxy && <span className="inline-flex items-center gap-1 rounded-full bg-surface-3 px-2 py-0.5 text-[11px] font-medium text-muted"><Server className="size-3" aria-hidden />{t("admin.cm.proxied")}</span>}
                </p>
                <p className="truncate font-mono text-xs text-subtle" title={m.url}>{m.url}</p>
              </div>
              <Button size="icon" variant="ghost" className="size-9" aria-label={t("common.edit")} onClick={() => { persist.reset(); setEdit({ ...m }); }}><Pencil className="size-4" /></Button>
              <Button size="icon" variant="ghost" className="size-9" aria-label={t("common.delete")} onClick={() => setRemoving(m)}><Trash2 className="size-4" /></Button>
            </li>
          ))}
        </ul>
      )}
      {edit && <MapDialog initial={edit} saving={persist.isPending} error={persist.error?.message} onClose={() => setEdit(null)} onSave={save} />}
      <ConfirmDialog open={!!removing} onClose={() => setRemoving(null)} loading={persist.isPending}
        onConfirm={() => removing && persist.mutate(list.filter((x) => x.id !== removing.id))}
        title={t("admin.cm.removeTitle", { name: removing?.name })} body={t("admin.cm.removeBody")} confirmLabel={t("common.delete")} />
    </div>
  );
}

type TestResult = { ok: boolean; preview?: string; content_type?: string; error?: string };

function MapDialog({ initial, saving, error, onClose, onSave }: { initial: CustomMap; saving: boolean; error?: string; onClose: () => void; onSave: (m: CustomMap) => void }) {
  const { t } = useTranslation();
  const [m, setM] = useState<CustomMap>(initial);
  const [result, setResult] = useState<TestResult | null>(null);
  const set = (patch: Partial<CustomMap>) => {
    setM((x) => ({ ...x, ...patch }));
    setResult(null);
  };
  const raster = m.kind === "raster";
  const insecure = raster && m.url.trim().startsWith("http://");
  const test = useMutation({
    meta: { inline: true },
    mutationFn: () => api<TestResult>("/admin/maps/test", { method: "POST", body: m }),
    onSuccess: setResult,
    onError: (e) => setResult({ ok: false, error: e.message }),
  });
  const valid = m.name.trim() !== "" && m.url.trim() !== "";
  const submit = () => valid && onSave({ ...m, proxy: m.proxy || insecure });

  return (
    <Dialog open onClose={onClose} wide title={initial.id ? t("admin.cm.edit") : t("admin.cm.add")}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button>
          <Button onClick={() => test.mutate()} loading={test.isPending} disabled={!valid}>{t("admin.cm.test")}</Button>
          <Button variant="primary" loading={saving} disabled={!valid} onClick={submit}>{t("common.save")}</Button>
        </>
      }>
      <form className="grid gap-4" onSubmit={(e) => { e.preventDefault(); submit(); }}>
        {error && <Notice tone="danger">{error}</Notice>}
        {!initial.id && (
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <span className="text-muted">{t("admin.cm.presets")}</span>
            {presets.map((p) => (
              <button key={p.label} type="button" className="rounded-full border border-border px-3 py-1 hover:bg-surface-2" onClick={() => set({ ...blank, ...p.map })}>{p.label}</button>
            ))}
          </div>
        )}
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={t("admin.cm.type")}>
            {(id) => (
              <Select id={id} label={t("admin.cm.type")} value={m.kind} onChange={(kind) => set({ kind })} options={[
                { value: "raster", label: t("admin.cm.typeRaster"), hint: t("admin.cm.typeRasterHint") },
                { value: "style", label: t("admin.cm.typeStyle"), hint: t("admin.cm.typeStyleHint") },
              ]} />
            )}
          </Field>
          <Field label={t("admin.cm.name")}>{(id) => <Input id={id} autoFocus value={m.name} maxLength={60} placeholder={t("admin.cm.namePlaceholder")} onChange={(e) => set({ name: e.target.value })} />}</Field>
        </div>
        <Field label={raster ? t("admin.cm.tileUrl") : t("admin.cm.styleUrl")} hint={raster ? t("admin.cm.tileUrlHint") : t("admin.cm.styleUrlHint")}>
          {(id, d) => <Input id={id} aria-describedby={d} value={m.url} spellCheck={false} className="font-mono text-xs"
            placeholder={raster ? "https://tiles.example.com/{z}/{x}/{y}.png" : "https://maps.example.com/style.json"} onChange={(e) => set({ url: e.target.value })} />}
        </Field>
        {!raster && (
          <Field label={t("admin.cm.styleDark")} hint={t("admin.cm.styleDarkHint")}>
            {(id, d) => <Input id={id} aria-describedby={d} value={m.url_dark ?? ""} spellCheck={false} className="font-mono text-xs" onChange={(e) => set({ url_dark: e.target.value })} />}
          </Field>
        )}
        {raster && (
          <>
            <Field label={t("admin.cm.attribution")} hint={t("admin.cm.attributionHint")}>
              {(id, d) => <Input id={id} aria-describedby={d} value={m.attribution ?? ""} maxLength={500} placeholder="© OpenStreetMap contributors" onChange={(e) => set({ attribution: e.target.value })} />}
            </Field>
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
              <Field label={t("admin.cm.minZoom")}>{(id) => <Stepper id={id} label={t("admin.cm.minZoom")} min={0} max={24} value={m.min_zoom} onChange={(v) => set({ min_zoom: v, max_zoom: Math.max(v, m.max_zoom) })} />}</Field>
              <Field label={t("admin.cm.maxZoom")}>{(id) => <Stepper id={id} label={t("admin.cm.maxZoom")} min={0} max={24} value={m.max_zoom} onChange={(v) => set({ max_zoom: v, min_zoom: Math.min(v, m.min_zoom) })} />}</Field>
              <Field label={t("admin.cm.tileSize")}>
                {(id) => <Select id={id} label={t("admin.cm.tileSize")} value={String(m.tile_size || 256)} onChange={(v) => set({ tile_size: Number(v) })} options={[{ value: "256", label: "256 px" }, { value: "512", label: t("admin.cm.retina") }]} />}
              </Field>
            </div>
            <Checkbox checked={m.proxy || insecure} onChange={(proxy) => !insecure && set({ proxy })} label={t("admin.cm.proxy")} description={insecure ? t("admin.cm.proxyForced") : t("admin.cm.proxyHint")} />
          </>
        )}
        {result && (
          result.ok ? (
            <div className="flex items-center gap-3 rounded-xl border border-success/40 bg-success/5 p-3 text-sm">
              {result.preview ? <img src={result.preview} alt="" className="size-16 shrink-0 rounded-md border border-border object-cover" /> : <CheckCircle2 className="size-5 text-success" aria-hidden />}
              <p>{raster ? t("admin.cm.testOk", { type: result.content_type }) : t("admin.cm.testStyleOk")}</p>
            </div>
          ) : (
            <Notice tone="danger">{result.error}</Notice>
          )
        )}
        {raster && <p className="text-xs text-subtle">{t("admin.cm.terms")}</p>}
        <button type="submit" hidden />
      </form>
    </Dialog>
  );
}

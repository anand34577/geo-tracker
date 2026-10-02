import { useRef, useState, type DragEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { CheckCircle2, CircleAlert, Download, FileUp, Loader2, PackagePlus, RefreshCw, Trash2, Undo2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, queryClient, upload, type DataExport, type Import, type PointStats } from "../../lib/api";
import { bytes, dateTime, number } from "../../lib/format";
import { usePrefs } from "../../lib/prefs";
import { Button, ConfirmDialog, Field, Section, cn, useToast } from "../../components/ui";
import { RangePicker, Select, presetRange, rangeLabel, type Range } from "../../components/controls";

const accepted = ".zip,.json,.geojson,.gpx,.csv,.rec";

export default function DataTab() {
  return (
    <>
      <ImportSection />
      <ExportSection />
      <RetentionSection />
      <MaintenanceSection />
    </>
  );
}

function ImportSection() {
  const { t } = useTranslation();
  const toast = useToast();
  const { clock } = usePrefs();
  const input = useRef<HTMLInputElement>(null);
  const [progress, setProgress] = useState<{ name: string; fraction: number } | null>(null);
  const [dragging, setDragging] = useState(false);
  const [undoing, setUndoing] = useState<Import | null>(null);
  const imports = useQuery({
    queryKey: ["imports"],
    queryFn: () => api<Import[]>("/imports"),
    refetchInterval: (q) => (q.state.data?.some((i) => i.status === "queued" || i.status === "running") ? 2000 : false),
  });

  const send = async (files: FileList | null) => {
    for (const file of Array.from(files ?? [])) {
      setProgress({ name: file.name, fraction: 0 });
      try {
        await upload("/imports", file, (fraction) => setProgress({ name: file.name, fraction }));
        queryClient.invalidateQueries({ queryKey: ["imports"] });
      } catch (e) {
        toast("error", `${file.name}: ${(e as Error).message}`);
      }
    }
    setProgress(null);
    if (input.current) input.current.value = "";
  };

  const undo = useMutation({
    mutationFn: (i: Import) => api(`/imports/${i.id}`, { method: "DELETE" }),
    onSuccess: () => {
      ["imports", "stats", "points", "timeline"].forEach((k) => queryClient.invalidateQueries({ queryKey: [k] }));
      toast("success", t("data.undone"));
      setUndoing(null);
    },
  });

  const onDrop = (e: DragEvent) => {
    e.preventDefault();
    setDragging(false);
    send(e.dataTransfer.files);
  };

  return (
    <Section title={t("data.importTitle")} description={t("data.importDesc")}>
      <label
        onDragOver={(e) => { e.preventDefault(); setDragging(true); }}
        onDragLeave={() => setDragging(false)}
        onDrop={onDrop}
        className={cn(
          "flex cursor-pointer flex-col items-center justify-center rounded-xl border-2 border-dashed px-6 py-10 text-center transition-colors",
          dragging ? "border-primary bg-primary-subtle" : "border-border-strong hover:bg-surface-2",
        )}
      >
        <input ref={input} type="file" accept={accepted} multiple className="sr-only" onChange={(e) => send(e.target.files)} disabled={!!progress} />
        {progress ? (
          <div className="w-full max-w-xs" role="status">
            <p className="truncate text-sm font-medium">{t("data.uploading", { name: progress.name })}</p>
            <div className="mt-3 h-2 overflow-hidden rounded-full bg-surface-3">
              <div className="h-full bg-primary transition-[width]" style={{ width: `${Math.round(progress.fraction * 100)}%` }} />
            </div>
            <p className="mt-2 text-xs text-muted tabular-nums">{Math.round(progress.fraction * 100)}%</p>
          </div>
        ) : (
          <>
            <FileUp className="size-8 text-primary" aria-hidden />
            <p className="mt-3 font-medium">{t("data.dropTitle")}</p>
            <p className="mt-1 text-sm text-muted">{t("data.dropFormats")}</p>
          </>
        )}
      </label>
      <details className="mt-3 text-sm text-muted">
        <summary className="cursor-pointer font-medium text-fg">{t("data.googleHowTo")}</summary>
        <ul className="mt-2 list-disc space-y-1 pl-5">
          {(t("data.googleSteps", { returnObjects: true }) as string[]).map((s) => <li key={s}>{s}</li>)}
        </ul>
      </details>

      {!!imports.data?.length && (
        <ul className="mt-5 divide-y divide-border border-t border-border">
          {imports.data.map((i) => (
            <li key={i.id} className="flex flex-wrap items-center gap-3 py-3">
              {i.status === "done" ? <CheckCircle2 className="size-5 text-success" aria-hidden /> : i.status === "failed" ? <CircleAlert className="size-5 text-danger" aria-hidden /> : <Loader2 className="size-5 animate-spin text-primary" aria-hidden />}
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium">{i.filename}</p>
                <p className="text-xs text-muted">
                  {i.status === "failed"
                    ? i.error
                    : t(`data.status.${i.status}`, { added: number(i.added), dupes: number(i.duplicates), rejected: number(i.rejected), format: i.format })}
                  {" · "}
                  {dateTime(i.created_at, clock)}
                </p>
              </div>
              {i.status !== "running" && (i.added > 0 || i.status === "queued") && (
                <Button size="sm" variant="ghost" icon={Undo2} onClick={() => setUndoing(i)}>{t("data.undo")}</Button>
              )}
            </li>
          ))}
        </ul>
      )}
      <ConfirmDialog open={!!undoing} onClose={() => setUndoing(null)} onConfirm={() => undoing && undo.mutate(undoing)} loading={undo.isPending}
        title={t("data.undoTitle")} body={t("data.undoBody", { count: undoing?.added ?? 0, name: undoing?.filename })} confirmLabel={t("data.undo")} />
    </Section>
  );
}

const formatLabels = { native: "GeoTracker", gpx: "GPX", geojson: "GeoJSON", csv: "CSV" } as const;

/** Exports are built in the background (they can be big); download them when they are ready. */
function ExportSection() {
  const { t } = useTranslation();
  const toast = useToast();
  const { clock } = usePrefs();
  const [format, setFormat] = useState<"native" | "gpx" | "geojson" | "csv">("native");
  const [range, setRange] = useState<Range>(() => presetRange("all"));
  const [removing, setRemoving] = useState<DataExport | null>(null);
  const exports = useQuery({
    queryKey: ["exports"],
    queryFn: () => api<DataExport[]>("/exports"),
    refetchInterval: (q) => (q.state.data?.some((e) => e.status === "queued" || e.status === "running") ? 2000 : false),
  });
  const create = useMutation({
    mutationFn: () => api("/exports", { method: "POST", body: { format, ...(range.preset !== "all" ? { from: range.from, to: range.to } : {}) } }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["exports"] });
      toast("success", t("data.exportQueued"));
    },
  });
  const remove = useMutation({
    mutationFn: (e: DataExport) => api(`/exports/${e.id}`, { method: "DELETE" }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["exports"] });
      setRemoving(null);
    },
  });
  return (
    <Section title={t("data.exportTitle")} description={t("data.exportDesc")}>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t("data.format")}>
          {(id) => (
            <Select id={id} label={t("data.format")} value={format} onChange={setFormat} options={[
              { value: "native", label: t("data.formatNative"), hint: t("data.formatNativeHint") },
              { value: "gpx", label: "GPX", hint: t("data.formatGpxHint") },
              { value: "geojson", label: "GeoJSON", hint: t("data.formatGeojsonHint") },
              { value: "csv", label: "CSV", hint: t("data.formatCsvHint") },
            ]} />
          )}
        </Field>
        <Field label={t("data.period")}>{() => <RangePicker label={t("data.period")} value={range} onChange={setRange} />}</Field>
      </div>
      <div className="mt-5 flex flex-wrap items-center gap-3">
        <Button variant="primary" icon={PackagePlus} loading={create.isPending} onClick={() => create.mutate()}>{t("data.createExport")}</Button>
        <p className="text-sm text-muted">{t("data.exportBackground")}</p>
      </div>
      {!!exports.data?.length && (
        <ul className="mt-5 divide-y divide-border border-t border-border">
          {exports.data.map((e) => (
            <li key={e.id} className="flex flex-wrap items-center gap-3 py-3">
              {e.status === "done" ? <CheckCircle2 className="size-5 text-success" aria-hidden /> : e.status === "failed" ? <CircleAlert className="size-5 text-danger" aria-hidden /> : <Loader2 className="size-5 animate-spin text-primary" aria-hidden />}
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium">
                  {formatLabels[e.format]} · {e.from != null ? rangeLabel({ from: e.from, to: e.to! }) : t("data.allTime")}
                </p>
                <p className="text-xs text-muted">
                  {e.status === "failed" ? e.error : t(`data.exportStatus.${e.status}`, { size: bytes(e.size) })}
                  {" · "}
                  {dateTime(e.created_at, clock)}
                </p>
              </div>
              {e.status === "done" && (
                <a href={`/api/v1/exports/${e.id}/download`} download>
                  <Button size="sm" variant="primary" icon={Download} tabIndex={-1}>{t("data.download")}</Button>
                </a>
              )}
              {e.status !== "running" && (
                <Button size="icon" variant="ghost" className="size-8" aria-label={t("common.delete")} onClick={() => setRemoving(e)}><Trash2 className="size-4" /></Button>
              )}
            </li>
          ))}
        </ul>
      )}
      <ConfirmDialog open={!!removing} onClose={() => setRemoving(null)} onConfirm={() => removing && remove.mutate(removing)} loading={remove.isPending}
        title={t("data.exportRemoveTitle")} body={t("data.exportRemoveBody")} confirmLabel={t("common.delete")} />
    </Section>
  );
}

function MaintenanceSection() {
  const { t } = useTranslation();
  const toast = useToast();
  const stats = useQuery({ queryKey: ["stats"], queryFn: () => api<PointStats>("/stats") });
  const [range, setRange] = useState<Range>(() => presetRange("today"));
  const [confirm, setConfirm] = useState(false);
  const rebuild = useMutation({
    mutationFn: () => api("/timeline/rebuild", { method: "POST" }),
    onSuccess: () => toast("success", t("data.rebuildStarted")),
  });
  const del = useMutation({
    mutationFn: () => api<{ deleted: number }>(`/points?from=${range.from}&to=${range.to}`, { method: "DELETE" }),
    onSuccess: (r) => {
      ["stats", "points", "timeline", "insights", "days"].forEach((k) => queryClient.invalidateQueries({ queryKey: [k] }));
      toast("success", t("data.deleted", { count: r.deleted }));
      setConfirm(false);
    },
  });
  return (
    <Section title={t("data.maintenanceTitle")} description={stats.data ? t("data.pointCount", { count: stats.data.count, formatted: number(stats.data.count) }) : undefined}>
      <div className="space-y-6">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <p className="text-sm font-medium">{t("data.rebuildTitle")}</p>
            <p className="text-sm text-muted">{t("data.rebuildDesc")}</p>
          </div>
          <Button icon={RefreshCw} loading={rebuild.isPending} onClick={() => rebuild.mutate()}>{t("data.rebuild")}</Button>
        </div>
        <div className="rounded-xl border border-danger/40 p-4">
          <p className="text-sm font-medium text-danger">{t("data.deleteTitle")}</p>
          <p className="mt-1 text-sm text-muted">{t("data.deleteDesc")}</p>
          <div className="mt-4 flex flex-wrap items-center gap-3">
            <div className="w-full sm:w-80"><RangePicker label={t("data.period")} value={range} onChange={setRange} allowAll={false} /></div>
            <Button variant="danger" icon={Trash2} onClick={() => setConfirm(true)}>{t("data.deleteRange")}</Button>
          </div>
        </div>
      </div>
      <ConfirmDialog open={confirm} onClose={() => setConfirm(false)} onConfirm={() => del.mutate()} loading={del.isPending} typeToConfirm="delete"
        title={t("data.deleteConfirmTitle")} body={t("data.deleteConfirmBody", { period: rangeLabel(range) })} confirmLabel={t("data.deleteRange")} />
    </Section>
  );
}

const retention = ["0", "30", "90", "180", "365", "730", "1825"] as const;

function RetentionSection() {
  const { t } = useTranslation();
  const toast = useToast();
  const q = useQuery({ queryKey: ["retention"], queryFn: () => api<{ days: number }>("/me/retention") });
  const [pending, setPending] = useState<number | null>(null);
  // Shortening the period deletes points for good, so it is confirmed; keeping more is not.
  const choose = (days: number) => (days > 0 && (q.data?.days === 0 || days < q.data!.days) ? setPending(days) : save.mutate(days));
  const save = useMutation({
    mutationFn: (days: number) => api<{ days: number }>("/me/retention", { method: "PUT", body: { days } }),
    onSuccess: (r) => {
      queryClient.setQueryData(["retention"], r);
      toast("success", t("settings.saved"));
      setPending(null);
    },
    onError: (e) => toast("error", e.message),
  });
  return (
    <Section title={t("data.retentionTitle")} description={t("data.retentionDesc")}>
      <div className="max-w-sm">
        <Field label={t("data.keepRaw")} hint={t("data.retentionHint")}>
          {(id) => (
            <Select id={id} label={t("data.keepRaw")} value={String(q.data?.days ?? 0) as (typeof retention)[number]} disabled={!q.data}
              onChange={(v) => choose(Number(v))} options={retention.map((d) => ({ value: d, label: t(`data.keep.${d}`) }))} />
          )}
        </Field>
      </div>
      <ConfirmDialog open={pending !== null} onClose={() => setPending(null)} onConfirm={() => pending && save.mutate(pending)} loading={save.isPending} typeToConfirm="delete"
        title={t("data.retentionConfirmTitle")} body={t("data.retentionConfirmBody", { period: t(`data.keep.${pending ?? 0}`) })} confirmLabel={t("data.retentionConfirm")} />
    </Section>
  );
}
import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router";
import { BarChart3, CalendarDays, CornerDownLeft, Database, Map as MapIcon, MapPin, Moon, Palette, Search, Settings, Share2, ShieldCheck, Smartphone, Sun, Upload, Users, Zap, type LucideIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { isValid, parse, subDays } from "date-fns";
import { useUser } from "../lib/api";
import { dayKey } from "../lib/format";
import { useFamily, usePlaces } from "../lib/data";
import { useIsDark, useSavePrefs } from "../lib/prefs";
import { PlaceIcon } from "./places";
import { Avatar, cn } from "./ui";

type Item = { id: string; label: string; hint?: string; icon: React.ReactNode; run: () => void; keywords?: string };

const ico = (I: LucideIcon) => <I className="size-4" aria-hidden />;

/** Parses "today", "yesterday", "2024-07-15", "15.07.2024", "15/07/2024", "July 15 2024". */
function parseDate(q: string): Date | null {
  const s = q.trim().toLowerCase();
  if (s === "today") return new Date();
  if (s === "yesterday") return subDays(new Date(), 1);
  for (const f of ["yyyy-MM-dd", "dd.MM.yyyy", "dd/MM/yyyy", "d MMM yyyy", "MMMM d yyyy", "MMM d yyyy", "d MMMM yyyy"]) {
    const d = parse(q.trim(), f, new Date());
    if (isValid(d) && d.getFullYear() > 1990 && d <= new Date()) return d;
  }
  return null;
}

/** Ctrl/Cmd+K: jump to any page, date, place or person, or toggle the theme. */
export function CommandPalette({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const user = useUser();
  const places = usePlaces();
  const family = useFamily();
  const dark = useIsDark();
  const savePrefs = useSavePrefs();
  const [q, setQ] = useState("");
  const [active, setActive] = useState(0);
  const dialog = useRef<HTMLDialogElement>(null);
  const list = useRef<HTMLUListElement>(null);

  useEffect(() => {
    const d = dialog.current;
    if (!d) return;
    if (open && !d.open) {
      setQ("");
      setActive(0);
      d.showModal();
    }
    if (!open && d.open) d.close();
  }, [open]);

  const go = (path: string) => () => navigate(path);
  const items = useMemo<Item[]>(() => {
    const out: Item[] = [];
    const date = parseDate(q);
    if (date) out.push({ id: "date", label: t("cmd.openDay", { date: date.toLocaleDateString(undefined, { dateStyle: "full" }) }), icon: ico(CalendarDays), run: go(`/timeline/${dayKey(date)}`) });
    const pages: [string, string, LucideIcon, string?][] = [
      ["/", t("nav.map"), MapIcon],
      ["/timeline", t("nav.timeline"), CalendarDays, "day history"],
      ["/insights", t("nav.insights"), BarChart3, "stats statistics charts"],
      ["/places", t("nav.places"), MapPin],
      ["/family", t("nav.family"), Users, "group people share"],
      ["/automations", t("nav.automations"), Zap, "geofence webhook arrive leave notify home assistant ntfy telegram"],
      ["/sharing", t("nav.sharing"), Share2, "link"],
      ["/settings/devices", t("settings.devices"), Smartphone, "phone app owntracks colota token"],
      ["/settings/data", t("settings.data"), Upload, "import export google takeout gpx"],
      ["/settings/notifications", t("settings.notifications"), Database, "email gotify alert"],
      ["/settings/appearance", t("settings.appearance"), Palette, "theme color units"],
      ["/settings", t("nav.settings"), Settings, "profile password sessions"],
      ...(user.role === "admin" ? ([["/admin", t("nav.admin"), ShieldCheck, "users backup restore audit sso smtp maps"]] as [string, string, LucideIcon, string][]) : []),
    ];
    for (const [path, label, I, kw] of pages) out.push({ id: "p" + path, label, hint: t("cmd.page"), icon: ico(I), run: go(path), keywords: kw });
    out.push({ id: "theme", label: dark ? t("cmd.light") : t("cmd.dark"), hint: t("cmd.action"), icon: ico(dark ? Sun : Moon), run: () => savePrefs({ theme: dark ? "light" : "dark" }), keywords: "theme dark light mode" });
    for (const p of places.data ?? []) out.push({ id: "pl" + p.id, label: p.name, hint: t("cmd.place"), icon: <PlaceIcon name={p.icon} className="size-4" />, run: go("/places") });
    for (const p of family.data ?? []) {
      if (p.point) out.push({ id: "fm" + p.user_id, label: p.name, hint: t("cmd.onMap"), icon: <Avatar name={p.name} color={p.color} className="size-5 text-[9px]" />, run: go(`/?person=${p.user_id}`) });
      if (p.history_from != null) out.push({ id: "ft" + p.user_id, label: t("timeline.whoseTitle", { name: p.name }), hint: t("nav.timeline"), icon: ico(CalendarDays), run: go(`/timeline?user=${p.user_id}`) });
    }
    const needle = q.trim().toLowerCase();
    return needle && !date ? out.filter((i) => (i.label + " " + (i.keywords ?? "") + " " + (i.hint ?? "")).toLowerCase().includes(needle)) : out;
  }, [q, places.data, family.data, dark, user.role]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => setActive(0), [q]);
  useEffect(() => list.current?.querySelector(`[data-i="${active}"]`)?.scrollIntoView({ block: "nearest" }), [active]);

  const run = (i: Item) => {
    onClose();
    i.run();
  };

  return (
    <dialog
      ref={dialog}
      onClose={onClose}
      onClick={(e) => e.target === dialog.current && onClose()}
      aria-label={t("cmd.title")}
      className="mx-auto mt-[12vh] w-[calc(100%-2rem)] max-w-xl overflow-hidden rounded-2xl border border-border bg-surface p-0 text-fg shadow-pop"
    >
      {open && (
        <div onKeyDown={(e) => {
          if (e.key === "ArrowDown") { e.preventDefault(); setActive((a) => Math.min(a + 1, items.length - 1)); }
          if (e.key === "ArrowUp") { e.preventDefault(); setActive((a) => Math.max(a - 1, 0)); }
          if (e.key === "Enter" && items[active]) { e.preventDefault(); run(items[active]); }
        }}>
          <div className="flex items-center gap-3 border-b border-border px-4">
            <Search className="size-5 text-subtle" aria-hidden />
            <input
              autoFocus
              role="combobox"
              aria-expanded
              aria-controls="cmd-list"
              aria-activedescendant={items[active] ? `cmd-${items[active].id}` : undefined}
              placeholder={t("cmd.placeholder")}
              value={q}
              onChange={(e) => setQ(e.target.value)}
              className="h-14 flex-1 bg-transparent text-[15px] placeholder:text-subtle focus:outline-none"
            />
            <kbd className="rounded border border-border px-1.5 py-0.5 text-[11px] text-subtle">Esc</kbd>
          </div>
          <ul id="cmd-list" ref={list} role="listbox" className="max-h-[50vh] overflow-y-auto p-2">
            {items.length === 0 && <li className="px-3 py-6 text-center text-sm text-muted">{t("cmd.none")}</li>}
            {items.map((i, n) => (
              <li
                key={i.id}
                id={`cmd-${i.id}`}
                data-i={n}
                role="option"
                aria-selected={n === active}
                onPointerMove={() => setActive(n)}
                onClick={() => run(i)}
                className={cn("flex cursor-pointer items-center gap-3 rounded-lg px-3 py-2.5 text-sm", n === active && "bg-surface-2")}
              >
                <span className="grid size-7 shrink-0 place-items-center rounded-md bg-surface-2 text-muted">{i.icon}</span>
                <span className="min-w-0 flex-1 truncate">{i.label}</span>
                {i.hint && <span className="text-xs text-subtle">{i.hint}</span>}
                {n === active && <CornerDownLeft className="size-3.5 text-subtle" aria-hidden />}
              </li>
            ))}
          </ul>
          <p className="border-t border-border px-4 py-2 text-xs text-subtle">{t("cmd.tip")}</p>
        </div>
      )}
    </dialog>
  );
}

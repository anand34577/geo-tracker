import { useRef, useState } from "react";
import { Link, useNavigate } from "react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { BatteryMedium, Bell, CalendarDays, Crown, Eye, EyeOff, LogOut, MapPin, MoreHorizontal, Pause, Pencil, Play, Plus, Trash2, UserPlus, Users } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { api, queryClient, useUser, type AlertRule, type FamilyPerson, type Group, type Member } from "../lib/api";
import { ago, dateTime } from "../lib/format";
import { useFamily, useGroups, usePlaces } from "../lib/data";

import { Checkbox, Popover, RadioCards, Select, Switch } from "../components/controls";
import { Avatar, Button, ConfirmDialog, Dialog, EmptyState, ErrorState, Field, Input, Notice, PageHeader, Skeleton, cn, useToast } from "../components/ui";

const historyOptions = ["0", "1", "7", "30", "365", "-1"] as const;

const refresh = () => ["groups", "family", "alerts"].forEach((k) => queryClient.invalidateQueries({ queryKey: [k] }));

export default function FamilyPage() {
  const { t } = useTranslation();
  const me = useUser();
  const groups = useGroups();
  const family = useFamily();
  const [creating, setCreating] = useState(false);
  const [sharingFor, setSharingFor] = useState<{ group: Group; mine: Member } | null>(null);
  const [alertFor, setAlertFor] = useState<FamilyPerson | null>(null);

  const decline = useMutation({ mutationFn: (g: Group) => api(`/groups/${g.id}/members/${me.id}`, { method: "DELETE" }), onSuccess: refresh });
  const invites = (groups.data ?? []).filter((g) => g.members.find((m) => m.user_id === me.id)?.status === "invited");
  const active = (groups.data ?? []).filter((g) => g.members.find((m) => m.user_id === me.id)?.status === "active");

  return (
    <div className="mx-auto w-full max-w-5xl px-4 py-6 md:px-8 md:py-10">
      <PageHeader title={t("family.title")} description={t("family.subtitle")} actions={<Button variant="primary" icon={Plus} onClick={() => setCreating(true)}>{t("family.newGroup")}</Button>} />

      {groups.isError ? (
        <ErrorState error={groups.error} retry={() => groups.refetch()} />
      ) : groups.isPending ? (
        <div className="grid gap-4 sm:grid-cols-2">{[0, 1, 2, 3].map((i) => <Skeleton key={i} className="h-28" />)}</div>
      ) : (
        <div className="space-y-8">
          {invites.map((g) => {
            const inviter = g.members.find((m) => m.role === "owner");
            return (
              <div key={g.id} className="flex flex-wrap items-center gap-4 rounded-2xl border border-primary/40 bg-primary-subtle p-4">
                <span className="grid size-10 place-items-center rounded-xl bg-primary text-primary-fg"><UserPlus className="size-5" aria-hidden /></span>
                <div className="min-w-0 flex-1">
                  <p className="font-medium">{t("family.invitedTo", { group: g.name })}</p>
                  <p className="text-sm text-muted">{t("family.invitedBy", { name: inviter?.name ?? "" })}</p>
                </div>
                <Button variant="ghost" loading={decline.isPending && decline.variables?.id === g.id} onClick={() => decline.mutate(g)}>{t("family.decline")}</Button>
                <Button variant="primary" onClick={() => setSharingFor({ group: g, mine: g.members.find((m) => m.user_id === me.id)! })}>{t("family.accept")}</Button>
              </div>
            );
          })}

          {active.length === 0 && invites.length === 0 ? (
            <div className="rounded-2xl border border-dashed border-border-strong">
              <EmptyState icon={Users} title={t("family.emptyTitle")} body={t("family.emptyBody")}
                action={<Button variant="primary" icon={Plus} onClick={() => setCreating(true)}>{t("family.newGroup")}</Button>} />
            </div>
          ) : (
            <>
              <People people={family.data} loading={family.isPending} onAlert={setAlertFor} />
              {active.map((g) => (
                <GroupCard key={g.id} group={g} onEditSharing={(mine) => setSharingFor({ group: g, mine })} />
              ))}
              <WhoSeesMe groups={active} />
              <Alerts />
            </>
          )}
        </div>
      )}

      <CreateGroupDialog open={creating} onClose={() => setCreating(false)} />
      <SharingDialog target={sharingFor} onClose={() => setSharingFor(null)} />
      <AlertDialog person={alertFor} onClose={() => setAlertFor(null)} />
    </div>
  );
}


function People({ people, loading, onAlert }: { people?: FamilyPerson[]; loading: boolean; onAlert: (p: FamilyPerson) => void }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  if (loading) return <Skeleton className="h-32" />;
  if (!people?.length) return <Notice>{t("family.nobodyYet")}</Notice>;
  return (
    <section aria-label={t("family.people")}>
      <h2 className="mb-3 text-sm font-semibold tracking-wide text-subtle uppercase">{t("family.people")}</h2>
      <ul className="grid gap-3 sm:grid-cols-2">
        {people.map((p) => {
          const fresh = p.point && Date.now() - p.point.ts < 15 * 60_000;
          return (
            <li key={p.user_id} className="rounded-2xl border border-border bg-surface p-4">
              <div className="flex items-start gap-3">
                <span className="relative">
                  <Avatar name={p.name} color={p.color} className="size-11" />
                  {p.live && !p.paused && <span className={cn("absolute -right-0.5 -bottom-0.5 size-3.5 rounded-full border-2 border-surface", fresh ? "bg-success" : "bg-subtle")} aria-hidden />}
                </span>
                <div className="min-w-0 flex-1">
                  <p className="truncate font-semibold">{p.name}</p>
                  <p className="truncate text-sm text-muted">
                    {p.paused ? t("family.paused") : !p.live ? t("family.notSharingLive") : p.point ? `${p.place || t("family.somewhere")} · ${ago(p.point.ts)}` : t("family.noLocation")}
                  </p>
                  <p className="mt-1 flex flex-wrap gap-x-3 text-xs text-subtle">
                    {p.point?.batt != null && <span className="flex items-center gap-1"><BatteryMedium className="size-3.5" aria-hidden />{p.point.batt}%</span>}
                    {p.approx && <span>{t("family.approxOnly")}</span>}
                    {p.history_from == null && <span>{t("family.noHistory")}</span>}
                  </p>
                </div>
              </div>
              <div className="mt-3 flex flex-wrap gap-2 border-t border-border pt-3">
                <Button size="sm" icon={MapPin} disabled={!p.point} onClick={() => navigate(`/?person=${p.user_id}`)}>{t("family.showOnMap")}</Button>
                <Button size="sm" icon={CalendarDays} disabled={p.history_from == null} onClick={() => navigate(`/timeline?user=${p.user_id}`)}>{t("nav.timeline")}</Button>
                <Button size="sm" variant="ghost" icon={Bell} disabled={!p.live || p.approx} onClick={() => onAlert(p)} title={p.approx ? t("family.alertNeedsExact") : undefined}>{t("family.alert")}</Button>
              </div>
            </li>
          );
        })}
      </ul>
    </section>
  );
}

function sharingSummary(m: Member, t: TFunction) {
  if (m.paused_until && m.paused_until > Date.now()) return t("family.pausedUntil", { when: dateTime(m.paused_until, "24h") });
  const parts = [m.share_live ? t("family.sumLive") : t("family.sumNoLive"), t(`family.hist.${m.share_history_days}`)];
  if (m.precision === "approx") parts.push(t("family.sumApprox"));
  return parts.join(" · ");
}

function GroupCard({ group, onEditSharing }: { group: Group; onEditSharing: (m: Member) => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const me = useUser();
  const mine = group.members.find((m) => m.user_id === me.id)!;
  const owner = mine.role === "owner";
  const [inviting, setInviting] = useState(false);
  const [email, setEmail] = useState("");
  const [renaming, setRenaming] = useState<string | null>(null);
  const [confirm, setConfirm] = useState<"leave" | "delete" | { remove: Member } | null>(null);
  const menuBtn = useRef<HTMLButtonElement>(null);
  const [menu, setMenu] = useState(false);
  const paused = !!mine.paused_until && mine.paused_until > Date.now();

  const invite = useMutation({
    mutationFn: () => api(`/groups/${group.id}/members`, { method: "POST", body: { email } }),
    onSuccess: () => {
      refresh();
      toast("success", t("family.invited", { email }));
      setInviting(false);
      setEmail("");
    },
  });
  const pause = useMutation({
    mutationFn: (hours: number | null) =>
      api(`/groups/${group.id}/me`, { method: "PUT", body: { ...mine, paused_until: hours ? Date.now() + hours * 3_600_000 : null } }),
    onSuccess: refresh,
  });
  const rename = useMutation({
    mutationFn: (name: string) => api(`/groups/${group.id}`, { method: "PATCH", body: { name } }),
    onSuccess: () => { refresh(); setRenaming(null); },
  });
  const act = useMutation({
    mutationFn: async () => {
      if (confirm === "delete") return api(`/groups/${group.id}`, { method: "DELETE" });
      const uid = confirm === "leave" ? me.id : (confirm as { remove: Member }).remove.user_id;
      return api(`/groups/${group.id}/members/${uid}`, { method: "DELETE" });
    },
    onSuccess: () => { refresh(); setConfirm(null); },
  });

  return (
    <section className="rounded-2xl border border-border bg-surface">
      <header className="flex flex-wrap items-center gap-3 border-b border-border px-5 py-4">
        <div className="min-w-0 flex-1">
          <h2 className="truncate font-semibold">{group.name}</h2>
          <p className="text-sm text-muted">{t("family.memberCount", { count: group.members.length })}</p>
        </div>
        {owner && <Button size="sm" icon={UserPlus} onClick={() => setInviting(true)}>{t("family.invite")}</Button>}
        <Button size="icon" variant="ghost" ref={menuBtn} aria-label={t("family.groupMenu")} aria-expanded={menu} onClick={() => setMenu(!menu)}>
          <MoreHorizontal className="size-5" />
        </Button>
        <Popover anchor={menuBtn} open={menu} onClose={() => setMenu(false)} align="end" className="w-52 p-1.5">
          {owner && <MenuItem icon={Pencil} label={t("family.rename")} onClick={() => { setMenu(false); setRenaming(group.name); }} />}
          <MenuItem icon={LogOut} label={t("family.leave")} onClick={() => { setMenu(false); setConfirm("leave"); }} />
          {owner && <MenuItem icon={Trash2} danger label={t("family.deleteGroup")} onClick={() => { setMenu(false); setConfirm("delete"); }} />}
        </Popover>
      </header>

      <div className="flex flex-wrap items-center gap-3 border-b border-border bg-surface-2/50 px-5 py-3 text-sm">
        {paused ? <EyeOff className="size-4 text-warning" aria-hidden /> : <Eye className="size-4 text-primary" aria-hidden />}
        <span className="min-w-0 flex-1"><span className="font-medium">{t("family.youShare")}</span> {sharingSummary(mine, t)}</span>
        {paused ? (
          <Button size="sm" variant="ghost" icon={Play} loading={pause.isPending} onClick={() => pause.mutate(null)}>{t("family.resume")}</Button>
        ) : (
          <PauseMenu onPause={(h) => pause.mutate(h)} loading={pause.isPending} />
        )}
        <Button size="sm" onClick={() => onEditSharing(mine)}>{t("family.change")}</Button>
      </div>

      <ul className="divide-y divide-border">
        {group.members.map((m) => (
          <li key={m.user_id} className="flex items-center gap-3 px-5 py-3">
            <Avatar name={m.name} color={m.color} />
            <div className="min-w-0 flex-1">
              <p className="flex items-center gap-1.5 truncate text-sm font-medium">
                {m.name}
                {m.user_id === me.id && <span className="text-subtle">({t("admin.you")})</span>}
                {m.role === "owner" && <Crown className="size-3.5 text-warning" aria-label={t("family.owner")} />}
              </p>
              <p className="truncate text-xs text-muted">{m.status === "invited" ? t("family.invitePending") : sharingSummary(m, t)}</p>
            </div>
            {owner && m.user_id !== me.id && (
              <Button size="icon" variant="ghost" className="size-8" aria-label={t("family.remove", { name: m.name })} onClick={() => setConfirm({ remove: m })}>
                <Trash2 className="size-4" />
              </Button>
            )}
          </li>
        ))}
      </ul>

      <Dialog open={inviting} onClose={() => setInviting(false)} title={t("family.inviteTitle", { group: group.name })}
        footer={<><Button variant="ghost" onClick={() => setInviting(false)}>{t("common.cancel")}</Button><Button variant="primary" loading={invite.isPending} disabled={!email.includes("@")} onClick={() => invite.mutate()}>{t("family.sendInvite")}</Button></>}>
        <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); invite.mutate(); }}>
          {invite.error && <Notice tone="danger">{invite.error.message}</Notice>}
          <Field label={t("auth.email")} hint={t("family.inviteHint")}>{(id, d) => <Input id={id} aria-describedby={d} type="email" value={email} onChange={(e) => setEmail(e.target.value)} />}</Field>
        </form>
      </Dialog>
      <Dialog open={renaming !== null} onClose={() => setRenaming(null)} title={t("family.rename")}
        footer={<><Button variant="ghost" onClick={() => setRenaming(null)}>{t("common.cancel")}</Button><Button variant="primary" loading={rename.isPending} onClick={() => renaming && rename.mutate(renaming)}>{t("common.save")}</Button></>}>
        <Field label={t("family.groupName")}>{(id) => <Input id={id} value={renaming ?? ""} maxLength={60} onChange={(e) => setRenaming(e.target.value)} />}</Field>
      </Dialog>
      <ConfirmDialog open={confirm !== null} onClose={() => setConfirm(null)} onConfirm={() => act.mutate()} loading={act.isPending}
        title={confirm === "delete" ? t("family.deleteTitle", { group: group.name }) : confirm === "leave" ? t("family.leaveTitle", { group: group.name }) : t("family.removeTitle", { name: (confirm as { remove: Member } | null)?.remove?.name })}
        body={confirm === "delete" ? t("family.deleteBody") : confirm === "leave" ? t("family.leaveBody") : t("family.removeBody")}
        confirmLabel={confirm === "delete" ? t("family.deleteGroup") : confirm === "leave" ? t("family.leave") : t("family.removeShort")} />
    </section>
  );
}


function MenuItem({ icon: Icon, label, onClick, danger }: { icon: typeof Pencil; label: string; onClick: () => void; danger?: boolean }) {
  return (
    <button onClick={onClick} className={cn("flex h-9 w-full items-center gap-2.5 rounded-lg px-3 text-left text-sm hover:bg-surface-2", danger ? "text-danger" : "text-fg")}>
      <Icon className="size-4" aria-hidden />
      {label}
    </button>
  );
}

function PauseMenu({ onPause, loading }: { onPause: (hours: number) => void; loading: boolean }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const btn = useRef<HTMLButtonElement>(null);
  return (
    <>
      <Button size="sm" variant="ghost" icon={Pause} loading={loading} ref={btn} aria-expanded={open} onClick={() => setOpen(!open)}>{t("family.pause")}</Button>
      <Popover anchor={btn} open={open} onClose={() => setOpen(false)} align="end" className="w-52 p-1.5">
        {[1, 4, 8, 24].map((h) => <MenuItem key={h} icon={Pause} label={t("family.pauseFor", { count: h })} onClick={() => { setOpen(false); onPause(h); }} />)}
      </Popover>
    </>
  );
}

function CreateGroupDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useTranslation();
  const [name, setName] = useState("");
  const create = useMutation({
    mutationFn: () => api("/groups", { method: "POST", body: { name } }),
    onSuccess: () => { refresh(); setName(""); onClose(); },
  });
  return (
    <Dialog open={open} onClose={onClose} title={t("family.newGroup")}
      footer={<><Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button><Button variant="primary" loading={create.isPending} disabled={!name.trim()} onClick={() => create.mutate()}>{t("family.create")}</Button></>}>
      <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); if (name.trim()) create.mutate(); }}>
        {create.error && <Notice tone="danger">{create.error.message}</Notice>}
        <Field label={t("family.groupName")} hint={t("family.groupNameHint")}>{(id, d) => <Input id={id} aria-describedby={d} value={name} maxLength={60} placeholder={t("family.groupNamePlaceholder")} onChange={(e) => setName(e.target.value)} />}</Field>
        <p className="text-sm text-muted">{t("family.createNote")}</p>
      </form>
    </Dialog>
  );
}

/** Accepting an invitation and changing what you share use the same, explicit choices. */
function SharingDialog({ target, onClose }: { target: { group: Group; mine: Member } | null; onClose: () => void }) {
  const { t } = useTranslation();
  const [form, setForm] = useState<{ live: boolean; history: (typeof historyOptions)[number]; precision: "exact" | "approx" } | null>(null);
  const accepting = target?.mine.status === "invited";
  const current = target && (form ?? { live: accepting ? true : target.mine.share_live, history: String(accepting ? 1 : target.mine.share_history_days) as (typeof historyOptions)[number], precision: target.mine.precision });
  const save = useMutation({
    mutationFn: () => api(`/groups/${target!.group.id}/me`, { method: "PUT", body: { share_live: current!.live, share_history_days: Number(current!.history), precision: current!.precision, paused_until: null } }),
    onSuccess: () => { refresh(); setForm(null); onClose(); },
  });
  const close = () => { setForm(null); onClose(); };
  if (!current) return null;
  const set = (patch: Partial<typeof current>) => setForm({ ...current, ...patch });
  return (
    <Dialog open={!!target} onClose={close} wide title={accepting ? t("family.acceptTitle", { group: target!.group.name }) : t("family.sharingTitle", { group: target!.group.name })}
      footer={<><Button variant="ghost" onClick={close}>{t("common.cancel")}</Button><Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>{accepting ? t("family.joinAndShare") : t("common.save")}</Button></>}>
      <div className="space-y-6">
        <p className="text-sm text-muted">{t("family.sharingIntro", { count: target!.group.members.length - 1 })}</p>
        <div className="flex items-center justify-between gap-4 rounded-xl border border-border p-4">
          <div>
            <p className="font-medium">{t("family.liveLabel")}</p>
            <p className="text-sm text-muted">{t("family.liveHint")}</p>
          </div>
          <Switch label={t("family.liveLabel")} checked={current.live} onChange={(live) => set({ live })} />
        </div>
        <Field label={t("family.historyLabel")} hint={t("family.historyHint")}>
          {(id) => <Select id={id} label={t("family.historyLabel")} value={current.history} onChange={(history) => set({ history })} options={historyOptions.map((h) => ({ value: h, label: t(`family.hist.${h}`) }))} />}
        </Field>
        <div>
          <p className="mb-2 text-sm font-medium">{t("share.precision")}</p>
          <RadioCards label={t("share.precision")} value={current.precision} onChange={(precision) => set({ precision })} options={[
            { value: "exact", label: t("share.exact"), hint: t("family.exactHint") },
            { value: "approx", label: t("share.approx"), hint: t("family.approxHint") },
          ]} />
        </div>
        <Notice>{t("family.consentNote")}</Notice>
      </div>
    </Dialog>
  );
}

function WhoSeesMe({ groups }: { groups: Group[] }) {
  const { t } = useTranslation();
  const me = useUser();
  const rows = groups.flatMap((g) => {
    const mine = g.members.find((m) => m.user_id === me.id)!;
    const sharing = (mine.share_live || mine.share_history_days !== 0) && !(mine.paused_until && mine.paused_until > Date.now());
    return sharing ? g.members.filter((m) => m.user_id !== me.id && m.status === "active").map((m) => ({ m, g, mine })) : [];
  });
  return (
    <section className="rounded-2xl border border-border bg-surface p-5">
      <h2 className="flex items-center gap-2 font-semibold"><Eye className="size-4 text-subtle" aria-hidden />{t("family.whoSeesMe")}</h2>
      {rows.length === 0 ? (
        <p className="mt-2 text-sm text-muted">{t("family.nobodySees")}</p>
      ) : (
        <ul className="mt-3 space-y-2">
          {rows.map(({ m, g, mine }) => (
            <li key={`${g.id}-${m.user_id}`} className="flex items-center gap-3 text-sm">
              <Avatar name={m.name} color={m.color} className="size-7 text-xs" />
              <span className="font-medium">{m.name}</span>
              <span className="min-w-0 truncate text-muted">{sharingSummary(mine, t)} · {g.name}</span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function Alerts() {
  const { t } = useTranslation();
  const alerts = useQuery({ queryKey: ["alerts"], queryFn: () => api<AlertRule[]>("/alerts") });
  const remove = useMutation({ mutationFn: (id: number) => api(`/alerts/${id}`, { method: "DELETE" }), onSuccess: refresh });
  if (!alerts.data?.length) return null;
  return (
    <section className="rounded-2xl border border-border bg-surface p-5">
      <h2 className="flex items-center gap-2 font-semibold"><Bell className="size-4 text-subtle" aria-hidden />{t("family.alertsTitle")}</h2>
      <ul className="mt-3 divide-y divide-border">
        {alerts.data.map((a) => (
          <li key={a.id} className="flex items-center gap-3 py-2.5 text-sm">
            <span className="min-w-0 flex-1">{t("family.alertRule", { name: a.subject_name, place: a.place_name, when: [a.on_arrive && t("family.arrives"), a.on_leave && t("family.leaves")].filter(Boolean).join(t("family.or")) })}</span>
            <Button size="icon" variant="ghost" className="size-8" aria-label={t("common.delete")} onClick={() => remove.mutate(a.id)}><Trash2 className="size-4" /></Button>
          </li>
        ))}
      </ul>
    </section>
  );
}

function AlertDialog({ person, onClose }: { person: FamilyPerson | null; onClose: () => void }) {
  const { t } = useTranslation();
  const places = usePlaces();
  const [placeId, setPlaceId] = useState("");
  const [arrive, setArrive] = useState(true);
  const [leaveToo, setLeaveToo] = useState(false);
  const create = useMutation({
    mutationFn: () => api("/alerts", { method: "POST", body: { subject_id: person!.user_id, place_id: Number(placeId), on_arrive: arrive, on_leave: leaveToo } }),
    onSuccess: () => { refresh(); onClose(); },
  });
  const opts = (places.data ?? []).map((p) => ({ value: String(p.id), label: p.name }));
  return (
    <Dialog open={!!person} onClose={onClose} title={t("family.alertTitle", { name: person?.name })}
      footer={<><Button variant="ghost" onClick={onClose}>{t("common.cancel")}</Button><Button variant="primary" loading={create.isPending} disabled={!placeId || (!arrive && !leaveToo)} onClick={() => create.mutate()}>{t("family.createAlert")}</Button></>}>
      {opts.length === 0 ? (
        <EmptyState icon={MapPin} title={t("family.noPlacesTitle")} body={t("family.noPlacesBody")} action={<Link to="/places" className="text-sm font-medium text-primary">{t("nav.places")}</Link>} />
      ) : (
        <div className="space-y-5">
          {create.error && <Notice tone="danger">{create.error.message}</Notice>}
          <Field label={t("family.atPlace")}>{(id) => <Select id={id} label={t("family.atPlace")} value={placeId} onChange={setPlaceId} options={opts} placeholder={t("family.choosePlace")} />}</Field>
          <div className="space-y-3">
            <Checkbox checked={arrive} onChange={setArrive} label={t("family.whenArrives", { name: person?.name })} />
            <Checkbox checked={leaveToo} onChange={setLeaveToo} label={t("family.whenLeaves", { name: person?.name })} />
          </div>
          <p className="text-xs text-muted">{t("family.alertChannels")}</p>
        </div>
      )}
    </Dialog>
  );
}


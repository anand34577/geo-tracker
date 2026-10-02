import { useState, type FormEvent } from "react";
import { useParams } from "react-router";
import { useMutation } from "@tanstack/react-query";
import { Bell, Database, Palette, Puzzle, Smartphone, UserRound } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, queryClient, useConfig, useUser, type Prefs, type User } from "../lib/api";
import { usePrefs, useSavePrefs } from "../lib/prefs";
import { TabLayout } from "../components/Tabs";
import { Button, cn, Field, Input, Section, Segmented, useToast } from "../components/ui";
import { Select } from "../components/controls";
import DevicesTab from "./settings/Devices";
import DataTab from "./settings/Data";
import NotificationsTab from "./settings/Notifications";
import IntegrationsTab from "./settings/Integrations";
import { SessionsSection } from "./settings/Security";

export default function SettingsPage() {
  const { t } = useTranslation();
  const tab = useParams().tab ?? "profile";
  const tabs = [
    { id: "profile", label: t("settings.profile"), icon: UserRound },
    { id: "appearance", label: t("settings.appearance"), icon: Palette },
    { id: "devices", label: t("settings.devices"), icon: Smartphone },
    { id: "notifications", label: t("settings.notifications"), icon: Bell },
    { id: "integrations", label: t("settings.integrations"), icon: Puzzle },
    { id: "data", label: t("settings.data"), icon: Database },
  ];
  return (
    <TabLayout title={t("nav.settings")} base="/settings" tabs={tabs}>
      {tab === "profile" && <ProfileTab />}
      {tab === "appearance" && <AppearanceTab />}
      {tab === "devices" && <DevicesTab />}
      {tab === "notifications" && <NotificationsTab />}
      {tab === "integrations" && <IntegrationsTab />}
      {tab === "data" && <DataTab />}
    </TabLayout>
  );
}

function ProfileTab() {
  const { t } = useTranslation();
  const user = useUser();
  const toast = useToast();
  const [profile, setProfile] = useState({ name: user.name, email: user.email });
  const [pw, setPw] = useState({ current: "", new: "" });
  const [emailPw, setEmailPw] = useState("");
  const emailChanged = profile.email.trim().toLowerCase() !== user.email;

  const saveProfile = useMutation({
    meta: { inline: true }, // error shown in the form
    mutationFn: () => api<User>("/me", { method: "PATCH", body: { ...profile, password: emailPw } }),
    onSuccess: (u) => {
      queryClient.setQueryData(["me"], u);
      setEmailPw("");
      toast("success", t("settings.saved"));
    },
  });
  const savePw = useMutation({
    meta: { inline: true }, // error shown in the form
    mutationFn: () => api("/me/password", { method: "POST", body: pw }),
    onSuccess: () => {
      setPw({ current: "", new: "" });
      toast("success", t("settings.passwordChanged"));
    },
  });
  const submit = (m: { mutate: () => void }) => (e: FormEvent) => {
    e.preventDefault();
    m.mutate();
  };

  return (
    <>
      <Section title={t("settings.profile")}>
        <form className="grid max-w-md gap-4" onSubmit={submit(saveProfile)}>
          <Field label={t("setup.name")}>{(id) => <Input id={id} value={profile.name} onChange={(e) => setProfile({ ...profile, name: e.target.value })} autoComplete="name" />}</Field>
          <Field label={t("auth.email")} error={saveProfile.error?.message}>
            {(id) => <Input id={id} type="email" value={profile.email} onChange={(e) => setProfile({ ...profile, email: e.target.value })} autoComplete="email" />}
          </Field>
          {emailChanged && (
            <Field label={t("settings.confirmWithPassword")} hint={t("settings.confirmEmailHint")}>
              {(id, d) => <Input id={id} aria-describedby={d} type="password" autoComplete="current-password" value={emailPw} onChange={(e) => setEmailPw(e.target.value)} />}
            </Field>
          )}
          <div>
            <Button variant="primary" type="submit" loading={saveProfile.isPending} disabled={emailChanged && !emailPw}>{t("common.save")}</Button>
          </div>
        </form>
      </Section>
      <Section title={t("settings.password")} description={t("settings.passwordDesc")}>
        <form className="grid max-w-md gap-4" onSubmit={submit(savePw)}>
          <Field label={t("settings.currentPassword")}>
            {(id) => <Input id={id} type="password" autoComplete="current-password" value={pw.current} onChange={(e) => setPw({ ...pw, current: e.target.value })} />}
          </Field>
          <Field label={t("settings.newPassword")} hint={t("setup.passwordHint")} error={savePw.error?.message}>
            {(id, d) => <Input id={id} aria-describedby={d} type="password" autoComplete="new-password" value={pw.new} onChange={(e) => setPw({ ...pw, new: e.target.value })} />}
          </Field>
          <div>
            <Button type="submit" loading={savePw.isPending} disabled={!pw.current || pw.new.length < 10}>{t("settings.changePassword")}</Button>
          </div>
        </form>
      </Section>
      <SessionsSection />
    </>
  );
}

const accents: { id: NonNullable<Prefs["accent"]>; color: string }[] = [
  { id: "teal", color: "#0f766e" },
  { id: "blue", color: "#1d4ed8" },
  { id: "green", color: "#15803d" },
  { id: "amber", color: "#b45309" },
  { id: "rose", color: "#be123c" },
  { id: "slate", color: "#334155" },
];

function AppearanceTab() {
  const { t } = useTranslation();
  const prefs = usePrefs();
  const toast = useToast();
  const { data: config } = useConfig();
  const set = useSavePrefs((e) => toast("error", e.message));

  return (
    <Section title={t("settings.appearance")} description={t("settings.appearanceDesc")}>
      <div className="grid gap-6">
        <Row label={t("settings.theme")}>
          <Segmented label={t("settings.theme")} value={prefs.theme} onChange={(theme) => set({ theme })}
            options={[{ value: "system", label: t("settings.system") }, { value: "light", label: t("settings.light") }, { value: "dark", label: t("settings.dark") }]} />
        </Row>
        <Row label={t("settings.accent")}>
          <div role="radiogroup" aria-label={t("settings.accent")} className="flex flex-wrap gap-2">
            {accents.map((a) => (
              <button key={a.id} role="radio" aria-checked={prefs.accent === a.id} aria-label={a.id} title={a.id} onClick={() => set({ accent: a.id })}
                className={cn("size-9 rounded-full border-2 transition-transform", prefs.accent === a.id ? "scale-110 border-fg" : "border-transparent hover:scale-105")}
                style={{ background: a.color }} />
            ))}
          </div>
        </Row>
        {config && config.basemaps.length > 1 && (
          <Row label={t("map.basemap")}>
            <div className="sm:w-72">
              <Select label={t("map.basemap")} value={prefs.basemap || config.basemap_default} onChange={(basemap) => set({ basemap })} options={config.basemaps.map((b) => ({ value: b.id, label: b.name }))} />
            </div>
          </Row>
        )}
        <Row label={t("settings.units")}>
          <Segmented label={t("settings.units")} value={prefs.units} onChange={(units) => set({ units })}
            options={[{ value: "metric", label: t("settings.metric") }, { value: "imperial", label: t("settings.imperial") }]} />
        </Row>
        <Row label={t("settings.clock")}>
          <Segmented label={t("settings.clock")} value={prefs.clock} onChange={(clock) => set({ clock })}
            options={[{ value: "24h", label: "24h" }, { value: "12h", label: "12h" }]} />
        </Row>
      </div>
    </Section>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
      <span className="text-sm font-medium">{label}</span>
      {children}
    </div>
  );
}

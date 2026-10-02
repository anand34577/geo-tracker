import { useMutation, useQuery } from "@tanstack/react-query";
import { Laptop, LogOut, Smartphone } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, queryClient, type SessionInfo } from "../../lib/api";
import { ago, dateTime } from "../../lib/format";
import { usePrefs } from "../../lib/prefs";
import { Button, ErrorState, Section, Skeleton, useToast } from "../../components/ui";

/** A readable device name from a user agent, without a parsing library. */
export function describeAgent(ua: string): { label: string; mobile: boolean } {
  const browser = /Edg\//.test(ua) ? "Edge" : /Firefox\//.test(ua) ? "Firefox" : /Chrome\//.test(ua) ? "Chrome" : /Safari\//.test(ua) ? "Safari" : ua.split("/")[0] || "Unknown";
  const os = /Android/.test(ua) ? "Android" : /iPhone|iPad/.test(ua) ? "iOS" : /Windows/.test(ua) ? "Windows" : /Mac OS/.test(ua) ? "macOS" : /Linux/.test(ua) ? "Linux" : "";
  return { label: os ? `${browser} on ${os}` : browser, mobile: /Android|iPhone|iPad|Mobile/.test(ua) };
}

export function SessionsSection() {
  const { t } = useTranslation();
  const toast = useToast();
  const { clock } = usePrefs();
  const sessions = useQuery({ queryKey: ["sessions"], queryFn: () => api<SessionInfo[]>("/me/sessions") });
  const revoke = useMutation({
    mutationFn: (s: SessionInfo) => api(`/me/sessions/${s.id}`, { method: "DELETE" }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["sessions"] });
      toast("success", t("security.revoked"));
    },
  });
  return (
    <Section title={t("security.sessions")} description={t("security.sessionsDesc")}>
      {sessions.isError ? (
        <ErrorState error={sessions.error} retry={() => sessions.refetch()} />
      ) : sessions.isPending ? (
        <Skeleton className="h-16" />
      ) : (
        <ul className="-my-2 divide-y divide-border">
          {sessions.data.map((s) => {
            const d = describeAgent(s.user_agent);
            const Icon = d.mobile ? Smartphone : Laptop;
            return (
              <li key={s.id} className="flex flex-wrap items-center gap-3 py-3">
                <span className="grid size-9 place-items-center rounded-lg bg-surface-2"><Icon className="size-4 text-muted" aria-hidden /></span>
                <div className="min-w-0 flex-1">
                  <p className="text-sm font-medium">
                    {d.label}
                    {s.current && <span className="ml-2 rounded-full bg-primary-subtle px-2 py-0.5 text-xs font-medium text-primary">{t("security.thisDevice")}</span>}
                  </p>
                  <p className="text-xs text-muted" title={dateTime(s.created_at, clock)}>{t("security.signedIn", { when: ago(s.created_at), ip: s.ip || "?" })}</p>
                </div>
                {!s.current && (
                  <Button size="sm" variant="ghost" icon={LogOut} loading={revoke.isPending && revoke.variables?.id === s.id} onClick={() => revoke.mutate(s)}>
                    {t("security.signOut")}
                  </Button>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </Section>
  );
}

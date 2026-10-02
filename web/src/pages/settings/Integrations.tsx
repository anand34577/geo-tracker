import { useEffect, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { CheckCircle2, Image, Unplug } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, queryClient } from "../../lib/api";
import { Button, Field, Input, Notice, Section, Skeleton, useToast } from "../../components/ui";

type Resp = { immich: { url: string; connected: boolean } };

export default function IntegrationsTab() {
  const { t } = useTranslation();
  const toast = useToast();
  const q = useQuery({ queryKey: ["integrations"], queryFn: () => api<Resp>("/me/integrations") });
  const [url, setUrl] = useState("");
  const [key, setKey] = useState("");
  useEffect(() => {
    if (q.data) setUrl(q.data.immich.url);
  }, [q.data]);
  const save = useMutation({
    meta: { inline: true }, // error shown in the form
    mutationFn: (body: { url: string; api_key: string }) => api<Resp>("/me/integrations", { method: "PUT", body: { immich: body } }),
    onSuccess: (r) => {
      queryClient.setQueryData(["integrations"], r);
      queryClient.invalidateQueries({ queryKey: ["photos"] });
      setKey("");
      toast("success", r.immich.connected ? t("integrations.connected") : t("integrations.disconnected"));
    },
  });
  if (!q.data) return <Skeleton className="h-64" />;
  const connected = q.data.immich.connected;
  return (
    <Section
      title="Immich"
      description={t("integrations.immichDesc")}
      actions={connected ? <span className="flex items-center gap-1.5 text-sm font-medium text-success"><CheckCircle2 className="size-4" aria-hidden />{t("integrations.statusConnected")}</span> : undefined}
    >
      <form className="grid max-w-lg gap-4" onSubmit={(e) => { e.preventDefault(); save.mutate({ url, api_key: key }); }}>
        {save.error && <Notice tone="danger">{save.error.message}</Notice>}
        <Field label={t("integrations.immichUrl")}>{(id) => <Input id={id} value={url} placeholder="https://photos.example.com" onChange={(e) => setUrl(e.target.value)} />}</Field>
        <Field label={t("integrations.apiKey")} hint={connected ? t("integrations.apiKeyKeep") : t("integrations.apiKeyHint")}>
          {(id, d) => <Input id={id} aria-describedby={d} type="password" autoComplete="off" value={key} placeholder={connected ? "••••••••" : ""} onChange={(e) => setKey(e.target.value)} />}
        </Field>
        <div className="flex flex-wrap gap-2">
          <Button type="submit" variant="primary" icon={Image} loading={save.isPending && !!url} disabled={!url || (!connected && !key)}>
            {connected ? t("common.save") : t("integrations.connect")}
          </Button>
          {connected && (
            <Button type="button" icon={Unplug} loading={save.isPending && !url} onClick={() => { setUrl(""); save.mutate({ url: "", api_key: "" }); }}>
              {t("integrations.disconnect")}
            </Button>
          )}
        </div>
        <p className="text-xs text-muted">{t("integrations.privacy")}</p>
      </form>
    </Section>
  );
}

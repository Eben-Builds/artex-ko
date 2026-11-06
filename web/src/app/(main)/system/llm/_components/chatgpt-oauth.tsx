"use client";

import * as React from "react";

import { useTranslations } from "next-intl";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { http } from "@/lib/api";

type Status = { connected: boolean; pending: boolean; email?: string; error?: string };
type Model = { id: string; name: string };

export function ChatGPTOAuth({ onConfigured }: { onConfigured: () => Promise<void> }) {
  const t = useTranslations("chatgptOAuth");
  const [status, setStatus] = React.useState<Status | null>(null);
  const [models, setModels] = React.useState<Model[]>([]);
  const [model, setModel] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState("");
  const [authUrl, setAuthUrl] = React.useState("");

  const refresh = React.useCallback(async () => {
    try {
      setStatus(await http<Status>("/llm/oauth/chatgpt"));
      setError("");
    } catch (e) {
      setError((e as Error).message);
    }
  }, []);

  React.useEffect(() => {
    void refresh();
  }, [refresh]);

  React.useEffect(() => {
    if (!status?.pending) return;
    const timer = setInterval(() => void refresh(), 2000);
    return () => clearInterval(timer);
  }, [status?.pending, refresh]);

  React.useEffect(() => {
    if (!status?.connected || status.pending) return;
    let cancelled = false;
    http<{ models: Model[] }>("/llm/oauth/chatgpt/models")
      .then((result) => {
        if (cancelled) return;
        setModels(result.models);
        setModel((current) => (result.models.some((m) => m.id === current) ? current : (result.models[0]?.id ?? "")));
      })
      .catch((e: Error) => {
        if (!cancelled) setError(e.message);
      });
    return () => {
      cancelled = true;
    };
  }, [status?.connected, status?.pending]);

  async function start() {
    setBusy(true);
    setError("");
    try {
      const result = await http<{ url: string }>("/llm/oauth/chatgpt/start", { method: "POST" });
      const url = new URL(result.url);
      if (url.origin !== "https://auth.openai.com") throw new Error(t("invalidUrl"));
      setAuthUrl(result.url);
      window.open(result.url, "_blank", "noopener,noreferrer");
      await refresh();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  async function configure() {
    setBusy(true);
    try {
      await http("/llm/oauth/chatgpt/configure", { method: "POST", body: JSON.stringify({ model }) });
      await onConfigured();
      toast.success(t("saved"));
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  async function disconnect() {
    setBusy(true);
    try {
      await http("/llm/oauth/chatgpt", { method: "DELETE" });
      setModels([]);
      setModel("");
      setAuthUrl("");
      await refresh();
      await onConfigured();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card>
      <CardContent className="space-y-3 pt-5">
        <h2 className="font-semibold">{t("title")}</h2>
        <p className="text-muted-foreground text-sm">{t("description")}</p>
        <p className="text-sm" role="status">
          {status?.pending
            ? t("pending")
            : status?.connected
              ? t("connected", { email: status.email ?? "ChatGPT" })
              : t("disconnected")}
        </p>
        {(error || status?.error) && (
          <p className="text-destructive text-sm" role="alert">
            {error || status?.error}
          </p>
        )}
        <div className="flex flex-wrap items-center gap-2">
          <Button variant="outline" disabled={busy || status?.pending} onClick={() => void start()}>
            {t("continue")}
          </Button>
          <Button variant="ghost" disabled={busy} onClick={() => void refresh()}>
            {t("refresh")}
          </Button>
          {(status?.connected || status?.pending) && (
            <Button variant="ghost" disabled={busy} onClick={() => void disconnect()}>
              {t("disconnect")}
            </Button>
          )}
          {status?.pending && authUrl && (
            <a className="text-sm underline" href={authUrl} target="_blank" rel="noopener noreferrer">
              {t("openLogin")}
            </a>
          )}
        </div>
        {status?.connected && !status.pending && (
          <div className="flex flex-wrap items-center gap-2">
            <Label htmlFor="chatgpt-oauth-model">{t("model")}</Label>
            <select
              id="chatgpt-oauth-model"
              className="rounded-md border bg-background p-2 text-sm"
              value={model}
              onChange={(e) => setModel(e.target.value)}
              disabled={busy || !models.length}
            >
              {!models.length && <option value="">{t("noModels")}</option>}
              {models.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.name || m.id}
                </option>
              ))}
            </select>
            <Button disabled={busy || !model} onClick={() => void configure()}>
              {t("activate")}
            </Button>
          </div>
        )}
        <p className="text-muted-foreground text-xs">{t("limits")}</p>
        <a
          className="text-muted-foreground text-xs underline"
          href="https://chatgpt.com/#settings"
          target="_blank"
          rel="noopener noreferrer"
        >
          {t("manage")}
        </a>
      </CardContent>
    </Card>
  );
}

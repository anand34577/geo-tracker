import { useEffect, useSyncExternalStore } from "react";
import { useMutation } from "@tanstack/react-query";
import { api, queryClient, useUser, type Prefs, type User } from "./api";

// Preferences live on the server (synced across devices) and are cached in
// localStorage so the right theme paints before the first request returns.
const CACHE = "gt-prefs";

export function cachedPrefs(): Prefs {
  try {
    return JSON.parse(localStorage.getItem(CACHE) ?? "{}");
  } catch {
    return {};
  }
}

const darkQuery = window.matchMedia("(prefers-color-scheme: dark)");

export function applyPrefs(p: Prefs) {
  const dark = p.theme === "dark" || ((p.theme ?? "system") === "system" && darkQuery.matches);
  const root = document.documentElement;
  root.classList.toggle("dark", dark);
  if (p.accent && p.accent !== "teal") root.dataset.accent = p.accent;
  else delete root.dataset.accent;
  document.querySelector('meta[name="theme-color"]')?.setAttribute("content", dark ? "#0c0a09" : "#fafaf9");
  try {
    localStorage.setItem(CACHE, JSON.stringify(p));
  } catch {
    /* private mode */
  }
  window.dispatchEvent(new Event("gt-theme"));
}

/** Keeps the document theme in sync with the user's prefs and the OS setting. */
export function useApplyPrefs(p: Prefs) {
  useEffect(() => {
    applyPrefs(p);
    const onChange = () => applyPrefs(p);
    darkQuery.addEventListener("change", onChange);
    return () => darkQuery.removeEventListener("change", onChange);
  }, [p]);
}

/** Whether dark mode is currently active (re-renders on change). */
export function useIsDark(): boolean {
  return useSyncExternalStore(
    (cb) => {
      window.addEventListener("gt-theme", cb);
      return () => window.removeEventListener("gt-theme", cb);
    },
    () => document.documentElement.classList.contains("dark"),
  );
}

export function usePrefs(): Required<Prefs> {
  const p = useUser()?.prefs ?? cachedPrefs(); // signed-out pages (share links) use the cached look
  return { theme: p.theme ?? "system", accent: p.accent ?? "teal", units: p.units ?? "metric", clock: p.clock ?? "24h", basemap: p.basemap ?? "" };
}

/** Saves preference changes optimistically: the UI updates before the server confirms. */
export function useSavePrefs(onError?: (e: Error) => void) {
  const user = useUser();
  const prefs = usePrefs();
  const m = useMutation({
    mutationFn: (p: Prefs) => api<User>("/me", { method: "PATCH", body: { prefs: p } }),
    onMutate: (p) => {
      applyPrefs(p);
      if (user) queryClient.setQueryData(["me"], { ...user, prefs: p });
    },
    onSuccess: (u) => queryClient.setQueryData(["me"], u),
    onError: (e) => {
      if (user) {
        queryClient.setQueryData(["me"], user);
        applyPrefs(user.prefs);
      }
      onError?.(e);
    },
  });
  return (patch: Prefs) => m.mutate({ ...prefs, ...patch });
}

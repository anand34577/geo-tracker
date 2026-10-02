import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "react-router";
import "./index.css";
import "./lib/i18n";
import { queryClient } from "./lib/api";
import { applyPrefs, cachedPrefs } from "./lib/prefs";
import { ToastProvider } from "./components/ui";
import { router } from "./App";

applyPrefs(cachedPrefs()); // paint the right theme before any request returns

// Installable app + offline shell (production builds only; dev uses Vite's own server).
if (import.meta.env.PROD && "serviceWorker" in navigator) {
  window.addEventListener("load", () => navigator.serviceWorker.register("/sw.js").catch(() => {}));
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <ToastProvider>
        <RouterProvider router={router} />
      </ToastProvider>
    </QueryClientProvider>
  </StrictMode>,
);

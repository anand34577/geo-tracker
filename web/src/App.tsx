import { lazy, Suspense, useEffect, useRef, useState } from "react";
import { createBrowserRouter, Link, Navigate, NavLink, Outlet, useLocation, useNavigate } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { BarChart3, CalendarDays, ChevronsUpDown, LogOut, Map as MapIcon, MapPin, Menu, Search, Settings, Share2, ShieldCheck, Users, Zap, type LucideIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, queryClient, useMe, useUser } from "./lib/api";
import { useLiveUpdates } from "./lib/live";
import { useApplyPrefs } from "./lib/prefs";
import { cn, EmptyState, ErrorState, Spinner } from "./components/ui";
import { Popover } from "./components/controls";
import { CommandPalette } from "./components/CommandPalette";
import { Logo } from "./components/Logo";
import { Login, Setup } from "./pages/Auth";

// Route-level code splitting: MapLibre only loads with the pages that need it.
const MapPage = lazy(() => import("./pages/MapPage"));
const TimelinePage = lazy(() => import("./pages/TimelinePage"));
const InsightsPage = lazy(() => import("./pages/InsightsPage"));
const PlacesPage = lazy(() => import("./pages/PlacesPage"));
const AutomationsPage = lazy(() => import("./pages/AutomationsPage"));
const SharingPage = lazy(() => import("./pages/SharingPage"));
const FamilyPage = lazy(() => import("./pages/FamilyPage"));
const SettingsPage = lazy(() => import("./pages/SettingsPage"));
const AdminPage = lazy(() => import("./pages/AdminPage"));
const PublicShare = lazy(() => import("./pages/PublicShare"));

export const router = createBrowserRouter([
  { path: "/setup", element: <Setup /> },
  { path: "/login", element: <Login /> },
  { path: "/s/:token", element: <Suspense fallback={<Spinner />}><PublicShare /></Suspense> },
  {
    element: <Protected />,
    children: [
      {
        element: <Shell />,
        children: [
          { index: true, element: <MapPage /> },
          { path: "timeline/:date?", element: <TimelinePage /> },
          { path: "insights", element: <InsightsPage /> },
          { path: "places", element: <PlacesPage /> },
          { path: "family", element: <FamilyPage /> },
          { path: "automations", element: <AutomationsPage /> },
          { path: "sharing", element: <SharingPage /> },
          { path: "settings/:tab?", element: <SettingsPage /> },
          { path: "admin/:tab?", element: <AdminPage /> },
          { path: "*", element: <NotFound /> },
        ],
      },
    ],
  },
]);

function Protected() {
  const me = useMe();
  const loc = useLocation();
  const setup = useQuery({ queryKey: ["setup"], queryFn: () => api<{ needs_setup: boolean }>("/setup"), enabled: me.data === null });
  if (me.isPending || (me.data === null && setup.isPending)) return <Spinner />;
  if (me.isError) return <ErrorState error={me.error} retry={() => me.refetch()} />;
  if (me.data === null) {
    if (setup.data?.needs_setup) return <Navigate to="/setup" replace />;
    const next = loc.pathname + loc.search;
    return <Navigate to={next === "/" ? "/login" : `/login?next=${encodeURIComponent(next)}`} replace />;
  }
  return <Authed />;
}

function Authed() {
  const user = useUser();
  useApplyPrefs(user.prefs);
  useLiveUpdates();
  return <Outlet />;
}

type NavItem = { to: string; icon: LucideIcon; key: string; end?: boolean };
const primaryNav: NavItem[] = [
  { to: "/", icon: MapIcon, key: "nav.map", end: true },
  { to: "/timeline", icon: CalendarDays, key: "nav.timeline" },
  { to: "/family", icon: Users, key: "nav.family" },
  { to: "/insights", icon: BarChart3, key: "nav.insights" },
  { to: "/places", icon: MapPin, key: "nav.places" },
  { to: "/automations", icon: Zap, key: "nav.automations" },
  { to: "/sharing", icon: Share2, key: "nav.sharing" },
];

function useSignOut() {
  const navigate = useNavigate();
  return async () => {
    await api("/auth/logout", { method: "POST" }).catch(() => {});
    queryClient.clear();
    queryClient.setQueryData(["me"], null);
    navigate("/login");
  };
}

function Shell() {
  const { t } = useTranslation();
  const loc = useLocation();
  const fullBleed = loc.pathname === "/" || loc.pathname.startsWith("/timeline") || loc.pathname === "/places";
  const [palette, setPalette] = useState(false);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setPalette((p) => !p);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  return (
    <div className="flex h-dvh">
      <CommandPalette open={palette} onClose={() => setPalette(false)} />
      <a href="#main" className="sr-only focus:not-sr-only focus:absolute focus:top-2 focus:left-4 focus:z-50 focus:rounded-lg focus:bg-surface focus:px-3 focus:py-2">
        {t("nav.skip")}
      </a>
      <Sidebar onSearch={() => setPalette(true)} />
      <div className="flex min-w-0 flex-1 flex-col">
        <main id="main" className={cn("min-h-0 flex-1", fullBleed ? "relative" : "overflow-y-auto")}>
          <Suspense fallback={<Spinner />}>
            <Outlet />
          </Suspense>
        </main>
        <MobileNav onSearch={() => setPalette(true)} />
      </div>
    </div>
  );
}

function Sidebar({ onSearch }: { onSearch: () => void }) {
  const { t } = useTranslation();
  const user = useUser();
  const signOut = useSignOut();
  const loc = useLocation();
  const accountBtn = useRef<HTMLButtonElement>(null);
  const [menu, setMenu] = useState(false);
  useEffect(() => { setMenu(false); }, [loc.pathname]);

  const item = ({ isActive }: { isActive: boolean }) =>
    cn(
      "group relative flex h-10 items-center gap-3 rounded-lg px-3 text-sm font-medium transition-colors",
      isActive ? "bg-side-active text-side-fg" : "text-side-muted hover:bg-side-hover hover:text-side-fg",
    );
  const indicator = (active: boolean) => <span className={cn("absolute top-2 bottom-2 left-0 w-0.5 rounded-full bg-primary transition-opacity", active ? "opacity-100" : "opacity-0")} aria-hidden />;

  return (
    <aside className="hidden w-60 shrink-0 flex-col border-r border-side-border bg-side text-side-fg md:flex">
      <Link to="/" className="flex h-16 items-center gap-2.5 px-5 font-semibold tracking-tight" aria-label="GeoTracker">
        <Logo className="size-8" />
        <span className="text-[15px]">GeoTracker</span>
      </Link>
      <button onClick={onSearch} className="mx-3 mb-2 flex h-9 items-center gap-2.5 rounded-lg border border-side-border px-3 text-sm text-side-muted hover:bg-side-hover hover:text-side-fg">
        <Search className="size-4" aria-hidden />
        <span className="flex-1 text-left">{t("cmd.search")}</span>
        <kbd className="rounded border border-side-border px-1.5 text-[10px]">{navigator.platform.includes("Mac") ? "⌘K" : "Ctrl K"}</kbd>
      </button>
      <nav aria-label={t("nav.main")} className="flex flex-col gap-0.5 px-3 pt-2">
        {primaryNav.map((n) => (
          <NavLink key={n.to} to={n.to} end={n.end} className={item}>
            {({ isActive }) => (
              <>
                {indicator(isActive)}
                <n.icon className={cn("size-[18px]", isActive && "text-primary")} aria-hidden />
                {t(n.key)}
              </>
            )}
          </NavLink>
        ))}
      </nav>
      <div className="mt-auto flex flex-col gap-0.5 px-3 pb-3">
        <NavLink to="/settings" className={item}>
          {({ isActive }) => (<>{indicator(isActive)}<Settings className="size-[18px]" aria-hidden />{t("nav.settings")}</>)}
        </NavLink>
        {user.role === "admin" && (
          <NavLink to="/admin" className={item}>
            {({ isActive }) => (<>{indicator(isActive)}<ShieldCheck className="size-[18px]" aria-hidden />{t("nav.admin")}</>)}
          </NavLink>
        )}
        <button
          ref={accountBtn}
          onClick={() => setMenu(!menu)}
          aria-expanded={menu}
          aria-label={t("nav.account")}
          className="mt-2 flex items-center gap-3 rounded-xl border border-side-border p-2 text-left hover:bg-side-hover"
        >
          <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-primary text-sm font-semibold text-primary-fg" aria-hidden>
            {user.name.slice(0, 1).toUpperCase()}
          </span>
          <span className="min-w-0 flex-1">
            <span className="block truncate text-sm font-medium">{user.name}</span>
            <span className="block truncate text-xs text-side-muted">{user.email}</span>
          </span>
          <ChevronsUpDown className="size-4 text-side-muted" aria-hidden />
        </button>
        <Popover anchor={accountBtn} open={menu} onClose={() => setMenu(false)} className="w-56 p-1.5">
          <button onClick={signOut} className="flex h-9 w-full items-center gap-2.5 rounded-lg px-3 text-sm text-muted hover:bg-surface-2 hover:text-fg">
            <LogOut className="size-4" aria-hidden />
            {t("nav.signOut")}
          </button>
        </Popover>
      </div>
    </aside>
  );
}

function MobileNav({ onSearch }: { onSearch: () => void }) {
  const { t } = useTranslation();
  const user = useUser();
  const signOut = useSignOut();
  const loc = useLocation();
  const moreBtn = useRef<HTMLButtonElement>(null);
  const [more, setMore] = useState(false);
  useEffect(() => { setMore(false); }, [loc.pathname]);
  const tabs = [primaryNav[0], primaryNav[1], primaryNav[2], primaryNav[3]]; // map, timeline, family, insights
  const moreActive = !tabs.some((n) => (n.end ? loc.pathname === n.to : loc.pathname.startsWith(n.to)));
  const tabClass = (active: boolean) => cn("flex flex-col items-center justify-center gap-1 text-[11px] font-medium", active ? "text-primary" : "text-muted");
  const sheetLink = "flex h-11 items-center gap-3 rounded-lg px-3 text-sm hover:bg-surface-2";
  return (
    <nav aria-label={t("nav.main")} className="z-20 grid h-16 shrink-0 grid-cols-5 border-t border-border bg-surface pb-[env(safe-area-inset-bottom)] md:hidden">
      {tabs.map((n) => (
        <NavLink key={n.to} to={n.to} end={n.end} className={({ isActive }) => tabClass(isActive)}>
          <n.icon className="size-5" aria-hidden />
          {t(n.key)}
        </NavLink>
      ))}
      <button ref={moreBtn} onClick={() => setMore(!more)} aria-expanded={more} className={tabClass(moreActive || more)}>
        <Menu className="size-5" aria-hidden />
        {t("nav.more")}
      </button>
      <Popover anchor={moreBtn} open={more} onClose={() => setMore(false)} align="end" className="w-64 p-1.5">
        <div className="border-b border-border px-3 pt-2 pb-3">
          <p className="truncate font-medium">{user.name}</p>
          <p className="truncate text-sm text-muted">{user.email}</p>
        </div>
        <div className="flex flex-col pt-1.5">
          <button onClick={() => { setMore(false); onSearch(); }} className={sheetLink + " text-left"}><Search className="size-4 text-muted" />{t("cmd.search")}</button>
          <Link to="/places" className={sheetLink}><MapPin className="size-4 text-muted" />{t("nav.places")}</Link>
          <Link to="/automations" className={sheetLink}><Zap className="size-4 text-muted" />{t("nav.automations")}</Link>
          <Link to="/sharing" className={sheetLink}><Share2 className="size-4 text-muted" />{t("nav.sharing")}</Link>
          <Link to="/settings" className={sheetLink}><Settings className="size-4 text-muted" />{t("nav.settings")}</Link>
          {user.role === "admin" && <Link to="/admin" className={sheetLink}><ShieldCheck className="size-4 text-muted" />{t("nav.admin")}</Link>}
          <button onClick={signOut} className={sheetLink + " text-left"}><LogOut className="size-4 text-muted" />{t("nav.signOut")}</button>
        </div>
      </Popover>
    </nav>
  );
}

function NotFound() {
  const { t } = useTranslation();
  return <EmptyState icon={MapPin} title={t("common.notFound")} action={<Link to="/" className="text-sm font-medium text-primary">{t("common.goHome")}</Link>} />;
}

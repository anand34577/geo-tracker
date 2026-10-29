import type { ReactNode } from "react";
import { NavLink, useLocation } from "react-router";
import type { LucideIcon } from "lucide-react";
import { cn, pageClass } from "./ui";

/** Sub-navigation for settings-style pages: a side list on desktop and a scrollable row on mobile; both stay put while the content scrolls. */
export function TabLayout({ title, base, tabs, children }: { title: string; base: string; tabs: { id: string; label: string; icon: LucideIcon }[]; children: ReactNode }) {
  const { pathname } = useLocation();
  return (
    <div className={pageClass}>
      <h1 className="mb-8 text-[1.75rem] leading-tight font-semibold tracking-tight">{title}</h1>
      <div className="flex flex-col gap-6 lg:flex-row lg:gap-10">
        <nav aria-label={title} className="sticky top-0 z-10 -mx-4 flex gap-1 overflow-x-auto border-b border-border bg-bg px-4 py-2 md:-mx-8 md:px-8 lg:top-10 lg:mx-0 lg:w-48 lg:shrink-0 lg:flex-col lg:self-start lg:border-b-0 lg:bg-transparent lg:px-0 lg:py-0">
          {tabs.map((t, i) => (
            <NavLink
              key={t.id}
              to={`${base}/${t.id}`}
              className={({ isActive }) => {
                const active = isActive || (i === 0 && pathname === base);
                return cn("flex h-9 shrink-0 items-center gap-2.5 rounded-lg px-3 text-sm font-medium transition-colors", active ? "bg-primary-subtle text-primary ring-1 ring-primary/10" : "text-muted hover:bg-surface-2 hover:text-fg");
              }}
            >
              <t.icon className="size-4" aria-hidden />
              {t.label}
            </NavLink>
          ))}
        </nav>
        <div className="min-w-0 flex-1 space-y-6">{children}</div>
      </div>
    </div>
  );
}

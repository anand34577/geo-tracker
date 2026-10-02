import type { ReactNode } from "react";
import { NavLink } from "react-router";
import type { LucideIcon } from "lucide-react";
import { cn } from "./ui";

/** Sub-navigation for settings-style pages: a side list on desktop, a scrollable row on mobile. */
export function TabLayout({ title, base, tabs, children }: { title: string; base: string; tabs: { id: string; label: string; icon: LucideIcon }[]; children: ReactNode }) {
  return (
    <div className="mx-auto w-full max-w-5xl px-4 py-6 md:px-8 md:py-10">
      <h1 className="mb-6 text-2xl font-semibold tracking-tight">{title}</h1>
      <div className="flex flex-col gap-6 lg:flex-row lg:gap-10">
        <nav aria-label={title} className="-mx-4 flex gap-1 overflow-x-auto px-4 lg:mx-0 lg:w-48 lg:shrink-0 lg:flex-col lg:px-0">
          {tabs.map((t, i) => (
            <NavLink
              key={t.id}
              to={`${base}/${t.id}`}
              className={({ isActive }) => {
                const active = isActive || (i === 0 && location.pathname === base);
                return cn("flex h-9 shrink-0 items-center gap-2.5 rounded-lg px-3 text-sm font-medium", active ? "bg-surface-2 text-fg" : "text-muted hover:text-fg");
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

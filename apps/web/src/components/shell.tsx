"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useState } from "react";
import { useSession } from "@/lib/session";
import { visibleNav, type NavEntry } from "@/lib/permissions";
import { Badge } from "@/components/ui";
import { authBypassEnabled, mockDataEnabled } from "@/lib/auth-bypass";

/**
 * Role-aware application shell (#57): navigation is filtered by the session's
 * server-resolved permissions; the school switcher re-roots the tenant context
 * (X-School-ID) for every API call. Accessibility: skip-to-content link,
 * aria-current on the active entry (active state is never color-only — weight
 * changes too), programmatic labels on every control, and a mobile disclosure
 * drawer below the md breakpoint (the fixed sidebar collapses away).
 */

const FOCUS_RING = "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary";

function navLinkClass(active: boolean): string {
  return `block rounded-md px-3 py-2 text-sm transition ${FOCUS_RING} ${
    active ? "bg-primary font-semibold text-white" : "text-slate-600 hover:bg-slate-100"
  }`;
}

function NavLinks({
  nav,
  pathname,
  onNavigate,
}: {
  nav: NavEntry[];
  pathname: string;
  onNavigate?: () => void;
}) {
  return (
    <>
      {nav.map((entry) => {
        const active = pathname === entry.href;
        return (
          <Link
            key={entry.href}
            href={entry.href}
            aria-current={active ? "page" : undefined}
            className={navLinkClass(active)}
            onClick={onNavigate}
          >
            {entry.label}
          </Link>
        );
      })}
    </>
  );
}

/** School switcher with a programmatic label (id is scoped per instance). */
function SchoolSwitcher({ idSuffix }: { idSuffix: string }) {
  const { memberships, activeSchoolId, switchSchool } = useSession();
  const activeMembership = memberships.find((m) => m.schoolId === activeSchoolId);
  return (
    <div>
      <label
        htmlFor={`school-select-${idSuffix}`}
        className="mb-1 block text-[11px] font-medium uppercase tracking-wide text-slate-500"
      >
        Active school
      </label>
      <select
        id={`school-select-${idSuffix}`}
        className={`w-full rounded-md border border-slate-300 px-2 py-1 text-sm ${FOCUS_RING}`}
        value={activeSchoolId ?? ""}
        onChange={(e) => switchSchool(e.target.value)}
      >
        {memberships.length === 0 && <option value="">No memberships</option>}
        {memberships.map((m) => (
          <option key={m.schoolId} value={m.schoolId}>
            {m.schoolId.slice(0, 8)} · {m.role}
          </option>
        ))}
      </select>
      {activeMembership && (
        <div className="mt-2">
          <Badge tone={activeMembership.status === "active" ? "green" : "amber"}>
            {activeMembership.role}
          </Badge>
        </div>
      )}
    </div>
  );
}

function SignOutButton({ className = "" }: { className?: string }) {
  const { logout } = useSession();
  // Open-access mode (#141): signing out would instantly auto-relogin via the
  // session boot, so the control is hidden while the bypass is active
  // (documented in docs/operations/DEMO.md).
  if (authBypassEnabled()) return null;
  return (
    <button
      type="button"
      onClick={() => void logout()}
      className={`rounded-sm text-xs font-medium text-red-600 hover:text-red-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-red-600 ${className}`}
    >
      Sign out
    </button>
  );
}

export function Shell({ children }: { children: React.ReactNode }) {
  const { me, loading } = useSession();
  const pathname = usePathname();
  const nav = visibleNav(me);
  const [menuOpen, setMenuOpen] = useState(false);
  // Close the mobile drawer whenever navigation happens. Render-time
  // adjustment (React-documented "adjust state when a reactive value
  // changes") — replaces the setState-in-effect the react-hooks v6 rule
  // rejects (#102 Next 16 upgrade).
  const [prevPath, setPrevPath] = useState(pathname);
  if (prevPath !== pathname) {
    setPrevPath(pathname);
    setMenuOpen(false);
  }

  if (loading) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-slate-50">
        <p className="text-sm text-slate-500">Loading Skolara…</p>
      </div>
    );
  }

  return (
    <div className="flex min-h-screen flex-col bg-slate-50 md:flex-row">
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:absolute focus:left-2 focus:top-2 focus:z-50 focus:rounded-md focus:bg-primary focus:px-3 focus:py-2 focus:text-sm focus:font-medium focus:text-white"
      >
        Skip to content
      </a>

      {/* Mobile top bar — the sidebar is hidden below the md breakpoint. */}
      <header className="flex items-center justify-between border-b border-slate-200 bg-white px-4 py-3 md:hidden">
        <p className="text-lg font-bold text-primary">Skolara</p>
        <button
          type="button"
          className={`rounded-md border border-slate-300 px-3 py-1.5 text-sm font-medium text-slate-700 ${FOCUS_RING}`}
          aria-expanded={menuOpen}
          aria-controls="mobile-nav"
          onClick={() => setMenuOpen((v) => !v)}
        >
          {menuOpen ? "Close menu" : "Open menu"}
        </button>
      </header>

      {/* Mobile drawer: a disclosure panel (no focus trap needed) holding the
          same nav + school switcher as the desktop sidebar. */}
      {menuOpen && (
        <div id="mobile-nav" className="border-b border-slate-200 bg-white px-4 py-3 md:hidden">
          <SchoolSwitcher idSuffix="mobile" />
          <nav aria-label="Mobile navigation" className="mt-3 space-y-1">
            <NavLinks nav={nav} pathname={pathname} />
          </nav>
          <div className="mt-3 border-t border-slate-200 pt-3">
            <SignOutButton className="text-sm" />
          </div>
        </div>
      )}

      <aside className="hidden w-60 shrink-0 flex-col border-r border-slate-200 bg-white md:flex">
        <div className="border-b border-slate-200 px-5 py-4">
          <p className="text-lg font-bold text-primary">Skolara</p>
          <p className="text-xs text-slate-500">The Operating System for Schools</p>
          {/* Issue #142: make the sample-data mode visible so no one mistakes
              mock numbers (wallet, invoices, learners) for live figures. */}
          {mockDataEnabled() && (
            <p className="mt-2">
              <Badge tone="amber">
                <span className="sr-only">Deployment mode: </span>Demo data — not live
              </Badge>
            </p>
          )}
        </div>

        <div className="border-b border-slate-200 px-4 py-3">
          <SchoolSwitcher idSuffix="desktop" />
        </div>

        <nav aria-label="Primary" className="flex-1 space-y-1 px-3 py-3">
          <NavLinks nav={nav} pathname={pathname} />
        </nav>

        <div className="border-t border-slate-200 px-4 py-3">
          {me && (
            <>
              <p className="truncate text-sm font-medium text-slate-700">{me.name}</p>
              <p className="truncate text-xs text-slate-500">{me.email}</p>
              {me.status !== "active" && (
                <p className="mt-1">
                  {/* Surface a disabled/locked account status instead of hiding it. */}
                  <Badge tone="amber">
                    <span className="sr-only">Account status: </span>
                    {me.status}
                  </Badge>
                </p>
              )}
            </>
          )}
          <SignOutButton className="mt-2" />
        </div>
      </aside>

      <main id="main-content" tabIndex={-1} className="flex-1 overflow-y-auto p-4 md:p-6">
        {children}
      </main>
    </div>
  );
}

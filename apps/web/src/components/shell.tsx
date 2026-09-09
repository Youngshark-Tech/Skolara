"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useSession } from "@/lib/session";
import { visibleNav } from "@/lib/permissions";
import { Badge } from "@/components/ui";

/**
 * Role-aware application shell: navigation is filtered by the session's
 * server-resolved permissions; the school switcher re-roots the tenant
 * context (X-School-ID) for every API call.
 */
export function Shell({ children }: { children: React.ReactNode }) {
  const { me, memberships, activeSchoolId, loading, logout, switchSchool } = useSession();
  const pathname = usePathname();
  const nav = visibleNav(me);
  const activeMembership = memberships.find((m) => m.schoolId === activeSchoolId);

  if (loading) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-slate-50">
        <p className="text-sm text-slate-500">Loading Skolara…</p>
      </div>
    );
  }

  return (
    <div className="flex min-h-screen bg-slate-50">
      <aside className="flex w-60 flex-col border-r border-slate-200 bg-white">
        <div className="border-b border-slate-200 px-5 py-4">
          <p className="text-lg font-bold text-primary">Skolara</p>
          <p className="text-xs text-slate-400">The Operating System for Schools</p>
        </div>

        <div className="border-b border-slate-200 px-4 py-3">
          <label className="mb-1 block text-[11px] font-medium uppercase tracking-wide text-slate-400">
            Active school
          </label>
          <select
            className="w-full rounded-md border border-slate-300 px-2 py-1 text-sm"
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

        <nav className="flex-1 space-y-1 px-3 py-3">
          {nav.map((entry) => {
            const active = pathname === entry.href;
            return (
              <Link
                key={entry.href}
                href={entry.href}
                className={`block rounded-md px-3 py-2 text-sm font-medium transition ${
                  active ? "bg-primary text-white" : "text-slate-600 hover:bg-slate-100"
                }`}
              >
                {entry.label}
              </Link>
            );
          })}
        </nav>

        <div className="border-t border-slate-200 px-4 py-3">
          {me && (
            <>
              <p className="truncate text-sm font-medium text-slate-700">{me.name}</p>
              <p className="truncate text-xs text-slate-400">{me.email}</p>
            </>
          )}
          <button
            onClick={() => void logout()}
            className="mt-2 text-xs font-medium text-red-600 hover:text-red-700"
          >
            Sign out
          </button>
        </div>
      </aside>

      <main className="flex-1 overflow-y-auto p-6">{children}</main>
    </div>
  );
}

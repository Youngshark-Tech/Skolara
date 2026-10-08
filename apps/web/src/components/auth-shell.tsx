import type { ReactNode } from "react";

/**
 * Shared authentication shell (#131): a two-panel split screen from 1024px —
 * brand/story panel + form panel — collapsing to a stacked single column with
 * a compact brand header below it. Used by BOTH /login and /signup so the two
 * surfaces share one visual language (single design system, token-driven).
 *
 * Accessibility: decorative brand panel is aria-hidden (the form panel is the
 * only content); page-level headings remain inside each form's h1.
 */
export function AuthShell({ children }: { children: ReactNode }) {
  return (
    <div className="flex min-h-screen bg-white">
      {/* Brand panel — decorative, hidden below 1024px */}
      <aside
        aria-hidden="true"
        className="hidden w-[44%] max-w-2xl flex-col justify-between bg-gradient-to-b from-primary-dark via-primary to-primary-dark p-10 text-white lg:flex"
      >
        <span className="text-2xl font-bold tracking-tight">Skolara</span>
        <div>
          <p className="max-w-md text-2xl font-semibold leading-snug">
            One system for admissions, attendance, academics, and school finance.
          </p>
          <ul className="mt-6 space-y-2 text-sm text-blue-100">
            <li>Multi-tenant by design — your data stays yours</li>
            <li>Ledger-first finance — every shilling accounted for</li>
            <li>Audit trail on every write</li>
          </ul>
        </div>
        <p className="text-xs text-blue-200">Secure by default · Server-side authorization</p>
      </aside>

      {/* Form panel */}
      <main className="flex flex-1 flex-col">
        {/* Compact brand header for the stacked (mobile/tablet) layout */}
        <div className="border-b border-slate-200 p-4 lg:hidden">
          <span className="text-lg font-bold tracking-tight">Skolara</span>
        </div>
        <div className="flex flex-1 items-center justify-center px-4 py-10">
          <div className="w-full max-w-md">{children}</div>
        </div>
      </main>
    </div>
  );
}

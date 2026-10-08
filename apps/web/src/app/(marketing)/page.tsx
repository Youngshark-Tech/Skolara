import type { Metadata } from "next";
import { HeroAuthActions } from "./hero-header";

/**
 * Public landing surface (#129): the first thing any visitor sees, with
 * Log in / Sign up CTAs. Lives in the (marketing) route group so it never
 * mounts the authenticated app shell; the proxy guard treats "/" as public
 * (lib/auth-cookies.ts) and the dashboard moved to /dashboard.
 */

export const metadata: Metadata = {
  title: { absolute: "Skolara — The Intelligent Operating System for Schools" },
  description:
    "Admissions, enrollment, attendance, academics, and ledger-first finance for schools — multi-tenant, secure by default, and fast enough for the front office.",
  openGraph: {
    title: "Skolara — The Intelligent Operating System for Schools",
    description:
      "One system for admissions, enrollment, attendance, academics, and school finance. Built multi-tenant, secure by default.",
    type: "website",
  },
};

const FEATURES = [
  {
    title: "Students & enrollment",
    body: "Global learner identities, guardians, and a guarded admission-to-graduation lifecycle that never loses a student's history.",
  },
  {
    title: "Attendance",
    body: "Fast daily rosters per class group with a clear record of who marked what, when — and why any change was made.",
  },
  {
    title: "Academics",
    body: "Academic years, terms, subjects, and class rosters that mirror how your school actually runs, campus by campus.",
  },
  {
    title: "Finance",
    body: "Ledger-first invoicing and payments: every shilling accounted for with immutable double-entry postings behind each balance.",
  },
] as const;

export default function LandingPage() {
  return (
    <div className="min-h-screen bg-white text-ink">
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:absolute focus:left-4 focus:top-4 focus:z-50 focus:rounded-md focus:bg-primary focus:px-3 focus:py-2 focus:text-sm focus:text-white"
      >
        Skip to content
      </a>

      <header className="sticky top-0 border-b border-slate-200 bg-white/90 backdrop-blur">
        <div className="mx-auto flex h-14 max-w-6xl items-center justify-between px-4 sm:px-6">
          <span className="text-lg font-bold tracking-tight">Skolara</span>
          <HeroAuthActions />
        </div>
      </header>

      <main id="main">
        {/* Hero */}
        <section className="border-b border-slate-200 bg-gradient-to-b from-blue-50 via-white to-white">
          <div className="mx-auto max-w-6xl px-4 py-16 sm:px-6 sm:py-24">
            <p className="inline-flex rounded-full border border-blue-200 bg-blue-50 px-3 py-1 text-xs font-medium text-primary-dark">
              School management platform
            </p>
            <h1 className="mt-4 max-w-3xl text-4xl font-bold leading-tight tracking-tight sm:text-5xl">
              Run your whole school from one place.
            </h1>
            <p className="mt-4 max-w-2xl text-base leading-relaxed text-slate-600 sm:text-lg">
              Skolara brings admissions, enrollment, attendance, academics, and
              finance together — with tenant isolation and server-side
              authorization built in from day one, not bolted on.
            </p>
            <div className="mt-8 flex flex-col gap-3 sm:flex-row">
              <HeroAuthActions />
            </div>
            <p className="mt-6 text-sm text-slate-500">
              Multi-tenant by design · Ledger-first finance · Audit trail on every write
            </p>
          </div>
        </section>

        {/* Features */}
        <section aria-labelledby="features-heading" className="mx-auto max-w-6xl px-4 py-16 sm:px-6">
          <h2 id="features-heading" className="text-2xl font-bold tracking-tight sm:text-3xl">
            Everything the front office, staffroom, and bursar need
          </h2>
          <p className="mt-2 max-w-2xl text-slate-600">
            Four bounded contexts that share one source of truth — so data
            entered once is right everywhere.
          </p>
          <div className="mt-10 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            {FEATURES.map((f) => (
              <div
                key={f.title}
                className="rounded-lg border border-slate-200 bg-white p-5 shadow-xs"
              >
                <h3 className="font-semibold">{f.title}</h3>
                <p className="mt-2 text-sm leading-relaxed text-slate-600">{f.body}</p>
              </div>
            ))}
          </div>
        </section>

        {/* Closing CTA */}
        <section className="border-t border-slate-200 bg-slate-50">
          <div className="mx-auto flex max-w-6xl flex-col items-start gap-4 px-4 py-12 sm:px-6 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <h2 className="text-xl font-bold tracking-tight sm:text-2xl">
                Ready to see your school in it?
              </h2>
              <p className="mt-1 text-sm text-slate-600">
                Create your workspace in under a minute — no credit card, no sales call.
              </p>
            </div>
            <HeroAuthActions />
          </div>
        </section>
      </main>

      <footer className="border-t border-slate-200">
        <div className="mx-auto flex max-w-6xl flex-col gap-2 px-4 py-8 text-sm text-slate-500 sm:flex-row sm:items-center sm:justify-between sm:px-6">
          <span>© {new Date().getFullYear()} Skolara</span>
          <nav aria-label="Footer">
            <ul className="flex gap-4">
              <li>
                <a className="hover:text-slate-700" href="/login">
                  Log in
                </a>
              </li>
              <li>
                <a className="hover:text-slate-700" href="/signup">
                  Sign up
                </a>
              </li>
            </ul>
          </nav>
        </div>
      </footer>
    </div>
  );
}

"use client";

import Link from "next/link";
import type { ReactNode } from "react";

/**
 * Shared button style system (#129): one source of truth for Button and
 * ButtonLink so navigation and action controls share a single visual
 * language. Variants: primary (filled), secondary (bordered), ghost (text).
 */
export type ButtonVariant = "primary" | "secondary" | "ghost";

export function buttonClass(variant: ButtonVariant = "primary", extra = ""): string {
  const styles: Record<ButtonVariant, string> = {
    primary: "bg-primary text-white hover:bg-primary-dark",
    secondary: "border border-slate-300 bg-white text-slate-700 hover:bg-slate-50",
    ghost: "text-slate-600 hover:bg-slate-100",
  };
  return `inline-flex items-center justify-center rounded-md px-3 py-1.5 text-sm font-medium transition focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary disabled:cursor-not-allowed disabled:opacity-50 ${styles[variant]} ${extra}`;
}

export function Card({
  title,
  action,
  children,
}: {
  title?: string;
  action?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="rounded-lg border border-slate-200 bg-white p-5 shadow-xs">
      {(title || action) && (
        <header className="mb-4 flex items-center justify-between">
          {title && <h2 className="text-sm font-semibold text-slate-700">{title}</h2>}
          {action}
        </header>
      )}
      {children}
    </section>
  );
}

export function Stat({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="rounded-lg border border-slate-200 bg-white p-5 shadow-xs">
      <p className="text-xs font-medium uppercase tracking-wide text-slate-500">{label}</p>
      <p className="mt-1 text-2xl font-semibold text-ink">{value}</p>
      {/* slate-500 on white is ≥4.5:1 (slate-400 ≈2.9:1 failed WCAG AA for text). */}
      {hint && <p className="mt-1 text-xs text-slate-500">{hint}</p>}
    </div>
  );
}

export function Button({
  children,
  variant = "primary",
  type = "button",
  disabled,
  onClick,
  className = "",
  ...rest
}: React.ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: ButtonVariant;
}) {
  return (
    <button
      {...rest}
      type={type}
      disabled={disabled}
      onClick={onClick}
      className={buttonClass(variant, className)}
    >
      {children}
    </button>
  );
}

export function Input(props: React.InputHTMLAttributes<HTMLInputElement>) {
  const { className = "", ...rest } = props;
  return (
    <input
      {...rest}
      className={`w-full rounded-md border border-slate-300 px-3 py-1.5 text-sm outline-hidden focus:border-primary focus:ring-1 focus:ring-primary ${className}`}
    />
  );
}

/**
 * Programmatic form-control label (#57 a11y): pair with the control via
 * htmlFor/id — placeholder-only inputs are never an accessible name.
 */
export function Label({
  htmlFor,
  children,
  className = "mb-1 block text-xs font-medium text-slate-600",
}: {
  htmlFor?: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <label htmlFor={htmlFor} className={className}>
      {children}
    </label>
  );
}

export function Badge({ tone, children }: { tone: "green" | "blue" | "amber" | "red" | "slate"; children: ReactNode }) {
  const tones: Record<string, string> = {
    green: "bg-emerald-50 text-emerald-700",
    blue: "bg-blue-50 text-blue-700",
    amber: "bg-amber-50 text-amber-700",
    red: "bg-red-50 text-red-700",
    slate: "bg-slate-100 text-slate-600",
  };
  return (
    <span className={`inline-flex rounded-full px-2 py-0.5 text-xs font-medium ${tones[tone]}`}>
      {children}
    </span>
  );
}

export function ErrorNote({ message }: { message: string | null }) {
  if (!message) return null;
  return (
    <p role="alert" className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700">
      {message}
    </p>
  );
}

/**
 * Polite live-region notice for success outcomes (#57 a11y): screen readers
 * announce it without stealing focus, unlike the old plain <p>.
 */
export function Notice({ children }: { children: ReactNode }) {
  return (
    <p role="status" className="rounded-md bg-emerald-50 px-3 py-2 text-sm text-emerald-700">
      {children}
    </p>
  );
}

/** Decorative loading placeholder (aria-hidden; pair with a live region if needed). */
export function Skeleton({ className = "" }: { className?: string }) {
  return <div aria-hidden="true" className={`animate-pulse rounded-sm bg-slate-200 ${className}`} />;
}

/**
 * Link styled as a button — use for navigations styled as CTAs (hero, auth
 * pages). A real <a> keeps semantics (middle-click, copy link, screen-reader
 * announcements) instead of a <button> wrapped in an anchor (#129).
 */
export function ButtonLink({
  href,
  variant = "primary",
  className = "",
  children,
  ...rest
}: React.ComponentProps<typeof Link> & { variant?: ButtonVariant }) {
  return (
    <Link href={href} className={buttonClass(variant, className)} {...rest}>
      {children}
    </Link>
  );
}

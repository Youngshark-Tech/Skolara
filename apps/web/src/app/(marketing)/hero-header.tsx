"use client";

import { useSession } from "@/lib/session";
import { ButtonLink } from "@/components/ui";

/**
 * Hero header auth area (#129): shows Log in / Sign up CTAs for guests and an
 * "Open workspace" CTA once a session exists. Rendered inside the root
 * SessionProvider, so it observes the same boot/silent-refresh lifecycle as
 * the app shell — no extra API surface, no cookie sniffing.
 */
export function HeroAuthActions() {
  const { me, loading } = useSession();

  if (loading) {
    // Width-stable placeholder avoids a layout jump when the boot resolves.
    return <div className="h-9 w-40 animate-pulse rounded-md bg-slate-200" aria-hidden="true" />;
  }

  if (me) {
    return (
      <ButtonLink href="/dashboard">Open workspace</ButtonLink>
    );
  }

  return (
    <div className="flex items-center gap-2">
      <ButtonLink href="/login" variant="secondary">
        Log in
      </ButtonLink>
      <ButtonLink href="/signup">Sign up</ButtonLink>
    </div>
  );
}

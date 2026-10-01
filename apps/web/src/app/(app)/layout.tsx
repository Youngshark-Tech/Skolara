"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useSession } from "@/lib/session";
import { Shell } from "@/components/shell";
import { Button } from "@/components/ui";

/**
 * Auth gate for the application shell: unauthenticated visitors are bounced
 * to /login; a bootstrap network failure shows an explicit retry state
 * (never a silent logout); authenticated users get the role-aware shell.
 */
export default function AuthedLayout({ children }: { children: React.ReactNode }) {
  const { me, loading, error, reload } = useSession();
  const router = useRouter();

  useEffect(() => {
    if (!loading && !me && !error) router.replace("/login");
  }, [loading, me, error, router]);

  if (error) {
    return (
      <div className="flex min-h-screen flex-col items-center justify-center gap-4 bg-slate-50 px-4">
        <p className="max-w-sm text-center text-sm text-slate-600">{error}</p>
        <Button onClick={() => void reload()}>Try again</Button>
      </div>
    );
  }

  if (loading || !me) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-slate-50">
        <p className="text-sm text-slate-400">Redirecting…</p>
      </div>
    );
  }

  return <Shell>{children}</Shell>;
}

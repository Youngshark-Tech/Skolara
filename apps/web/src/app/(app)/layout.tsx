"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useSession } from "@/lib/session";
import { Shell } from "@/components/shell";

/**
 * Auth gate for the application shell: unauthenticated visitors are bounced
 * to /login; authenticated users get the role-aware shell.
 */
export default function AuthedLayout({ children }: { children: React.ReactNode }) {
  const { me, loading } = useSession();
  const router = useRouter();

  useEffect(() => {
    if (!loading && !me) router.replace("/login");
  }, [loading, me, router]);

  if (loading || !me) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-slate-50">
        <p className="text-sm text-slate-400">Redirecting…</p>
      </div>
    );
  }

  return <Shell>{children}</Shell>;
}

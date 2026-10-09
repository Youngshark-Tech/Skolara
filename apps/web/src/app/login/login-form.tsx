"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { useSession } from "@/lib/session";
import { safeNextPath } from "@/lib/next-path";
import { Button, ErrorNote, Input, Label } from "@/components/ui";
import { AuthShell } from "@/components/auth-shell";
import { authBypassEnabled, bypassCredentials, mockDataEnabled } from "@/lib/auth-bypass";

/**
 * Login surface: exchanges email+password for a session (refresh cookie),
 * then returns the user to the sanitized `?next=` target (#57).
 *
 * Open-access mode (#141): while NEXT_PUBLIC_AUTH_BYPASS=true the session
 * provider usually signs the visitor in during boot. If boot settled logged
 * out anyway (e.g. the demo seed is not enabled on the API), this form makes
 * ONE automatic retry, then surfaces the real API error next to the form so
 * the deployment stays diagnosable — never a silent failure or a loop.
 */
export function LoginForm({ nextParam }: { nextParam: string | null }) {
  const { me, loading, login } = useSession();
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const bypassAttempted = useRef(false);

  // Sanitize ONCE at the boundary; components below only ever see a safe path.
  // Default destination is the workspace dashboard: "/" is now the public
  // landing page, so a signed-in user belongs behind the app shell (#129).
  const nextPath = safeNextPath(nextParam) ?? "/dashboard";

  useEffect(() => {
    if (me) router.replace(nextPath);
  }, [me, nextPath, router]);

  useEffect(() => {
    if (!authBypassEnabled() || bypassAttempted.current) return;
    if (loading || me) return; // boot still working, or already signed in
    bypassAttempted.current = true;
    // Deferred one tick: the attempt's state updates must never run
    // synchronously inside the effect (react-hooks v6 set-state-in-effect,
    // same rule family as #102/#119).
    void Promise.resolve().then(async () => {
      const creds = bypassCredentials();
      setBusy(true);
      try {
        await login(creds.email, creds.password);
        router.replace(nextPath);
      } catch (err) {
        setError(
          err instanceof Error
            ? `Automatic demo sign-in failed: ${err.message}`
            : "Automatic demo sign-in failed — sign in manually below.",
        );
      } finally {
        setBusy(false);
      }
    });
  }, [loading, me, login, nextPath, router]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await login(email, password);
      router.replace(nextPath);
    } catch (err) {
      setError(err instanceof Error ? err.message : "login failed");
    } finally {
      setBusy(false);
    }
  };

  const bypass = authBypassEnabled();
  const mock = mockDataEnabled();

  return (
    <AuthShell>
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Sign in to your workspace</h1>
        <p className="mt-1 text-sm text-slate-500">
          Welcome back — enter your school account details.
        </p>
        {bypass && (
          <p
            role="note"
            className="mt-4 rounded-md border border-blue-200 bg-blue-50 px-3 py-2 text-xs leading-relaxed text-blue-900"
          >
            {mock ? (
              <>
                Demo mode is active: authentication is disabled and this workspace runs on
                sample data — nothing you do here reaches a real database. Any email and
                password will sign you in.
              </>
            ) : (
              <>
                Demo mode is active: authentication is temporarily disabled on this
                deployment and visitors are signed in automatically as{" "}
                <span className="font-mono">{bypassCredentials().email}</span>. If that
                fails, the demo dataset is not seeded — enable{" "}
                <span className="font-mono">SKOLARA_DEMO_SEED=true</span> on the API and
                redeploy, or sign in manually below.
              </>
            )}
          </p>
        )}
        <form className="mt-6 space-y-4" onSubmit={submit}>
          <div>
            <Label htmlFor="login-email">Email</Label>
            <Input
              id="login-email"
              type="email"
              autoComplete="email"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="you@school.example"
            />
          </div>
          <div>
            <Label htmlFor="login-password">Password</Label>
            <Input
              id="login-password"
              type="password"
              autoComplete="current-password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="••••••••"
            />
          </div>
          <ErrorNote message={error} />
          <Button type="submit" disabled={busy} className="w-full">
            {busy ? "Signing in…" : "Sign in"}
          </Button>
        </form>
        <p className="mt-6 text-sm text-slate-600">
          New to Skolara?{" "}
          <a href="/signup" className="font-medium text-primary hover:underline">
            Create your school workspace
          </a>
        </p>
      </div>
    </AuthShell>
  );
}

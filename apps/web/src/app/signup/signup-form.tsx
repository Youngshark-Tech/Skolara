"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { useSession } from "@/lib/session";
import { safeNextPath } from "@/lib/next-path";
import { apiFetch, ApiError } from "@/lib/api";
import { Button, ErrorNote, Input, Label } from "@/components/ui";
import { AuthShell } from "@/components/auth-shell";

/**
 * Signup surface (#130): provisions the school workspace + admin account via
 * the public POST /api/v1/auth/signup, then completes onboarding through the
 * STANDARD login flow (single cookie/session code path — the signup endpoint
 * deliberately issues no session).
 */
export function SignupForm({ nextParam }: { nextParam: string | null }) {
  const { me, login } = useSession();
  const router = useRouter();
  const [schoolName, setSchoolName] = useState("");
  const [adminName, setAdminName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const nextPath = safeNextPath(nextParam) ?? "/dashboard";

  useEffect(() => {
    if (me) router.replace(nextPath);
  }, [me, nextPath, router]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (password !== confirm) {
      setError("Passwords do not match.");
      return;
    }
    setBusy(true);
    try {
      await apiFetch("/api/v1/auth/signup", {
        method: "POST",
        noRetry: true,
        body: { schoolName, adminName, email, password },
      });
      // Provisioned. Complete onboarding through the standard login flow —
      // this sets the refresh cookie and boots the session exactly like the
      // login page (one session-issuing code path).
      await login(email, password);
      router.replace(nextPath);
    } catch (err) {
      if (err instanceof ApiError && err.code === "email_taken") {
        setError("That email is already registered — try signing in instead.");
      } else if (err instanceof ApiError) {
        setError(err.message);
      } else {
        setError("Could not create the workspace — check your connection and try again.");
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <AuthShell>
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Create your school workspace</h1>
        <p className="mt-1 text-sm text-slate-500">
          One account for you, one workspace for your school — ready in under a minute.
        </p>
        <form className="mt-6 space-y-4" onSubmit={submit}>
          <div>
            <Label htmlFor="signup-school">School name</Label>
            <Input
              id="signup-school"
              type="text"
              autoComplete="organization"
              required
              minLength={2}
              maxLength={128}
              value={schoolName}
              onChange={(e) => setSchoolName(e.target.value)}
              placeholder="Riverside High School"
            />
          </div>
          <div>
            <Label htmlFor="signup-name">Your name</Label>
            <Input
              id="signup-name"
              type="text"
              autoComplete="name"
              required
              maxLength={120}
              value={adminName}
              onChange={(e) => setAdminName(e.target.value)}
              placeholder="Jane Mukami"
            />
          </div>
          <div>
            <Label htmlFor="signup-email">Work email</Label>
            <Input
              id="signup-email"
              type="email"
              autoComplete="email"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="you@school.example"
            />
          </div>
          <div>
            <Label htmlFor="signup-password">Password</Label>
            <Input
              id="signup-password"
              type="password"
              autoComplete="new-password"
              required
              minLength={8}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="At least 8 characters"
            />
          </div>
          <div>
            <Label htmlFor="signup-confirm">Confirm password</Label>
            <Input
              id="signup-confirm"
              type="password"
              autoComplete="new-password"
              required
              minLength={8}
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
              placeholder="Repeat the password"
            />
          </div>
          <ErrorNote message={error} />
          <Button type="submit" disabled={busy} className="w-full">
            {busy ? "Creating workspace…" : "Create workspace"}
          </Button>
        </form>
        <p className="mt-6 text-sm text-slate-600">
          Already have an account?{" "}
          <Link href="/login" className="font-medium text-primary hover:underline">
            Log in
          </Link>
        </p>
      </div>
    </AuthShell>
  );
}

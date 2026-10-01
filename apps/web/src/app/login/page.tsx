import type { Metadata } from "next";
import { LoginForm } from "./login-form";

export const metadata: Metadata = { title: "Sign in" };

/**
 * Server surface for the login form: the only job here is to hand the raw
 * `?next=` search param to the client form, which sanitizes it before use
 * (see lib/next-path.ts — never redirect on an unsanitized value).
 */
export default async function LoginPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const params = await searchParams;
  const next = params.next;
  return <LoginForm nextParam={typeof next === "string" ? next : null} />;
}

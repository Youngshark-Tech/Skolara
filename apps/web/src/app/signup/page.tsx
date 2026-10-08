import type { Metadata } from "next";
import { SignupForm } from "./signup-form";

export const metadata: Metadata = { title: "Create your school workspace" };

/**
 * Public signup surface (issue #130): provisions a school workspace + admin
 * account, then signs the new admin straight into their workspace. The
 * `?next=` param is honored after signup the same way the login form honors
 * it (sanitized client-side — lib/next-path.ts).
 */
export default async function SignupPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const params = await searchParams;
  const next = params.next;
  return <SignupForm nextParam={typeof next === "string" ? next : null} />;
}

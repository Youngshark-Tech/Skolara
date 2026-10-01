import type { Metadata } from "next";
import Link from "next/link";

export const metadata: Metadata = { title: "Page not found" };

/** 404 inside the app shell: offer the primary surfaces back (#57). */
export default function AppNotFound() {
  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold">Page not found</h1>
      <p className="text-sm text-slate-600">
        That page does not exist in this workspace. It may have moved, or the link was wrong.
      </p>
      <Link
        href="/"
        className="inline-block rounded-md bg-primary px-3 py-1.5 text-sm font-medium text-white hover:bg-primary-dark focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
      >
        Back to Command Center
      </Link>
    </div>
  );
}

"use client";

/**
 * Root error boundary of last resort (#57): when the root layout itself
 * throws, this component must render its own <html>/<body>. Kept dependency-
 * light on purpose (no context consumers — the provider may be the failure).
 */
export default function GlobalError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  return (
    <html lang="en">
      <body className="flex min-h-screen items-center justify-center bg-slate-50 p-6 antialiased">
        <div className="max-w-md text-center">
          <h1 className="text-xl font-semibold text-ink">Application error</h1>
          <p className="mt-2 text-sm text-slate-600">
            Skolara could not render. Reload the page; if the problem persists, sign in again.
          </p>
          {error.digest && (
            <p className="mt-1 text-xs text-slate-500">Error reference: {error.digest}</p>
          )}
          <button
            type="button"
            onClick={reset}
            className="mt-4 inline-flex items-center justify-center rounded-md bg-primary px-3 py-1.5 text-sm font-medium text-white hover:bg-primary-dark focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
          >
            Try again
          </button>
        </div>
      </body>
    </html>
  );
}

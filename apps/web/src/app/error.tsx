"use client";

import { Button } from "@/components/ui";

/**
 * Route-segment error boundary (#57): render failures get an explicit,
 * recoverable state instead of a blank screen. Errors must never look like
 * empty data.
 */
export default function ErrorPage({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  return (
    <div className="flex min-h-[60vh] flex-col items-center justify-center gap-4 px-4 text-center">
      <h1 className="text-xl font-semibold">Something went wrong</h1>
      <p className="max-w-md text-sm text-slate-600">
        An unexpected error occurred while rendering this page. Your session and data are
        unaffected — try again.
      </p>
      {error.digest && <p className="text-xs text-slate-500">Error reference: {error.digest}</p>}
      <Button onClick={reset}>Try again</Button>
    </div>
  );
}

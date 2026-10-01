"use client";

import { Button } from "@/components/ui";

/** App-shell error boundary (#57): failures inside the shell keep their context. */
export default function AppError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  return (
    <div className="flex min-h-[60vh] flex-col items-center justify-center gap-4 px-4 text-center">
      <h1 className="text-xl font-semibold">This section failed to load</h1>
      <p className="max-w-md text-sm text-slate-600">
        Something went wrong while rendering this workspace. Try again — your data is safe.
      </p>
      {error.digest && <p className="text-xs text-slate-500">Error reference: {error.digest}</p>}
      <Button onClick={reset}>Try again</Button>
    </div>
  );
}

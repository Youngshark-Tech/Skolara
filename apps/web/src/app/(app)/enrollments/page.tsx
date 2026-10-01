"use client";

import { Suspense } from "react";
import { Skeleton } from "@/components/ui";
import { EnrollmentsView } from "./enrollments-view";

function ViewFallback() {
  return (
    <div role="status" aria-label="Loading enrollments" className="space-y-4">
      <Skeleton className="h-8 w-48" />
      <Skeleton className="h-10 w-full" />
      <Skeleton className="h-72 w-full" />
    </div>
  );
}

/**
 * Enrollments surface (#58). The `?enroll=` deep link is read through
 * useSearchParams, which requires a Suspense boundary to prerender.
 */
export default function EnrollmentsPage() {
  return (
    <Suspense fallback={<ViewFallback />}>
      <EnrollmentsView />
    </Suspense>
  );
}

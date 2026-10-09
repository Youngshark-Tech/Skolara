"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { apiFetch } from "@/lib/api";
import { useSession } from "@/lib/session";
import { can } from "@/lib/permissions";
import { Badge, Card, ErrorNote } from "@/components/ui";
import type {
  AcademicYear,
  ClassGroup,
  EnrollmentPage,
} from "@/types/api";

const BADGE_TONE: Record<AcademicYear["status"], "green" | "blue" | "slate"> = {
  active: "green",
  planning: "blue",
  closed: "slate",
};

function isAbortError(err: unknown): boolean {
  return err instanceof Error && err.name === "AbortError";
}

/**
 * Academics surface (#188): the school's academic calendar structure —
 * academic years and class groups with LIVE enrollment counts.
 *
 * Counts are derived from `GET /api/v1/enrollments` (not hard-coded), so a
 * class grows as admissions happen elsewhere in the demo session — academics
 * and enrollments share one store, mirroring how the connected API shares one
 * database (contract parity per #142).
 */
export default function AcademicsPage() {
  const { me, activeSchoolId } = useSession();
  const allowed = can(me, "academics.read") || can(me, "academics.manage");

  const [years, setYears] = useState<AcademicYear[] | null>(null);
  const [classes, setClasses] = useState<ClassGroup[] | null>(null);
  const [enrollmentCounts, setEnrollmentCounts] = useState<Map<string, number>>(new Map());
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(
    async (signal?: AbortSignal) => {
      if (!activeSchoolId || !can(me, "academics.read")) return;
      try {
        const [yearsRes, classesRes, enrollmentsRes] = await Promise.all([
          apiFetch<{ years?: AcademicYear[] } | AcademicYear[]>("/api/v1/academic-years", { signal }),
          apiFetch<{ classes?: ClassGroup[] } | ClassGroup[]>("/api/v1/classes", { signal }),
          apiFetch<EnrollmentPage>("/api/v1/enrollments?limit=100", { signal }),
        ]);
        // Tolerate both the bare-array and envelope shapes so the page works
        // against the demo transport today and the live API tomorrow.
        const yearsList = Array.isArray(yearsRes) ? yearsRes : (yearsRes.years ?? []);
        const classesList = Array.isArray(classesRes) ? classesRes : (classesRes.classes ?? []);
        const counts = new Map<string, number>();
        for (const e of enrollmentsRes.enrollments) {
          if (!e.classGroupId) continue;
          counts.set(e.classGroupId, (counts.get(e.classGroupId) ?? 0) + 1);
        }
        setYears(yearsList);
        setClasses(classesList);
        setEnrollmentCounts(counts);
        setError(null);
      } catch (err) {
        if (isAbortError(err)) return; // a newer request superseded this one
        setError(err instanceof Error ? err.message : "failed to load academics data");
      } finally {
        setLoading(false);
      }
    },
    [activeSchoolId, me],
  );

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  const activeYearName = useMemo(
    () => years?.find((y) => y.status === "active")?.name ?? null,
    [years],
  );

  if (!allowed) {
    return (
      <div className="space-y-4">
        <h1 className="text-xl font-semibold">Academics</h1>
        <ErrorNote message="Your role does not include academics access." />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-xl font-semibold">Academics</h1>
        <p className="text-sm text-slate-500">
          Academic years and class groups at the active school
          {activeYearName ? ` · ${activeYearName} in session` : ""}
        </p>
      </header>

      <ErrorNote message={error} />

      {loading && (
        <p className="text-sm text-slate-500" aria-live="polite">
          Loading academics data…
        </p>
      )}

      {!loading && years && (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          {years.map((y) => (
            <Card
              key={y.id}
              title={y.name}
              action={<Badge tone={BADGE_TONE[y.status]}>{y.status}</Badge>}
            >
              <p className="text-sm text-slate-600">
                {y.startDate} → {y.endDate}
              </p>
              <p className="mt-1 text-xs text-slate-500">
                {y.status === "active"
                  ? "Current academic year"
                  : y.status === "planning"
                    ? "Not started yet"
                    : "Archived"}
              </p>
            </Card>
          ))}
        </div>
      )}

      {!loading && classes && (
        <Card title="Class groups">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-slate-200 text-left text-xs uppercase tracking-wide text-slate-500">
                  <th scope="col" className="pb-2">Class</th>
                  <th scope="col" className="pb-2">Academic year</th>
                  <th scope="col" className="pb-2">Enrolled</th>
                </tr>
              </thead>
              <tbody>
                {classes.map((c) => {
                  const year = years?.find((y) => y.id === c.academicYearId);
                  return (
                    <tr key={c.id} className="border-b border-slate-100 last:border-0">
                      <td className="py-2 font-medium">{c.name}</td>
                      <td className="py-2 text-slate-500">{year?.name ?? c.academicYearId}</td>
                      <td className="py-2">
                        <Badge tone="blue">{enrollmentCounts.get(c.id) ?? 0}</Badge>
                      </td>
                    </tr>
                  );
                })}
                {classes.length === 0 && (
                  <tr>
                    <td colSpan={3} className="py-6 text-center text-slate-500">
                      No class groups configured yet
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </Card>
      )}
    </div>
  );
}

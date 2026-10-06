"use client";

import { useCallback, useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import { apiFetch } from "@/lib/api";
import { useSession } from "@/lib/session";
import { can } from "@/lib/permissions";
import {
  ALL_STATES,
  STATUS_LABELS,
  isTerminal,
  needsConfirmation,
  nextStates,
  statusTone,
} from "@/lib/enrollment-states";
import { Badge, Button, Card, ErrorNote, Notice, Skeleton } from "@/components/ui";
import type {
  Enrollment,
  EnrollmentPage,
  EnrollmentStatus,
  LearnerPage,
} from "@/types/api";
import { EnrollPanel } from "./enroll-panel";

const PAGE_SIZE = 20;
/** Learner names are a display nicety — bound the roster fan-out (3×100). */
const LEARNER_NAME_PAGES = 3;

function shortId(id: string): string {
  return id.slice(0, 8);
}

function formatDate(iso: string | null | undefined): string {
  if (!iso) return "—";
  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? "—" : date.toLocaleDateString();
}

function isAbortError(err: unknown): boolean {
  return err instanceof Error && err.name === "AbortError";
}

function FilterChip({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={`rounded-full px-3 py-1 text-xs font-medium transition focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary ${
        active
          ? "bg-primary text-white"
          : "border border-slate-300 bg-white text-slate-700 hover:bg-slate-100"
      }`}
    >
      {children}
    </button>
  );
}

/**
 * Enrollments surface (#58): the admit story. List is school-scoped
 * (GET /api/v1/enrollments, student.read), transitions POST to
 * /enrollments/{id}/transition (student.manage) offering ONLY the legal next
 * states mirrored from students/domain.go, with confirmation before
 * career-end moves. The server remains authoritative throughout.
 */
export function EnrollmentsView() {
  const { me, activeSchoolId } = useSession();
  const searchParams = useSearchParams();
  const enrollParam = searchParams.get("enroll");

  const canRead = can(me, "student.read");
  const canManage = can(me, "student.manage");

  const [pageData, setPageData] = useState<EnrollmentPage | null>(null);
  const [statusFilter, setStatusFilter] = useState<EnrollmentStatus | "all">("all");
  const [offset, setOffset] = useState(0);
  const [listError, setListError] = useState<string | null>(null);
  const [listLoading, setListLoading] = useState(true);
  const [notice, setNotice] = useState<string | null>(null);
  const [confirmMove, setConfirmMove] = useState<{ id: string; to: EnrollmentStatus } | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [learnerNames, setLearnerNames] = useState<Record<string, string>>({});
  const [enrollOpen, setEnrollOpen] = useState(false);

  const loadList = useCallback(
    async (signal?: AbortSignal) => {
      if (!activeSchoolId || !can(me, "student.read")) return;
      setListLoading(true);
      try {
        const params = new URLSearchParams({ limit: String(PAGE_SIZE), offset: String(offset) });
        if (statusFilter !== "all") params.set("status", statusFilter);
        let headerTotal: number | null = null;
        const data = await apiFetch<EnrollmentPage>(
          `/api/v1/enrollments?${params.toString()}`,
          {
            signal,
            // Pagination honors X-Total-Count (authoritative); the body total
            // is only a fallback for clients/caches that strip the header.
            onResponse: (res) => {
              const raw = res.headers.get("X-Total-Count");
              if (raw !== null && raw !== "" && !Number.isNaN(Number(raw))) {
                headerTotal = Number(raw);
              }
            },
          },
        );
        setPageData({ ...data, total: headerTotal ?? data.total });
        setListError(null);
      } catch (err) {
        if (isAbortError(err)) return;
        setListError(err instanceof Error ? err.message : "failed to load enrollments");
      } finally {
        setListLoading(false);
      }
    },
    [activeSchoolId, me, offset, statusFilter],
  );

  useEffect(() => {
    const controller = new AbortController();
    void loadList(controller.signal);
    return () => controller.abort();
  }, [loadList]);

  // Resolve learner ids -> display names from the school roster (bounded).
  const loadLearnerNames = useCallback(async () => {
    if (!activeSchoolId || !can(me, "student.read")) return;
    const names: Record<string, string> = {};
    try {
      for (let page = 0; page < LEARNER_NAME_PAGES; page++) {
        const data = await apiFetch<LearnerPage>(
          `/api/v1/learners?limit=100&offset=${page * 100}`,
        );
        for (const learner of data.learners) {
          names[learner.id] = `${learner.firstName} ${learner.lastName}`.trim();
        }
        if ((page + 1) * 100 >= data.total) break;
      }
      setLearnerNames(names);
    } catch {
      // Names are decorative; the table falls back to short learner ids.
    }
  }, [activeSchoolId, me]);

  useEffect(() => {
    void loadLearnerNames();
  }, [loadLearnerNames]);

  // Admit-story deep link: /enrollments?enroll=<learnerId> opens the flow.
  // Render-time adjustment keyed on (param, permission) — behavior-identical
  // to the previous effect INCLUDING the mount case (the sentinel `null`
  // makes the first render always "change", so a deep link present at load
  // opens the dialog once the session resolves canManage), while satisfying
  // react-hooks v6's set-state-in-effect rule (#102 Next 16).
  const deepLink = `${enrollParam ?? ""}|${canManage}`;
  const [lastDeepLink, setLastDeepLink] = useState<string | null>(null);
  if (deepLink !== lastDeepLink) {
    setLastDeepLink(deepLink);
    if (enrollParam && canManage) setEnrollOpen(true);
  }

  const performTransition = async (enrollmentId: string, to: EnrollmentStatus) => {
    setBusyId(enrollmentId);
    setListError(null);
    setConfirmMove(null);
    try {
      const updated = await apiFetch<Enrollment>(
        `/api/v1/enrollments/${enrollmentId}/transition`,
        { method: "POST", body: { to } },
      );
      setPageData((prev) =>
        prev
          ? {
              ...prev,
              enrollments: prev.enrollments.map((e) => (e.id === updated.id ? updated : e)),
            }
          : prev,
      );
      setNotice(`Enrollment moved to ${STATUS_LABELS[updated.status]}.`);
    } catch (err) {
      // A 409 here means the mirror drifted from the server — surface it.
      setListError(err instanceof Error ? err.message : "failed to transition enrollment");
    } finally {
      setBusyId(null);
    }
  };

  const requestTransition = (enrollment: Enrollment, to: EnrollmentStatus) => {
    setNotice(null);
    if (needsConfirmation(to)) {
      setConfirmMove({ id: enrollment.id, to });
      return;
    }
    void performTransition(enrollment.id, to);
  };

  if (!canRead && !canManage) {
    return (
      <div className="space-y-4">
        <h1 className="text-xl font-semibold">Enrollments</h1>
        <ErrorNote message="Your role does not include enrollment access." />
      </div>
    );
  }

  const total = pageData?.total ?? 0;
  const enrollments = pageData?.enrollments ?? [];
  const columns = canManage ? 5 : 4;

  return (
    <div className="space-y-6">
      <header className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-xl font-semibold">Enrollments</h1>
          <p className="text-sm text-slate-500">
            The learner lifecycle at this school — applicant to alumni.
          </p>
        </div>
        {canManage && (
          <Button
            aria-expanded={enrollOpen}
            aria-controls="enroll-panel"
            onClick={() => setEnrollOpen((v) => !v)}
          >
            {enrollOpen ? "Close enroll panel" : "Enroll a learner"}
          </Button>
        )}
      </header>

      <ErrorNote message={listError} />
      {notice && <Notice>{notice}</Notice>}

      {canManage && enrollOpen && (
        <div id="enroll-panel">
          <EnrollPanel
            preselectLearnerId={enrollParam}
            onClose={() => setEnrollOpen(false)}
            onEnrolled={(enrollment) => {
              setEnrollOpen(false);
              setNotice(
                `Learner enrolled as ${STATUS_LABELS[enrollment.status]}. Welcome aboard!`,
              );
              void loadList();
              void loadLearnerNames();
            }}
          />
        </div>
      )}

      <div role="group" aria-label="Filter enrollments by status" className="flex flex-wrap gap-2">
        <FilterChip
          active={statusFilter === "all"}
          onClick={() => {
            setStatusFilter("all");
            setOffset(0);
          }}
        >
          All
        </FilterChip>
        {ALL_STATES.map((s) => (
          <FilterChip
            key={s}
            active={statusFilter === s}
            onClick={() => {
              setStatusFilter(s);
              setOffset(0);
            }}
          >
            {STATUS_LABELS[s]}
          </FilterChip>
        ))}
      </div>

      <Card
        title={
          statusFilter === "all"
            ? "All enrollments"
            : `Enrollments · ${STATUS_LABELS[statusFilter]}`
        }
      >
        {listLoading && !pageData ? (
          <div role="status" aria-label="Loading enrollments" className="space-y-2">
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-8 w-2/3" />
          </div>
        ) : (
          <>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-slate-200 text-left text-xs uppercase tracking-wide text-slate-500">
                    <th scope="col" className="pb-2 pr-4">
                      Learner
                    </th>
                    <th scope="col" className="pb-2 pr-4">
                      Status
                    </th>
                    <th scope="col" className="pb-2 pr-4">
                      Started
                    </th>
                    <th scope="col" className="pb-2">
                      Ended
                    </th>
                    {canManage && (
                      <th scope="col" className="pb-2">
                        Move to
                      </th>
                    )}
                  </tr>
                </thead>
                <tbody>
                  {enrollments.map((e) => {
                    const name = learnerNames[e.learnerId];
                    const confirming = confirmMove?.id === e.id ? confirmMove : null;
                    return (
                      <tr key={e.id} className="border-b border-slate-100 last:border-0 align-top">
                        <td className="py-3 pr-4 font-medium">
                          {name ?? `Learner ${shortId(e.learnerId)}`}
                          <span className="block text-xs font-normal text-slate-500">
                            #{shortId(e.learnerId)}
                          </span>
                        </td>
                        <td className="py-3 pr-4">
                          <Badge tone={statusTone(e.status)}>{STATUS_LABELS[e.status]}</Badge>
                        </td>
                        <td className="py-3 pr-4 text-slate-500">{formatDate(e.startedAt)}</td>
                        <td className="py-3 text-slate-500">{formatDate(e.endedAt)}</td>
                        {canManage && (
                          <td className="py-3 pl-4">
                            {confirming ? (
                              <div className="flex flex-col gap-1.5">
                                <p className="text-xs text-slate-600">
                                  Move to <strong>{STATUS_LABELS[confirming.to]}</strong>?{" "}
                                  {isTerminal(confirming.to)
                                    ? "This ends the enrollment and cannot be undone."
                                    : "This is a significant, hard-to-reverse move."}
                                </p>
                                <div className="flex gap-2">
                                  <Button
                                    className="px-2! py-1! text-xs"
                                    disabled={busyId === e.id}
                                    onClick={() => void performTransition(e.id, confirming.to)}
                                  >
                                    Confirm
                                  </Button>
                                  <Button
                                    variant="secondary"
                                    className="px-2! py-1! text-xs"
                                    onClick={() => setConfirmMove(null)}
                                  >
                                    Cancel
                                  </Button>
                                </div>
                              </div>
                            ) : nextStates(e.status).length === 0 ? (
                              <span className="text-xs text-slate-500">No further moves</span>
                            ) : (
                              <div className="flex flex-wrap gap-1.5">
                                {nextStates(e.status).map((to) => (
                                  <Button
                                    key={to}
                                    variant="secondary"
                                    className="px-2! py-1! text-xs"
                                    disabled={busyId === e.id}
                                    onClick={() => requestTransition(e, to)}
                                  >
                                    {STATUS_LABELS[to]}
                                  </Button>
                                ))}
                              </div>
                            )}
                          </td>
                        )}
                      </tr>
                    );
                  })}
                  {pageData && enrollments.length === 0 && (
                    <tr>
                      <td colSpan={columns} className="py-8 text-center">
                        <p className="text-sm text-slate-500">
                          {statusFilter === "all"
                            ? "No enrollments yet — admit the first learner."
                            : `No enrollments with status "${STATUS_LABELS[statusFilter]}".`}
                        </p>
                        {statusFilter === "all" && canManage && (
                          <Button className="mt-3" onClick={() => setEnrollOpen(true)}>
                            Enroll a learner
                          </Button>
                        )}
                        {statusFilter !== "all" && (
                          <Button
                            variant="secondary"
                            className="mt-3"
                            onClick={() => {
                              setStatusFilter("all");
                              setOffset(0);
                            }}
                          >
                            Clear filter
                          </Button>
                        )}
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>

            <div className="mt-4 flex items-center justify-between text-xs text-slate-600">
              <span aria-live="polite">
                {total === 0
                  ? "0 enrollments"
                  : `${offset + 1}–${Math.min(offset + PAGE_SIZE, total)} of ${total}`}
              </span>
              <div className="flex gap-2">
                <Button
                  variant="secondary"
                  disabled={offset === 0}
                  onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
                >
                  Previous
                </Button>
                <Button
                  variant="secondary"
                  disabled={offset + PAGE_SIZE >= total}
                  onClick={() => setOffset(offset + PAGE_SIZE)}
                >
                  Next
                </Button>
              </div>
            </div>
          </>
        )}
      </Card>
    </div>
  );
}

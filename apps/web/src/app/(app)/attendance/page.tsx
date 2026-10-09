"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { apiFetch } from "@/lib/api";
import { useSession } from "@/lib/session";
import { can } from "@/lib/permissions";
import { Badge, Button, Card, ErrorNote } from "@/components/ui";
import { isTerminal } from "@/lib/enrollment-states";
import type {
  AttendanceRegister,
  AttendanceStatus,
  ClassGroup,
  EnrollmentPage,
} from "@/types/api";

const STATUSES: AttendanceStatus[] = ["present", "late", "absent", "excused"];

const STATUS_TONE: Record<AttendanceStatus, "green" | "amber" | "red" | "blue"> = {
  present: "green",
  late: "amber",
  absent: "red",
  excused: "blue",
};

function isAbortError(err: unknown): boolean {
  return err instanceof Error && err.name === "AbortError";
}

/** The calendar date the workspace treats as "today" (local time). */
function todayLocalKey(): string {
  const d = new Date();
  const month = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${d.getFullYear()}-${month}-${day}`;
}

interface RosterRow {
  learnerId: string;
  name: string;
}

/**
 * Attendance surface (#189): the morning roll call. Pick a class and a date,
 * mark each learner present / late / absent / excused, and save — the register
 * persists in the demo store (idempotent per date+class+learner) exactly the
 * way the live attendance domain will accept it once the database is
 * connected (#142 contract parity).
 *
 * The class roster derives from `/api/v1/enrollments` (non-terminal statuses
 * bound to the class) merged with the saved register — unmarked learners
 * render honestly as "unmarked" rather than being hidden.
 */
export default function AttendancePage() {
  const { me, activeSchoolId } = useSession();
  const allowed = can(me, "attendance.record");

  const [classes, setClasses] = useState<ClassGroup[]>([]);
  const [roster, setRoster] = useState<RosterRow[]>([]);
  const [learnerNames, setLearnerNames] = useState<Map<string, string>>(new Map());
  const [classId, setClassId] = useState<string>("");
  const [date, setDate] = useState<string>(todayLocalKey());
  /** The marks as SAVED in the store — the baseline for the dirty check. */
  const [saved, setSaved] = useState<Map<string, AttendanceStatus>>(new Map());
  /** The marks currently selected in the UI (may be ahead of `saved`). */
  const [marks, setMarks] = useState<Map<string, AttendanceStatus>>(new Map());
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  /** Fetch/refresh failures — cleared by the next successful load. */
  const [loadError, setLoadError] = useState<string | null>(null);
  /** Save failures — never touched by background loads (race-free). */
  const [saveError, setSaveError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const load = useCallback(
    async (signal?: AbortSignal) => {
      if (!activeSchoolId || !can(me, "attendance.record")) return;
      try {
        const [classesRes, enrollmentsRes, rosterRes] = await Promise.all([
          apiFetch<{ classes?: ClassGroup[] } | ClassGroup[]>("/api/v1/classes", { signal }),
          apiFetch<EnrollmentPage>("/api/v1/enrollments?limit=100", { signal }),
          apiFetch<{ learners: Array<{ id: string; firstName: string; lastName: string }> }>(
            "/api/v1/learners?limit=100",
            { signal },
          ),
        ]);
        const classList = Array.isArray(classesRes) ? classesRes : (classesRes.classes ?? []);
        const names = new Map(
          rosterRes.learners.map((l) => [l.id, `${l.firstName} ${l.lastName}`]),
        );
        setClasses(classList);
        setLearnerNames(names);

        const targetClass = classId || classList[0]?.id || "";
        if (!classId && targetClass) setClassId(targetClass);

        if (targetClass) {
          const rows: RosterRow[] = enrollmentsRes.enrollments
            .filter((e) => e.classGroupId === targetClass && !isTerminal(e.status))
            .map((e) => ({
              learnerId: e.learnerId,
              name: names.get(e.learnerId) ?? e.learnerId,
            }))
            .sort((a, b) => a.name.localeCompare(b.name));
          setRoster(rows);

          const register = await apiFetch<AttendanceRegister>(
            `/api/v1/attendance?date=${encodeURIComponent(date)}&classGroupId=${encodeURIComponent(targetClass)}`,
            { signal },
          );
          const savedMarks = new Map<string, AttendanceStatus>(
            register.records.map((r) => [r.learnerId, r.status]),
          );
          setSaved(savedMarks);
          setMarks(new Map(savedMarks));
        }
        setLoadError(null);
        setNotice(null);
      } catch (err) {
        if (isAbortError(err)) return;
        setLoadError(err instanceof Error ? err.message : "failed to load attendance data");
      } finally {
        setLoading(false);
      }
    },
    [activeSchoolId, me, classId, date],
  );

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  const dirty = useMemo(() => {
    if (saved.size !== marks.size) return true;
    for (const [learnerId, status] of marks) {
      if (saved.get(learnerId) !== status) return true;
    }
    return false;
  }, [saved, marks]);

  const summary = useMemo(() => {
    const counts: Record<AttendanceStatus | "unmarked", number> = {
      present: 0,
      late: 0,
      absent: 0,
      excused: 0,
      unmarked: 0,
    };
    for (const row of roster) {
      const status = marks.get(row.learnerId);
      counts[status ?? "unmarked"] += 1;
    }
    return counts;
  }, [roster, marks]);

  const save = async () => {
    if (!classId || roster.length === 0) return;
    const entries = roster
      .filter((r) => marks.has(r.learnerId))
      .map((r) => ({ learnerId: r.learnerId, status: marks.get(r.learnerId)! }));
    if (entries.length === 0) {
      setSaveError("Mark at least one learner before saving the register.");
      return;
    }
    setSaving(true);
    setSaveError(null);
    setNotice(null);
    try {
      const register = await apiFetch<AttendanceRegister>("/api/v1/attendance", {
        method: "POST",
        body: { date, classGroupId: classId, entries },
      });
      const savedMarks = new Map<string, AttendanceStatus>(
        register.records.map((r) => [r.learnerId, r.status]),
      );
      setSaved(savedMarks);
      setMarks(new Map(savedMarks));
      setNotice(`Register saved: ${register.total} learners marked for ${date}.`);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : "failed to save the register");
    } finally {
      setSaving(false);
    }
  };

  if (!allowed) {
    return (
      <div className="space-y-4">
        <h1 className="text-xl font-semibold">Attendance</h1>
        <ErrorNote message="Your role does not include attendance access." />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">Attendance</h1>
          <p className="text-sm text-slate-500">Daily roll call per class group</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <label htmlFor="attendance-class" className="text-xs text-slate-500">
            Class
          </label>
          <select
            id="attendance-class"
            value={classId}
            onChange={(e) => setClassId(e.target.value)}
            className="rounded-md border border-slate-300 bg-white px-2 py-1 text-sm"
          >
            {classes.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
          </select>
          <label htmlFor="attendance-date" className="text-xs text-slate-500">
            Date
          </label>
          <input
            id="attendance-date"
            type="date"
            value={date}
            onChange={(e) => setDate(e.target.value)}
            className="rounded-md border border-slate-300 bg-white px-2 py-1 text-sm"
          />
        </div>
      </header>

      <ErrorNote message={loadError} />
      {saveError && <ErrorNote message={saveError} />}
      {notice && (
        <p className="rounded-md border border-green-200 bg-green-50 px-3 py-2 text-sm text-green-800">
          {notice}
        </p>
      )}

      {loading && (
        <p className="text-sm text-slate-500" aria-live="polite">
          Loading register…
        </p>
      )}

      {!loading && roster.length > 0 && (
        <Card
          title="Roll call"
          action={
            <div className="flex items-center gap-3">
              <span className="text-xs text-slate-500" aria-live="polite">
                {dirty ? "Unsaved changes" : "All changes saved"}
              </span>
              <Button onClick={save} disabled={saving}>
                {saving ? "Saving…" : "Save register"}
              </Button>
            </div>
          }
        >
          <div className="mb-4 flex flex-wrap gap-x-4 gap-y-1 text-xs text-slate-600">
            {STATUSES.map((s) => (
              <span key={s}>
                {s}: <strong>{summary[s]}</strong>
              </span>
            ))}
            <span>
              unmarked: <strong>{summary.unmarked}</strong>
            </span>
          </div>

          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-slate-200 text-left text-xs uppercase tracking-wide text-slate-500">
                  <th scope="col" className="pb-2">Learner</th>
                  <th scope="col" className="pb-2">Mark</th>
                </tr>
              </thead>
              <tbody>
                {roster.map((row) => {
                  const current = marks.get(row.learnerId) ?? null;
                  return (
                    <tr key={row.learnerId} className="border-b border-slate-100 last:border-0">
                      <td className="py-2 font-medium">{row.name}</td>
                      <td className="py-2">
                        <div
                          role="group"
                          aria-label={`Attendance mark for ${row.name}`}
                          className="flex flex-wrap gap-1"
                        >
                          {STATUSES.map((s) => (
                            <button
                              key={s}
                              type="button"
                              aria-pressed={current === s}
                              onClick={() =>
                                setMarks((prev) => {
                                  const next = new Map(prev);
                                  next.set(row.learnerId, s);
                                  return next;
                                })
                              }
                              className={`rounded-full border px-2.5 py-0.5 text-xs transition-colors ${
                                current === s
                                  ? "border-slate-800 bg-slate-800 text-white"
                                  : "border-slate-300 bg-white text-slate-600 hover:border-slate-500"
                              }`}
                            >
                              {s}
                            </button>
                          ))}
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </Card>
      )}

      {!loading && roster.length === 0 && classId && (
        <Card title="Roll call">
          <div className="flex items-center gap-2 text-sm text-slate-500">
            <Badge tone="slate">empty</Badge>
            No learners are placed in this class yet — admit or enroll them first.
          </div>
        </Card>
      )}
    </div>
  );
}

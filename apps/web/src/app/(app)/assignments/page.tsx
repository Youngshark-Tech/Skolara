"use client";

import { useCallback, useEffect, useState } from "react";
import { apiFetch } from "@/lib/api";
import { useSession } from "@/lib/session";
import { can } from "@/lib/permissions";
import { Badge, Button, Card, ErrorNote, Input, Label } from "@/components/ui";
import type { Assignment, AssignmentPage, ClassGroup } from "@/types/api";

const PAGE_SIZE = 50;

type StatusFilter = "all" | "open" | "overdue";
const STATUS_FILTERS: StatusFilter[] = ["all", "open", "overdue"];

function isAbortError(err: unknown): boolean {
  return err instanceof Error && err.name === "AbortError";
}

/** Local calendar date — the same basis the derived status uses. */
function todayLocalKey(): string {
  const d = new Date();
  const month = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${d.getFullYear()}-${month}-${day}`;
}

/**
 * Assignments surface (#190): the class work book. Filter by class and by
 * derived status, create work with a due date — the demo transport persists
 * to the same in-memory store the other workspaces read (contract parity
 * per #142).
 *
 * `overdue` is DERIVED from dueDate against today — never stored — so the
 * badge flips honestly as days pass, exactly as the live domain will judge.
 */
export default function AssignmentsPage() {
  const { me, activeSchoolId } = useSession();
  const allowed = can(me, "assignment.read") || can(me, "assignment.manage");

  const [classes, setClasses] = useState<ClassGroup[]>([]);
  const [assignments, setAssignments] = useState<Assignment[]>([]);
  const [total, setTotal] = useState(0);
  const [classFilter, setClassFilter] = useState<string>("");
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  /** Fetch/refresh failures — cleared by the next successful load. */
  const [loadError, setLoadError] = useState<string | null>(null);
  /** Publish failures — never touched by background loads (race-free). */
  const [formError, setFormError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [title, setTitle] = useState("");
  const [subject, setSubject] = useState("");
  const [dueDate, setDueDate] = useState("");
  const [description, setDescription] = useState("");

  const load = useCallback(
    async (signal?: AbortSignal) => {
      if (!activeSchoolId || !can(me, "assignment.read")) return;
      try {
        const [classesRes, assignmentsRes] = await Promise.all([
          apiFetch<{ classes?: ClassGroup[] } | ClassGroup[]>("/api/v1/classes", { signal }),
          (() => {
            const params = new URLSearchParams({
              limit: String(PAGE_SIZE),
              offset: "0",
            });
            if (classFilter) params.set("classGroupId", classFilter);
            if (statusFilter !== "all") params.set("status", statusFilter);
            return apiFetch<AssignmentPage>(`/api/v1/assignments?${params.toString()}`, { signal });
          })(),
        ]);
        setClasses(Array.isArray(classesRes) ? classesRes : (classesRes.classes ?? []));
        setAssignments(assignmentsRes.assignments);
        setTotal(assignmentsRes.total);
        setLoadError(null);
      } catch (err) {
        if (isAbortError(err)) return;
        setLoadError(err instanceof Error ? err.message : "failed to load assignments");
      } finally {
        setLoading(false);
      }
    },
    [activeSchoolId, me, classFilter, statusFilter],
  );

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  const today = todayLocalKey();

  const create = async (e: React.FormEvent) => {
    e.preventDefault();
    setCreating(true);
    setFormError(null);
    setNotice(null);
    try {
      const body: Record<string, string> = { title, classGroupId: classFilter, dueDate };
      if (subject) body.subject = subject;
      if (description) body.description = description;
      const created = await apiFetch<Assignment>("/api/v1/assignments", {
        method: "POST",
        body,
      });
      setNotice(`“${created.title}” published — due ${created.dueDate}.`);
      setTitle("");
      setSubject("");
      setDueDate("");
      setDescription("");
      await load();
    } catch (err) {
      setFormError(err instanceof Error ? err.message : "failed to create the assignment");
    } finally {
      setCreating(false);
    }
  };

  if (!allowed) {
    return (
      <div className="space-y-4">
        <h1 className="text-xl font-semibold">Assignments</h1>
        <ErrorNote message="Your role does not include assignment access." />
      </div>
    );
  }

  const className = (id: string) => classes.find((c) => c.id === id)?.name ?? id;

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-xl font-semibold">Assignments</h1>
        <p className="text-sm text-slate-500">
          The class work book — {total} assignment{total === 1 ? "" : "s"} in view
        </p>
      </header>

      <ErrorNote message={loadError} />
      {notice && (
        <p className="rounded-md border border-green-200 bg-green-50 px-3 py-2 text-sm text-green-800">
          {notice}
        </p>
      )}

      {loading && (
        <p className="text-sm text-slate-500" aria-live="polite">
          Loading assignments…
        </p>
      )}

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <div className="lg:col-span-2">
          <Card
            title="Work book"
            action={
              <div className="flex items-center gap-2">
                <label htmlFor="assignment-class-filter" className="text-xs text-slate-500">
                  Class
                </label>
                <select
                  id="assignment-class-filter"
                  value={classFilter}
                  onChange={(e) => setClassFilter(e.target.value)}
                  className="rounded-md border border-slate-300 bg-white px-2 py-1 text-sm"
                >
                  <option value="">All classes</option>
                  {classes.map((c) => (
                    <option key={c.id} value={c.id}>
                      {c.name}
                    </option>
                  ))}
                </select>
                <label htmlFor="assignment-status-filter" className="text-xs text-slate-500">
                  Status
                </label>
                <select
                  id="assignment-status-filter"
                  value={statusFilter}
                  onChange={(e) => setStatusFilter(e.target.value as StatusFilter)}
                  className="rounded-md border border-slate-300 bg-white px-2 py-1 text-sm"
                >
                  {STATUS_FILTERS.map((s) => (
                    <option key={s} value={s}>
                      {s}
                    </option>
                  ))}
                </select>
              </div>
            }
          >
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-slate-200 text-left text-xs uppercase tracking-wide text-slate-500">
                    <th scope="col" className="pb-2">Title</th>
                    <th scope="col" className="pb-2">Class</th>
                    <th scope="col" className="pb-2">Subject</th>
                    <th scope="col" className="pb-2">Due</th>
                    <th scope="col" className="pb-2">Status</th>
                  </tr>
                </thead>
                <tbody>
                  {assignments.map((a) => {
                    const overdue = a.dueDate < today;
                    return (
                      <tr key={a.id} className="border-b border-slate-100 last:border-0">
                        <td className="py-2 font-medium">
                          {a.title}
                          {a.description && (
                            <span className="block text-xs font-normal text-slate-500">
                              {a.description}
                            </span>
                          )}
                        </td>
                        <td className="py-2 text-slate-500">{className(a.classGroupId)}</td>
                        <td className="py-2 text-slate-500">{a.subject ?? "—"}</td>
                        <td className="py-2 text-slate-500">{a.dueDate}</td>
                        <td className="py-2">
                          <Badge tone={overdue ? "red" : "green"}>
                            {overdue ? "overdue" : "open"}
                          </Badge>
                        </td>
                      </tr>
                    );
                  })}
                  {assignments.length === 0 && (
                    <tr>
                      <td colSpan={5} className="py-6 text-center text-slate-500">
                        No assignments match this filter
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </Card>
        </div>

        {can(me, "assignment.manage") && (
          <Card title="Publish an assignment">
            <form className="space-y-3" onSubmit={create}>
              <div>
                <Label htmlFor="assignment-title">Title</Label>
                <Input
                  id="assignment-title"
                  required
                  minLength={3}
                  maxLength={200}
                  value={title}
                  onChange={(e) => setTitle(e.target.value)}
                />
              </div>
              <div>
                <Label htmlFor="assignment-class">Class</Label>
                <select
                  id="assignment-class"
                  required
                  value={classFilter}
                  onChange={(e) => setClassFilter(e.target.value)}
                  className="w-full rounded-md border border-slate-300 bg-white px-2 py-1.5 text-sm"
                >
                  <option value="" disabled>
                    Select a class…
                  </option>
                  {classes.map((c) => (
                    <option key={c.id} value={c.id}>
                      {c.name}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <Label htmlFor="assignment-subject">Subject (optional)</Label>
                <Input
                  id="assignment-subject"
                  maxLength={100}
                  value={subject}
                  onChange={(e) => setSubject(e.target.value)}
                />
              </div>
              <div>
                <Label htmlFor="assignment-due">Due date</Label>
                <Input
                  id="assignment-due"
                  type="date"
                  required
                  value={dueDate}
                  onChange={(e) => setDueDate(e.target.value)}
                />
              </div>
              <div>
                <Label htmlFor="assignment-description">Description (optional)</Label>
                <textarea
                  id="assignment-description"
                  rows={3}
                  maxLength={2000}
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  className="w-full rounded-md border border-slate-300 bg-white px-2 py-1.5 text-sm"
                />
              </div>
              <Button type="submit" disabled={creating}>
                {creating ? "Publishing…" : "Publish assignment"}
              </Button>
              <ErrorNote message={formError} />
              <p className="text-xs text-slate-500">
                Overdue is derived from the due date — never stored — so the badge stays honest.
              </p>
            </form>
          </Card>
        )}
      </div>
    </div>
  );
}

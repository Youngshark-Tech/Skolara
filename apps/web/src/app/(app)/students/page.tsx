"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { apiFetch } from "@/lib/api";
import { useSession } from "@/lib/session";
import { can } from "@/lib/permissions";
import { Badge, Button, Card, ErrorNote, Input, Label, Notice } from "@/components/ui";
import type { Learner, LearnerPage } from "@/types/api";

const PAGE_SIZE = 20;
const SEARCH_DEBOUNCE_MS = 250;

/**
 * A superseded request's rejection must never surface as an error: aborting a
 * fetch rejects with an "AbortError" DOMException, which we simply ignore.
 */
function isAbortError(err: unknown): boolean {
  return err instanceof Error && err.name === "AbortError";
}

/**
 * Students surface: learner roster with search, pagination, and record
 * creation. #57: search is debounced and every superseded request is aborted
 * (out-of-order stale responses used to win the race); the pager handles the
 * empty table honestly; every control is labeled; the roster scrolls
 * horizontally on narrow screens instead of overflowing.
 */
export default function StudentsPage() {
  const { me, activeSchoolId } = useSession();
  const [page, setPage] = useState<LearnerPage | null>(null);
  const [query, setQuery] = useState("");
  const [debouncedQuery, setDebouncedQuery] = useState("");
  const [offset, setOffset] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [firstName, setFirstName] = useState("");
  const [lastName, setLastName] = useState("");
  const [externalId, setExternalId] = useState("");
  const [notice, setNotice] = useState<string | null>(null);
  /** The learner just created — deep link into the enroll flow (#58). */
  const [createdLearner, setCreatedLearner] = useState<Learner | null>(null);

  // Debounce: one request per SETTLED query, not one per keystroke.
  useEffect(() => {
    const timer = window.setTimeout(() => setDebouncedQuery(query), SEARCH_DEBOUNCE_MS);
    return () => window.clearTimeout(timer);
  }, [query]);

  const load = useCallback(
    async (signal?: AbortSignal) => {
      if (!activeSchoolId || !can(me, "student.read")) return;
      try {
        const params = new URLSearchParams({ limit: String(PAGE_SIZE), offset: String(offset) });
        if (debouncedQuery) params.set("q", debouncedQuery);
        const data = await apiFetch<LearnerPage>(`/api/v1/learners?${params.toString()}`, { signal });
        setPage(data);
        setError(null);
      } catch (err) {
        if (isAbortError(err)) return; // a newer request superseded this one
        setError(err instanceof Error ? err.message : "failed to load learners");
      }
    },
    [activeSchoolId, me, offset, debouncedQuery],
  );

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    // Abort the in-flight request whenever a newer one (or unmount) replaces
    // it — this is what makes out-of-order stale responses impossible.
    return () => controller.abort();
  }, [load]);

  const createLearner = async (e: React.FormEvent) => {
    e.preventDefault();
    setCreating(true);
    setError(null);
    setNotice(null);
    try {
      const body: Record<string, string> = { firstName, lastName };
      if (externalId) body.externalId = externalId;
      const created = await apiFetch<Learner>("/api/v1/learners", { method: "POST", body });
      setCreatedLearner(created);
      // Honest copy (#57/#58): creating a learner records their identity —
      // enrollment (binding them to this school) is a separate, explicit step.
      setNotice(
        `Learner record created for ${firstName} ${lastName}. Enroll them at this school to finish admission.`,
      );
      setFirstName("");
      setLastName("");
      setExternalId("");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to create learner");
    } finally {
      setCreating(false);
    }
  };

  if (!can(me, "student.read") && !can(me, "student.manage")) {
    return (
      <div className="space-y-4">
        <h1 className="text-xl font-semibold">Students</h1>
        <ErrorNote message="Your role does not include student access." />
      </div>
    );
  }

  const total = page?.total ?? 0;

  return (
    <div className="space-y-6">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">Students</h1>
          <p className="text-sm text-slate-500">{total} learners enrolled at the active school</p>
        </div>
      </header>

      <ErrorNote message={error} />

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <div className="lg:col-span-2">
          <Card
            title="Learner roster"
            action={
              <div className="flex items-center gap-2">
                <Input
                  id="learner-search"
                  type="search"
                  aria-label="Search learners by name"
                  placeholder="Search name…"
                  value={query}
                  onChange={(e) => {
                    setOffset(0);
                    setQuery(e.target.value);
                  }}
                  className="!w-48"
                />
              </div>
            }
          >
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-slate-200 text-left text-xs uppercase tracking-wide text-slate-500">
                    <th scope="col" className="pb-2">
                      Name
                    </th>
                    <th scope="col" className="pb-2">
                      Admission no.
                    </th>
                    <th scope="col" className="pb-2">
                      Enrolled
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {(page?.learners ?? []).map((l) => (
                    <tr key={l.id} className="border-b border-slate-100 last:border-0">
                      <td className="py-2 font-medium">
                        {l.firstName} {l.lastName}
                      </td>
                      <td className="py-2 text-slate-500">{l.externalId ?? "—"}</td>
                      <td className="py-2 text-slate-500">
                        {new Date(l.createdAt).toLocaleDateString()}
                      </td>
                    </tr>
                  ))}
                  {page && page.learners.length === 0 && (
                    <tr>
                      <td colSpan={3} className="py-6 text-center text-slate-500">
                        No learners found
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>

            <div className="mt-4 flex items-center justify-between text-xs text-slate-600">
              {/* "1–0 of 0" used to render for an empty roster — show the truth. */}
              <span aria-live="polite">
                {total === 0
                  ? "0 learners"
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
          </Card>
        </div>

        {can(me, "student.manage") && (
          <Card title="Create a learner record">
            <form className="space-y-3" onSubmit={createLearner}>
              <div>
                <Label htmlFor="student-first-name">First name</Label>
                <Input
                  id="student-first-name"
                  required
                  maxLength={100}
                  value={firstName}
                  onChange={(e) => setFirstName(e.target.value)}
                />
              </div>
              <div>
                <Label htmlFor="student-last-name">Last name</Label>
                <Input
                  id="student-last-name"
                  required
                  maxLength={100}
                  value={lastName}
                  onChange={(e) => setLastName(e.target.value)}
                />
              </div>
              <div>
                <Label htmlFor="student-external-id">Admission number (optional)</Label>
                <Input
                  id="student-external-id"
                  value={externalId}
                  onChange={(e) => setExternalId(e.target.value)}
                />
              </div>
              {notice && (
                <Notice>
                  <Badge tone="green">OK</Badge> {notice}{" "}
                  {createdLearner && (
                    <Link
                      className="font-semibold underline hover:no-underline"
                      href={`/enrollments?enroll=${createdLearner.id}`}
                    >
                      Enroll {createdLearner.firstName} →
                    </Link>
                  )}
                </Notice>
              )}
              <Button type="submit" disabled={creating}>
                {creating ? "Creating…" : "Create learner"}
              </Button>
              <p className="text-xs text-slate-500">
                Learner identity is global; enrollment binds them to this school. After creating
                the record, finish admission on the{" "}
                <Link className="underline hover:no-underline" href="/enrollments">
                  Enrollments
                </Link>{" "}
                page.
              </p>
            </form>
          </Card>
        )}
      </div>
    </div>
  );
}

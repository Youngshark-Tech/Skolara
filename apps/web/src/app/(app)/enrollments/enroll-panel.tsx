"use client";

import { useCallback, useEffect, useState } from "react";
import { apiFetch, ApiError } from "@/lib/api";
import { useSession } from "@/lib/session";
import { Button, Card, ErrorNote, Input, Label } from "@/components/ui";
import { splitEnrollError, type EnrollFormErrors } from "@/lib/enroll-form";
import type { AcademicYear, ClassGroup, Enrollment, Learner, LearnerPage } from "@/types/api";

const SEARCH_DEBOUNCE_MS = 250;

/** A superseded search is aborted, never surfaced as an error (#57 pattern). */
function isAbortError(err: unknown): boolean {
  return err instanceof Error && err.name === "AbortError";
}

interface EnrollPanelProps {
  /** Deep link ?enroll=<learnerId> — fetch and preselect that learner (#58). */
  preselectLearnerId?: string | null;
  onEnrolled: (enrollment: Enrollment) => void;
  onClose: () => void;
}

/**
 * The enroll-learner flow (#58): learner picker (search from /learners) →
 * optional year/class pickers → POST /api/v1/enrollments. Consumes EXISTING
 * endpoints only; the initial-status select offers exactly the two initial
 * states the server accepts (applicant | admitted — students/service.go).
 */
export function EnrollPanel({ preselectLearnerId, onEnrolled, onClose }: EnrollPanelProps) {
  const { activeSchoolId } = useSession();

  const [query, setQuery] = useState("");
  const [debouncedQuery, setDebouncedQuery] = useState("");
  const [results, setResults] = useState<Learner[]>([]);
  const [searching, setSearching] = useState(false);
  const [searchError, setSearchError] = useState<string | null>(null);
  const [selectedLearner, setSelectedLearner] = useState<Learner | null>(null);

  const [years, setYears] = useState<AcademicYear[]>([]);
  const [classes, setClasses] = useState<ClassGroup[]>([]);
  const [referenceError, setReferenceError] = useState<string | null>(null);

  const [yearId, setYearId] = useState("");
  const [classId, setClassId] = useState("");
  const [status, setStatus] = useState<"admitted" | "applicant">("admitted");
  const [preselectNote, setPreselectNote] = useState<string | null>(null);

  const [submitting, setSubmitting] = useState(false);
  const [fieldErrors, setFieldErrors] = useState<EnrollFormErrors>({});

  // Debounced learner search: one request per settled query, superseded
  // requests aborted (same pattern as the students page, #57).
  useEffect(() => {
    const timer = window.setTimeout(() => setDebouncedQuery(query.trim()), SEARCH_DEBOUNCE_MS);
    return () => window.clearTimeout(timer);
  }, [query]);

  const loadLearners = useCallback(
    async (signal: AbortSignal) => {
      if (!activeSchoolId) return;
      setSearching(true);
      setSearchError(null);
      try {
        const params = new URLSearchParams({ limit: "10", offset: "0" });
        if (debouncedQuery) params.set("q", debouncedQuery);
        const data = await apiFetch<LearnerPage>(`/api/v1/learners?${params.toString()}`, { signal });
        setResults(data.learners);
      } catch (err) {
        if (isAbortError(err)) return;
        setSearchError(err instanceof Error ? err.message : "failed to search learners");
      } finally {
        setSearching(false);
      }
    },
    [activeSchoolId, debouncedQuery],
  );

  useEffect(() => {
    const controller = new AbortController();
    void loadLearners(controller.signal);
    return () => controller.abort();
  }, [loadLearners]);

  // Optional reference data (academic years, classes). Degrades to a hint —
  // enrollment without year/class is fully valid per the API contract.
  useEffect(() => {
    if (!activeSchoolId) return;
    const controller = new AbortController();
    void (async () => {
      try {
        const [yearList, classList] = await Promise.all([
          apiFetch<AcademicYear[]>("/api/v1/academic-years", { signal: controller.signal }),
          apiFetch<ClassGroup[]>("/api/v1/classes", { signal: controller.signal }),
        ]);
        setYears(yearList);
        setClasses(classList);
      } catch (err) {
        if (isAbortError(err)) return;
        setReferenceError(
          "Could not load academic years and classes — you can still enroll without them.",
        );
      }
    })();
    return () => controller.abort();
  }, [activeSchoolId]);

  // Deep-link preselect (?enroll=<learnerId> from the create-learner flows).
  useEffect(() => {
    if (!preselectLearnerId || !activeSchoolId) return;
    const controller = new AbortController();
    void (async () => {
      try {
        const learner = await apiFetch<Learner>(`/api/v1/learners/${preselectLearnerId}`, {
          signal: controller.signal,
        });
        setSelectedLearner(learner);
        setPreselectNote(null);
        setFieldErrors((prev) => ({ ...prev, learner: undefined }));
      } catch (err) {
        if (isAbortError(err)) return;
        if (err instanceof ApiError && err.status === 404) {
          // KNOWN API VISIBILITY GAP (report-only finding, see PR): the
          // /learners endpoints resolve learners through their enrollments,
          // so a JUST-CREATED learner 404s here until its first enrollment
          // lands. The deep link carries the id from our own create-learner
          // success — the enrollment can proceed for exactly that learner —
          // so keep the admit story alive with an honest note instead of a
          // dead end. The id never comes from free-text input.
          setSelectedLearner({
            id: preselectLearnerId,
            firstName: "New",
            lastName: "learner",
            createdAt: "",
          });
          setPreselectNote(
            "This learner's record isn't listed yet — brand-new learners become searchable only after their first enrollment. The enrollment will target the learner carried by this link.",
          );
        } else {
          setFieldErrors((prev) => ({
            ...prev,
            learner: "Could not load that learner — search for them by name.",
          }));
        }
      }
    })();
    return () => controller.abort();
  }, [preselectLearnerId, activeSchoolId]);

  const classOptions = yearId ? classes.filter((c) => c.academicYearId === yearId) : classes;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedLearner) {
      setFieldErrors({ learner: "Pick a learner to enroll." });
      return;
    }
    setSubmitting(true);
    setFieldErrors({});
    try {
      const body: Record<string, string> = { learnerId: selectedLearner.id, status };
      if (yearId) body.academicYearId = yearId;
      if (classId) body.classGroupId = classId;
      const enrollment = await apiFetch<Enrollment>("/api/v1/enrollments", {
        method: "POST",
        body,
      });
      onEnrolled(enrollment);
    } catch (err) {
      // ErrAlreadyEnrolled (409) / ErrValidation (400) become field-level copy.
      setFieldErrors(splitEnrollError(err));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Card
      title="Enroll a learner"
      action={
        <button
          type="button"
          onClick={onClose}
          aria-label="Close enroll panel"
          className="rounded-sm px-2 py-1 text-slate-500 hover:text-slate-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
        >
          Close ✕
        </button>
      }
    >
      <form className="space-y-4" onSubmit={submit}>
        <fieldset className="space-y-2" disabled={submitting}>
          <legend className="mb-1 text-xs font-semibold text-slate-700">1 · Pick the learner</legend>
          <Label htmlFor="enroll-learner-search" className="sr-only">
            Search learners by name
          </Label>
          <Input
            id="enroll-learner-search"
            type="search"
            placeholder="Search learners by name…"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
          {searching && <p className="text-xs text-slate-500">Searching…</p>}
          <ErrorNote message={searchError} />
          <ErrorNote message={fieldErrors.learner ?? null} />
          <div className="max-h-56 space-y-1 overflow-y-auto rounded-md border border-slate-200 p-2">
            {results.length === 0 && !searching && (
              <p className="px-1 py-2 text-xs text-slate-500">
                No learners match. Returning and withdrawn learners are searchable by
                name; brand-new learners become searchable here only after their first
                enrollment — create the record (Students → Create learner) and follow
                the “Enroll …” link that follows it.
              </p>
            )}
            {results.map((l) => (
              <label
                key={l.id}
                className={`flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm ${
                  selectedLearner?.id === l.id ? "bg-primary/10 font-medium" : "hover:bg-slate-50"
                }`}
              >
                <input
                  type="radio"
                  name="enroll-learner"
                  value={l.id}
                  checked={selectedLearner?.id === l.id}
                  onChange={() => {
                    setSelectedLearner(l);
                    setFieldErrors((prev) => ({ ...prev, learner: undefined }));
                  }}
                  className="accent-primary"
                />
                <span>
                  {l.firstName} {l.lastName}
                  {l.externalId ? (
                    <span className="ml-1 text-xs text-slate-500">({l.externalId})</span>
                  ) : null}
                </span>
              </label>
            ))}
          </div>
          {selectedLearner && (
            <p className="text-xs text-slate-500">
              Selected: {selectedLearner.firstName} {selectedLearner.lastName}
            </p>
          )}
          {preselectNote && (
            <p role="note" className="rounded-md bg-amber-50 px-3 py-2 text-xs text-amber-700">
              {preselectNote}
            </p>
          )}
        </fieldset>

        <fieldset className="grid grid-cols-1 gap-3 sm:grid-cols-2" disabled={submitting}>
          <legend className="mb-1 text-xs font-semibold text-slate-700">2 · Optional context</legend>
          <div>
            <Label htmlFor="enroll-year">Academic year</Label>
            <select
              id="enroll-year"
              value={yearId}
              onChange={(e) => {
                setYearId(e.target.value);
                setClassId("");
              }}
              className="w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
            >
              <option value="">None</option>
              {years.map((y) => (
                <option key={y.id} value={y.id}>
                  {y.name}
                </option>
              ))}
            </select>
          </div>
          <div>
            <Label htmlFor="enroll-class">Class group</Label>
            <select
              id="enroll-class"
              value={classId}
              onChange={(e) => setClassId(e.target.value)}
              className="w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
            >
              <option value="">None</option>
              {classOptions.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </select>
          </div>
          <div>
            <Label htmlFor="enroll-status">Initial status</Label>
            <select
              id="enroll-status"
              value={status}
              onChange={(e) => setStatus(e.target.value as "admitted" | "applicant")}
              className="w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
            >
              <option value="admitted">Admitted</option>
              <option value="applicant">Applicant</option>
            </select>
            {fieldErrors.status && (
              <p className="mt-1 text-xs text-red-700">{fieldErrors.status}</p>
            )}
          </div>
        </fieldset>

        {referenceError && <p className="text-xs text-amber-700">{referenceError}</p>}
        <ErrorNote message={fieldErrors.academicYearId ?? null} />
        <ErrorNote message={fieldErrors.classGroupId ?? null} />
        <ErrorNote message={fieldErrors.form ?? null} />

        <div className="flex flex-wrap items-center gap-2">
          <Button type="submit" disabled={submitting || !selectedLearner}>
            {submitting ? "Enrolling…" : "Enroll learner"}
          </Button>
          <p className="text-xs text-slate-500">
            The server stays authoritative — this form only offers legal initial states.
          </p>
        </div>
      </form>
    </Card>
  );
}

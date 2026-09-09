"use client";

import { useCallback, useEffect, useState } from "react";
import { apiFetch } from "@/lib/api";
import { useSession } from "@/lib/session";
import { can } from "@/lib/permissions";
import { Badge, Button, Card, ErrorNote, Input, Label } from "@/components/ui";

interface Learner {
  id: string;
  firstName: string;
  lastName: string;
  middleName?: string | null;
  externalId?: string | null;
  createdAt: string;
}

interface LearnerPage {
  learners: Learner[];
  total: number;
  limit: number;
  offset: number;
}

const PAGE_SIZE = 20;

/** Students surface: learner roster with search, pagination, and admission. */
export default function StudentsPage() {
  const { me, activeSchoolId } = useSession();
  const [page, setPage] = useState<LearnerPage | null>(null);
  const [query, setQuery] = useState("");
  const [offset, setOffset] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [firstName, setFirstName] = useState("");
  const [lastName, setLastName] = useState("");
  const [externalId, setExternalId] = useState("");
  const [notice, setNotice] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!activeSchoolId || !can(me, "student.read")) return;
    try {
      const params = new URLSearchParams({ limit: String(PAGE_SIZE), offset: String(offset) });
      if (query) params.set("q", query);
      const data = await apiFetch<LearnerPage>(`/api/v1/learners?${params.toString()}`);
      setPage(data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to load learners");
    }
  }, [activeSchoolId, me, offset, query]);

  useEffect(() => {
    void load();
  }, [load]);

  const createLearner = async (e: React.FormEvent) => {
    e.preventDefault();
    setCreating(true);
    setError(null);
    setNotice(null);
    try {
      const body: Record<string, string> = { firstName, lastName };
      if (externalId) body.externalId = externalId;
      await apiFetch("/api/v1/learners", { method: "POST", body });
      setNotice(`Learner ${firstName} ${lastName} admitted`);
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
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-slate-200 text-left text-xs uppercase tracking-wide text-slate-400">
                  <th className="pb-2">Name</th>
                  <th className="pb-2">Admission no.</th>
                  <th className="pb-2">Enrolled</th>
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
                    <td colSpan={3} className="py-6 text-center text-slate-400">
                      No learners found
                    </td>
                  </tr>
                )}
              </tbody>
            </table>

            <div className="mt-4 flex items-center justify-between text-xs text-slate-500">
              <span>
                {offset + 1}–{Math.min(offset + PAGE_SIZE, total)} of {total}
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
          <Card title="Admit a learner">
            <form className="space-y-3" onSubmit={createLearner}>
              <div>
                <Label>First name</Label>
                <Input required maxLength={100} value={firstName} onChange={(e) => setFirstName(e.target.value)} />
              </div>
              <div>
                <Label>Last name</Label>
                <Input required maxLength={100} value={lastName} onChange={(e) => setLastName(e.target.value)} />
              </div>
              <div>
                <Label>Admission number (optional)</Label>
                <Input value={externalId} onChange={(e) => setExternalId(e.target.value)} />
              </div>
              {notice && (
                <p className="text-xs text-emerald-600">
                  <Badge tone="green">OK</Badge> {notice}
                </p>
              )}
              <Button type="submit" disabled={creating}>
                {creating ? "Creating…" : "Create learner"}
              </Button>
              <p className="text-xs text-slate-400">
                Learner identity is global; enrollment binds them to this school.
              </p>
            </form>
          </Card>
        )}
      </div>
    </div>
  );
}

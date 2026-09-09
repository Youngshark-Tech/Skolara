"use client";

import { Card } from "@/components/ui";
import { useSession } from "@/lib/session";
import { can } from "@/lib/permissions";

export default function AcademicsPage() {
  const { me } = useSession();
  const allowed = can(me, "academics.read") || can(me, "academics.manage");
  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-xl font-semibold">Academics</h1>
        <p className="text-sm text-slate-500">Wired to the live /api/v1/academics endpoints.</p>
      </header>
      {!allowed ? (
        <Card>
          <p className="text-sm text-slate-500">Your role does not include academics access.</p>
        </Card>
      ) : (
        <Card title="academics workspace">
          <p className="text-sm text-slate-500">
            The academics domain is live in the API (see packages/contracts/openapi.yaml).
            This workspace ships incrementally on the same contract — the students
            surface shows the established pattern.
          </p>
        </Card>
      )}
    </div>
  );
}

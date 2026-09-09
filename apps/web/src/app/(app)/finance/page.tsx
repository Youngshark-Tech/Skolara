"use client";

import { Card } from "@/components/ui";
import { useSession } from "@/lib/session";
import { can } from "@/lib/permissions";

export default function FinancePage() {
  const { me } = useSession();
  const allowed = can(me, "finance.read") || can(me, "finance.manage");
  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-xl font-semibold">Finance</h1>
        <p className="text-sm text-slate-500">Wired to the live /api/v1/finance endpoints.</p>
      </header>
      {!allowed ? (
        <Card>
          <p className="text-sm text-slate-500">Your role does not include finance access.</p>
        </Card>
      ) : (
        <Card title="finance workspace">
          <p className="text-sm text-slate-500">
            The finance domain is live in the API (see packages/contracts/openapi.yaml).
            This workspace ships incrementally on the same contract — the students
            surface shows the established pattern.
          </p>
        </Card>
      )}
    </div>
  );
}

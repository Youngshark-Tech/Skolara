"use client";

import { Badge, Card } from "@/components/ui";
import { useSession } from "@/lib/session";
import { can } from "@/lib/permissions";

/**
 * Assignments placeholder (#57): the nav entry is discoverable, but the domain
 * UI has no screen yet. A labeled "coming soon" state beats hiding the entry
 * silently — roles with assignment permissions see where the feature will live.
 */
export default function AssignmentsPage() {
  const { me } = useSession();
  const allowed = can(me, "assignment.read") || can(me, "assignment.manage");

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-xl font-semibold">Assignments</h1>
        <p className="text-sm text-slate-500">Draft → publish → close, with graded submissions.</p>
      </header>
      {!allowed ? (
        <Card>
          <p className="text-sm text-slate-500">Your role does not include assignments access.</p>
        </Card>
      ) : (
        <Card title="Coming soon">
          <div className="flex items-start gap-3">
            <Badge tone="amber">Planned</Badge>
            <p className="text-sm text-slate-600">
              The assignments workspace is not built yet. The API domain is live
              (draft → published → closed, submissions and grading — see
              packages/contracts/openapi.yaml); this screen will ship on the same contract.
            </p>
          </div>
        </Card>
      )}
    </div>
  );
}

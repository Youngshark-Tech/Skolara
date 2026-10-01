"use client";

import { Card } from "@/components/ui";
import { useSession } from "@/lib/session";
import { can } from "@/lib/permissions";

export default function AttendancePage() {
  const { me } = useSession();
  // Aligned with the nav gate (#57): attendance.record is the permission the
  // workspace needs (recording roll calls) — least privilege, and consistent
  // with the route guards the API registers (identity.PermAttendanceRecord).
  const allowed = can(me, "attendance.record");
  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-xl font-semibold">Attendance</h1>
        <p className="text-sm text-slate-500">Wired to the live /api/v1/attendance endpoints.</p>
      </header>
      {!allowed ? (
        <Card>
          <p className="text-sm text-slate-500">Your role does not include attendance access.</p>
        </Card>
      ) : (
        <Card title="attendance workspace">
          <p className="text-sm text-slate-500">
            The attendance domain is live in the API (see packages/contracts/openapi.yaml).
            This workspace ships incrementally on the same contract — the students
            surface shows the established pattern.
          </p>
        </Card>
      )}
    </div>
  );
}

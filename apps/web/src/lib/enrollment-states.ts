/**
 * UI mirror of the enrollment lifecycle state machine (#58).
 *
 * SOURCE OF TRUTH: services/api/internal/students/domain.go `legalTransitions`
 * (quoted verbatim below). This mirror exists for UX only — it decides which
 * transition buttons a row offers so users cannot even attempt illegal moves.
 * The SERVER stays authoritative: any out-of-sync move is rejected with 409
 * ErrIllegalTransition, which this UI surfaces as an error note.
 *
 * The unit test (enrollment-states.test.ts) pins every entry of this map to
 * the Go source; update both together in the same commit.
 */
import type { EnrollmentStatus } from "@/types/api";

/**
 * domain.go legalTransitions:
 *
 *      applicant        -> admitted | withdrawn
 *      admitted         -> active | withdrawn
 *      active           -> suspended | transfer_pending | graduated | withdrawn | alumni
 *      suspended        -> active | withdrawn
 *      transfer_pending -> transferred_out | active
 *      transferred_out  -> (terminal)
 *      graduated        -> alumni
 *      withdrawn        -> (terminal)
 *      alumni           -> (terminal)
 */
export const LEGAL_TRANSITIONS: Readonly<
  Record<EnrollmentStatus, readonly EnrollmentStatus[]>
> = {
  applicant: ["admitted", "withdrawn"],
  admitted: ["active", "withdrawn"],
  active: ["suspended", "transfer_pending", "graduated", "withdrawn", "alumni"],
  suspended: ["active", "withdrawn"],
  transfer_pending: ["transferred_out", "active"],
  transferred_out: [],
  graduated: ["alumni"],
  withdrawn: [],
  alumni: [],
};

/** Every state of the machine, in the OpenAPI components.schemas order. */
export const ALL_STATES: readonly EnrollmentStatus[] = Object.keys(
  LEGAL_TRANSITIONS,
) as EnrollmentStatus[];

/** States with no outgoing transition (domain.go "terminal"). */
export const TERMINAL_STATES: readonly EnrollmentStatus[] = [
  "transferred_out",
  "withdrawn",
  "alumni",
];

/**
 * Moves the UI double-confirms: the true terminal states PLUS `graduated`
 * (issue #58 asks for a confirm on graduating; server-side `graduated ->
 * alumni` remains legal, so graduation is "near-terminal", not terminal).
 */
export const CONFIRM_TRANSITIONS: readonly EnrollmentStatus[] = [
  "transferred_out",
  "withdrawn",
  "graduated",
  "alumni",
];

/** Legal next states from `from` (empty for terminal states). */
export function nextStates(from: EnrollmentStatus): readonly EnrollmentStatus[] {
  return LEGAL_TRANSITIONS[from] ?? [];
}

/** Whether moving from -> to is legal per the mirrored machine. */
export function canTransition(from: EnrollmentStatus, to: EnrollmentStatus): boolean {
  return nextStates(from).includes(to);
}

export function isTerminal(state: EnrollmentStatus): boolean {
  return (TERMINAL_STATES as readonly string[]).includes(state);
}

/** Whether the UI must ask for confirmation before transitioning to `to`. */
export function needsConfirmation(to: EnrollmentStatus): boolean {
  return (CONFIRM_TRANSITIONS as readonly string[]).includes(to);
}

/** Human labels for badges, filters, and confirm copy. */
export const STATUS_LABELS: Record<EnrollmentStatus, string> = {
  applicant: "Applicant",
  admitted: "Admitted",
  active: "Active",
  suspended: "Suspended",
  transfer_pending: "Transfer pending",
  transferred_out: "Transferred out",
  alumni: "Alumni",
  withdrawn: "Withdrawn",
  graduated: "Graduated",
};

/** Badge tone per state (color is decorative — the label text carries meaning). */
export function statusTone(
  status: EnrollmentStatus,
): "green" | "blue" | "amber" | "red" | "slate" {
  switch (status) {
    case "active":
      return "green";
    case "admitted":
    case "transfer_pending":
      return "blue";
    case "applicant":
    case "suspended":
      return "amber";
    case "withdrawn":
      return "red";
    default:
      return "slate";
  }
}

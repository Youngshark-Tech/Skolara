import { describe, it, expect } from "vitest";
import {
  ALL_STATES,
  canTransition,
  isTerminal,
  needsConfirmation,
  nextStates,
  statusTone,
} from "./enrollment-states";
import type { EnrollmentStatus } from "@/types/api";

/**
 * Pins the UI mirror to services/api/internal/students/domain.go
 * `legalTransitions` VERBATIM (quoted per state below). If a Go change alters
 * the machine, these tests fail until the mirror is updated in the same
 * change — that is the honesty mechanism (#58).
 */
const GO_LEGAL_TRANSITIONS: Record<EnrollmentStatus, EnrollmentStatus[]> = {
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

describe("enrollment state-machine mirror (#58)", () => {
  it("covers exactly the 9 states of the OpenAPI EnrollmentStatus enum", () => {
    expect([...ALL_STATES].sort()).toEqual(
      [
        "applicant",
        "admitted",
        "active",
        "suspended",
        "transfer_pending",
        "transferred_out",
        "alumni",
        "withdrawn",
        "graduated",
      ].sort(),
    );
    expect(ALL_STATES).toHaveLength(9);
  });

  it("matches legalTransitions in domain.go entry-for-entry, order included", () => {
    for (const state of ALL_STATES) {
      expect(nextStates(state), `nextStates(${state})`).toEqual(GO_LEGAL_TRANSITIONS[state]);
    }
  });

  it("treats transferred_out, withdrawn and alumni as terminal (domain.go)", () => {
    expect(isTerminal("transferred_out")).toBe(true);
    expect(isTerminal("withdrawn")).toBe(true);
    expect(isTerminal("alumni")).toBe(true);
    for (const terminal of ["transferred_out", "withdrawn", "alumni"] as const) {
      expect(nextStates(terminal)).toEqual([]);
    }
    // graduated is NOT terminal server-side: graduated -> alumni is legal.
    expect(isTerminal("graduated")).toBe(false);
    expect(canTransition("graduated", "alumni")).toBe(true);
  });

  it("spot-checks the canonical admit path and rejections", () => {
    expect(canTransition("applicant", "admitted")).toBe(true);
    expect(canTransition("admitted", "active")).toBe(true);
    expect(canTransition("active", "transfer_pending")).toBe(true);
    expect(canTransition("transfer_pending", "transferred_out")).toBe(true);
    expect(canTransition("applicant", "active")).toBe(false); // must pass through admitted
    expect(canTransition("suspended", "graduated")).toBe(false);
    expect(canTransition("withdrawn", "active")).toBe(false);
  });

  it("confirms before the career-end moves (#58: terminal states + graduation)", () => {
    expect(needsConfirmation("transferred_out")).toBe(true);
    expect(needsConfirmation("withdrawn")).toBe(true);
    expect(needsConfirmation("graduated")).toBe(true);
    expect(needsConfirmation("alumni")).toBe(true);
    // Everyday lifecycle moves go through without a confirm dialog.
    expect(needsConfirmation("admitted")).toBe(false);
    expect(needsConfirmation("active")).toBe(false);
    expect(needsConfirmation("suspended")).toBe(false);
  });

  it("labels and tones exist for every state", () => {
    for (const state of ALL_STATES) {
      expect(() => statusTone(state)).not.toThrow();
    }
    expect(statusTone("active")).toBe("green");
    expect(statusTone("withdrawn")).toBe("red");
  });
});

/**
 * Field-level error mapping for the enroll-learner flow (#58).
 *
 * The API's error envelope is coarse (code + message); this pure function
 * translates the known students-domain failures into per-field messages so
 * the form can point at WHAT is wrong (ErrAlreadyEnrolled -> the learner
 * picker, ErrValidation -> the offending field) instead of a single opaque
 * banner. See services/api/internal/students/{service,http}.go for the
 * message fragments being matched.
 */
import { ApiError } from "./api";

export interface EnrollFormErrors {
  /** Learner picker (ErrAlreadyEnrolled 409, unknown learner, missing id). */
  learner?: string;
  /** Optional academic year picker (ErrValidation: not a UUID / missing). */
  academicYearId?: string;
  /** Optional class group picker (ErrValidation: not a UUID / missing). */
  classGroupId?: string;
  /** Initial status select (ErrValidation: only applicant|admitted allowed). */
  status?: string;
  /** Form-level fallback (network, unknown validation, server errors). */
  form?: string;
}

export function splitEnrollError(err: unknown): EnrollFormErrors {
  if (!(err instanceof ApiError)) {
    return {
      form: err instanceof Error ? err.message : "Failed to enroll the learner — try again.",
    };
  }

  const message = err.message ?? "";

  // 409 ErrAlreadyEnrolled — the most common enrollment rejection.
  if (err.status === 409 || /already has an open enrollment/i.test(message)) {
    return {
      learner: "This learner already has an open enrollment at this school.",
    };
  }

  if (err.status === 400) {
    if (/learnerId required/i.test(message)) {
      return { learner: "Pick a learner to enroll." };
    }
    if (/learner .* not found/i.test(message)) {
      return { learner: "That learner was not found — search again." };
    }
    if (/classGroupId must be a UUID/i.test(message)) {
      return { classGroupId: "The selected class is invalid — pick another." };
    }
    if (/academicYearId must be a UUID/i.test(message)) {
      return { academicYearId: "The selected academic year is invalid — pick another." };
    }
    if (/classGroupId or academicYearId does not exist/i.test(message)) {
      return { form: "The selected class or academic year does not exist at this school." };
    }
    if (/initial status must be/i.test(message)) {
      return { status: "Initial status must be applicant or admitted." };
    }
    return { form: message || "The enrollment was rejected — check the fields and retry." };
  }

  if (err.status === 404) {
    return { learner: "That learner was not found — search again." };
  }

  if (err.status === 403) {
    return { form: "Your role cannot enroll learners at this school." };
  }

  return { form: message || "Failed to enroll the learner — try again." };
}

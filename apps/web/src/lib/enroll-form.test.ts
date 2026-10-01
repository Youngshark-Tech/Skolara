import { describe, it, expect } from "vitest";
import { ApiError } from "./api";
import { splitEnrollError } from "./enroll-form";

describe("splitEnrollError (#58 field-level enrollment errors)", () => {
  it("maps ErrAlreadyEnrolled (409 conflict) onto the learner picker", () => {
    const err = new ApiError(
      409,
      "conflict",
      "students: learner already has an open enrollment at this school",
    );
    expect(splitEnrollError(err)).toEqual({
      learner: "This learner already has an open enrollment at this school.",
    });
  });

  it("maps missing-learner validation onto the learner field", () => {
    const err = new ApiError(400, "bad_request", "students: validation failed: learnerId required");
    expect(splitEnrollError(err).learner).toBe("Pick a learner to enroll.");
  });

  it("maps unknown learner onto the learner field", () => {
    const err = new ApiError(400, "bad_request", 'students: learner "abc" not found');
    expect(splitEnrollError(err).learner).toContain("not found");
  });

  it("routes class/year UUID validation onto the respective pickers", () => {
    const classErr = new ApiError(
      400,
      "bad_request",
      "students: validation failed: classGroupId must be a UUID",
    );
    expect(splitEnrollError(classErr).classGroupId).toBeTruthy();
    expect(splitEnrollError(classErr).learner).toBeUndefined();

    const yearErr = new ApiError(
      400,
      "bad_request",
      "students: validation failed: academicYearId must be a UUID",
    );
    expect(splitEnrollError(yearErr).academicYearId).toBeTruthy();
  });

  it("keeps initial-status validation on the status field", () => {
    const err = new ApiError(
      400,
      "bad_request",
      "students: validation failed: initial status must be applicant or admitted",
    );
    expect(splitEnrollError(err).status).toBeTruthy();
  });

  it("falls back to a form-level message for unknown and non-API errors", () => {
    const serverErr = new ApiError(500, "internal", "an internal error occurred");
    expect(splitEnrollError(serverErr).form).toBeTruthy();

    const networkErr = new TypeError("Failed to fetch");
    expect(splitEnrollError(networkErr).form).toBe("Failed to fetch");
  });

  it("maps 404 onto the learner field (tenant-enumeration-safe copy)", () => {
    const err = new ApiError(404, "not_found", "resource not found");
    expect(splitEnrollError(err).learner).toContain("not found");
  });
});

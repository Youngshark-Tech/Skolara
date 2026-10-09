/**
 * In-memory store backing the mock transport (issue #142).
 *
 * A module-scoped singleton: it survives client-side navigation (SPA
 * lifecycle) and resets on every full page load — deliberate: a demo always
 * starts from the clean seeded dataset, and no sample data can leak into
 * later sessions. Writes go through these helpers ONLY, so validation and
 * state-machine behavior stay in one auditable place, mirroring
 * services/api/internal/students/{service,repo}.go semantics.
 */
import type { AcademicYear, ClassGroup, Enrollment, EnrollmentStatus, Learner } from "@/types/api";
import { isTerminal, canTransition } from "@/lib/enrollment-states";
import {
  DEMO_ADMIN,
  DEMO_SCHOOL_ID,
  SEED_CLASSES,
  SEED_INVOICES,
  SEED_LEARNERS,
  SEED_WALLET,
  SEED_YEARS,
  seedEnrollments,
  type DemoUser,
  type Invoice,
  type WalletBalance,
} from "./data";

/** Error carrying an API-shaped failure out of the store. */
export class MockApiError extends Error {
  readonly status: number;
  readonly code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "MockApiError";
    this.status = status;
    this.code = code;
  }
}

interface StoreState {
  users: DemoUser[];
  currentUser: DemoUser;
  learners: Learner[];
  enrollments: Enrollment[];
  academicYears: AcademicYear[];
  classGroups: ClassGroup[];
  wallet: WalletBalance[];
  invoices: Invoice[];
  tokenCounter: number;
}

function freshState(): StoreState {
  return {
    users: [DEMO_ADMIN],
    currentUser: DEMO_ADMIN,
    learners: SEED_LEARNERS.map((l) => ({ ...l })),
    enrollments: seedEnrollments(),
    academicYears: SEED_YEARS.map((y) => ({ ...y })),
    classGroups: SEED_CLASSES.map((c) => ({ ...c })),
    wallet: SEED_WALLET.map((w) => ({ ...w })),
    invoices: SEED_INVOICES.map((i) => ({ ...i })),
    tokenCounter: 0,
  };
}

// Module-scoped singleton, replaced wholesale by resetMockStore().
let state: StoreState = freshState();

/** Reset to the pristine seed (used by tests between cases). */
export function resetMockStore(): void {
  state = freshState();
}

// ---------------------------------------------------------------------------
// session

export function currentDemoUser(): DemoUser {
  return state.currentUser;
}

/**
 * Mock login: ANY non-empty credentials are accepted (documented demo
 * behavior) — the display name is derived from the email local part so the
 * shell greeting reflects whoever "signed in". Passwords are never stored.
 */
export function mockLogin(email: unknown, password: unknown): DemoUser {
  if (typeof email !== "string" || !email.includes("@") || email.trim().length === 0) {
    throw new MockApiError(400, "invalid_request", "A valid email is required.");
  }
  if (typeof password !== "string" || password.length === 0) {
    throw new MockApiError(400, "invalid_request", "A password is required.");
  }
  const normalized = email.trim().toLowerCase();
  const existing = state.users.find((u) => u.email === normalized);
  if (existing) {
    state.currentUser = existing;
    return existing;
  }
  const local = normalized.split("@")[0] ?? "user";
  const name = local
    .split(/[._-]+/)
    .filter(Boolean)
    .map((p) => p.charAt(0).toUpperCase() + p.slice(1))
    .join(" ") || "Demo User";
  const user: DemoUser = {
    id: `usr-demo-${state.users.length + 1}`,
    email: normalized,
    name,
    status: "active",
    role: "school_admin",
  };
  state.users.push(user);
  state.currentUser = user;
  return user;
}

/** Mock signup: provisions the (mock) workspace admin, no session issued. */
export function mockSignup(body: unknown): void {
  const b = (body ?? {}) as Record<string, unknown>;
  const email = typeof b.email === "string" ? b.email.trim().toLowerCase() : "";
  const schoolName = typeof b.schoolName === "string" ? b.schoolName.trim() : "";
  const adminName = typeof b.adminName === "string" ? b.adminName.trim() : "";
  const password = typeof b.password === "string" ? b.password : "";
  if (!schoolName || schoolName.length < 2) {
    throw new MockApiError(400, "validation_error", "schoolName must be at least 2 characters.");
  }
  if (!adminName) {
    throw new MockApiError(400, "validation_error", "adminName is required.");
  }
  if (!email.includes("@")) {
    throw new MockApiError(400, "validation_error", "A valid email is required.");
  }
  if (password.length < 8) {
    throw new MockApiError(400, "validation_error", "password must be at least 8 characters.");
  }
  const existing = state.users.find((u) => u.email === email);
  if (existing) {
    throw new MockApiError(409, "email_taken", "That email is already registered.");
  }
  state.users.push({
    id: `usr-demo-${state.users.length + 1}`,
    email,
    name: adminName,
    status: "active",
    role: "school_admin",
  });
  // NOTE: like the real API, signup deliberately issues NO session — the
  // signup form completes onboarding through the standard login flow.
}

export function nextMockToken(): string {
  state.tokenCounter += 1;
  return `mock-access-${state.tokenCounter}`;
}

// ---------------------------------------------------------------------------
// learners

/** Parity with the real API: school lists resolve through enrollments. */
function learnerIsVisible(id: string): boolean {
  return state.enrollments.some((e) => e.learnerId === id);
}

export function listVisibleLearners(params: {
  q?: string;
  limit: number;
  offset: number;
}): { learners: Learner[]; total: number } {
  const visible = state.learners.filter((l) => learnerIsVisible(l.id));
  const q = (params.q ?? "").trim().toLowerCase();
  const filtered = q
    ? visible.filter((l) =>
        `${l.firstName} ${l.middleName ?? ""} ${l.lastName}`.toLowerCase().includes(q),
      )
    : visible;
  const sorted = [...filtered].sort((a, b) => a.createdAt.localeCompare(b.createdAt));
  return {
    learners: sorted.slice(params.offset, params.offset + params.limit).map((l) => ({ ...l })),
    total: sorted.length,
  };
}

export function allLearners(): Learner[] {
  return state.learners.map((l) => ({ ...l }));
}

export function getVisibleLearner(id: string): Learner {
  const found = state.learners.find((l) => l.id === id);
  if (!found || !learnerIsVisible(id)) {
    throw new MockApiError(404, "not_found", "learner not found at this school");
  }
  return { ...found };
}

export function createLearner(body: unknown): Learner {
  const b = (body ?? {}) as Record<string, unknown>;
  const firstName = typeof b.firstName === "string" ? b.firstName.trim() : "";
  const lastName = typeof b.lastName === "string" ? b.lastName.trim() : "";
  if (!firstName || !lastName) {
    throw new MockApiError(400, "validation_error", "firstName and lastName are required.");
  }
  const externalId =
    typeof b.externalId === "string" && b.externalId.trim() !== ""
      ? b.externalId.trim()
      : undefined;
  if (externalId && state.learners.some((l) => l.externalId === externalId)) {
    throw new MockApiError(409, "external_id_taken", "That admission number is already in use.");
  }
  const now = new Date().toISOString();
  const created: Learner = {
    id:
      typeof crypto !== "undefined" && "randomUUID" in crypto
        ? crypto.randomUUID()
        : `lrn-${Date.now()}-${state.learners.length + 1}`,
    firstName,
    lastName,
    externalId,
    createdAt: now,
  };
  state.learners.push(created);
  return { ...created };
}

// ---------------------------------------------------------------------------
// enrollments

export function listEnrollments(params: {
  status?: string;
  limit: number;
  offset: number;
}): { enrollments: Enrollment[]; total: number } {
  const filtered = params.status
    ? state.enrollments.filter((e) => e.status === params.status)
    : state.enrollments;
  const sorted = [...filtered].sort((a, b) => a.startedAt!.localeCompare(b.startedAt!));
  return {
    enrollments: sorted.slice(params.offset, params.offset + params.limit).map((e) => ({ ...e })),
    total: sorted.length,
  };
}

export function createEnrollment(body: unknown): Enrollment {
  const b = (body ?? {}) as Record<string, unknown>;
  const learnerId = typeof b.learnerId === "string" ? b.learnerId : "";
  const status = typeof b.status === "string" ? b.status : "";
  const academicYearId = typeof b.academicYearId === "string" ? b.academicYearId : "";
  const classGroupId = typeof b.classGroupId === "string" ? b.classGroupId : "";

  if (!learnerId) {
    throw new MockApiError(400, "validation_error", "learnerId required.");
  }
  if (!state.learners.some((l) => l.id === learnerId)) {
    throw new MockApiError(400, "validation_error", "learner not found.");
  }
  if (status !== "applicant" && status !== "admitted") {
    throw new MockApiError(400, "validation_error", "initial status must be applicant or admitted.");
  }
  if (academicYearId && !state.academicYears.some((y) => y.id === academicYearId)) {
    throw new MockApiError(400, "validation_error", "academicYearId or classGroupId does not exist.");
  }
  if (classGroupId && !state.classGroups.some((c) => c.id === classGroupId)) {
    throw new MockApiError(400, "validation_error", "academicYearId or classGroupId does not exist.");
  }
  // Open-enrollment guard (students/service.go): ANY non-terminal enrollment
  // at this school blocks a new one — mirrors ErrAlreadyEnrolled -> 409.
  const hasOpen = state.enrollments.some(
    (e) => e.learnerId === learnerId && !isTerminal(e.status),
  );
  if (hasOpen) {
    throw new MockApiError(
      409,
      "already_enrolled",
      "learner already has an open enrollment at this school",
    );
  }
  const now = new Date().toISOString();
  const created: Enrollment = {
    id:
      typeof crypto !== "undefined" && "randomUUID" in crypto
        ? crypto.randomUUID()
        : `enr-${Date.now()}`,
    schoolId: DEMO_SCHOOL_ID,
    learnerId,
    classGroupId: classGroupId || null,
    academicYearId: academicYearId || null,
    status: status as EnrollmentStatus,
    startedAt: now,
    endedAt: null,
    createdAt: now,
  };
  state.enrollments.push(created);
  return { ...created };
}

export function transitionEnrollment(id: string, to: unknown): Enrollment {
  const enrollment = state.enrollments.find((e) => e.id === id);
  if (!enrollment) {
    throw new MockApiError(404, "not_found", "enrollment not found");
  }
  const target = typeof to === "string" ? (to as EnrollmentStatus) : ("" as EnrollmentStatus);
  if (!target) {
    throw new MockApiError(400, "validation_error", "to is required.");
  }
  if (!canTransition(enrollment.status, target)) {
    throw new MockApiError(
      409,
      "illegal_transition",
      `cannot move an enrollment from ${enrollment.status} to ${target}`,
    );
  }
  enrollment.status = target;
  enrollment.endedAt = isTerminal(target) ? new Date().toISOString() : null;
  return { ...enrollment };
}

// ---------------------------------------------------------------------------
// academics / finance (read-only surfaces today)

export function academicYears(): AcademicYear[] {
  return state.academicYears.map((y) => ({ ...y }));
}

export function classGroups(): ClassGroup[] {
  return state.classGroups.map((c) => ({ ...c }));
}

export function wallet(): WalletBalance[] {
  return state.wallet.map((w) => ({ ...w }));
}

export function listInvoices(params: {
  status?: string;
  limit: number;
  offset: number;
}): { invoices: Invoice[]; total: number } {
  const filtered = params.status
    ? state.invoices.filter((i) => i.status === params.status)
    : state.invoices;
  return {
    invoices: filtered
      .slice(params.offset, params.offset + params.limit)
      .map((i) => ({ ...i })),
    total: filtered.length,
  };
}

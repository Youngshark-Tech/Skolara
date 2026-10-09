/**
 * Hand-written contract types for the Skolara API.
 *
 * packages/contracts/openapi.yaml is the source of truth for these shapes
 * (components/schemas: Learner, Enrollment, EnrollmentStatus, AcademicYear,
 * ClassGroup). The generated client package (packages/contracts/generated/)
 * is currently ABSENT from the repo — known issue #62 — so the web app cannot
 * import generated types yet. When #62 lands and the generated directory is
 * restored, delete this file in favor of the generated contract types and
 * re-point imports (they are name-compatible).
 */

/** OpenAPI components.schemas.EnrollmentStatus — the closed 9-state set. */
export type EnrollmentStatus =
  | "applicant"
  | "admitted"
  | "active"
  | "suspended"
  | "transfer_pending"
  | "transferred_out"
  | "alumni"
  | "withdrawn"
  | "graduated";

/** OpenAPI components.schemas.Learner — global identity record (not tenant-scoped). */
export interface Learner {
  id: string;
  firstName: string;
  lastName: string;
  middleName?: string;
  dateOfBirth?: string;
  gender?: string;
  /** School-issued admission number, unique when present. */
  externalId?: string;
  createdAt: string;
}

/** GET /api/v1/learners response envelope (services/api .../students/http.go). */
export interface LearnerPage {
  learners: Learner[];
  total: number;
  limit: number;
  offset: number;
}

/** OpenAPI components.schemas.Enrollment — the tenant binding of a learner. */
export interface Enrollment {
  id: string;
  schoolId: string;
  learnerId: string;
  classGroupId?: string | null;
  academicYearId?: string | null;
  status: EnrollmentStatus;
  startedAt?: string | null;
  endedAt?: string | null;
  createdAt: string;
}

/** GET /api/v1/enrollments response envelope (X-Total-Count also set). */
export interface EnrollmentPage {
  enrollments: Enrollment[];
  limit: number;
  offset: number;
  total: number;
}

/** OpenAPI components.schemas.AcademicYear. */
export interface AcademicYear {
  id: string;
  schoolId: string;
  name: string;
  startDate: string;
  endDate: string;
  status: "planning" | "active" | "closed";
  createdAt: string;
}

/** OpenAPI components.schemas.ClassGroup. */
export interface ClassGroup {
  id: string;
  schoolId: string;
  academicYearId: string;
  name: string;
}

/**
 * Wallet balance by ledger purpose (finance read surface). The demo transport
 * (#142) seeds main/operations/tuition; the real API derives the same shape
 * from the double-entry ledger.
 */
export interface WalletBalance {
  purpose: string;
  balanceMinor: number;
  currency: string;
}

/** GET /api/v1/wallet response envelope. */
export interface WalletResponse {
  wallet: WalletBalance[];
}

/**
 * Invoice in the school's invoice book (demo transport seeds open/paid/void;
 * the production shape grows from the same contract, #179).
 */
export interface Invoice {
  id: string;
  learnerId: string;
  status: "open" | "paid" | "void";
  amountMinor: number;
  currency: string;
  dueDate: string;
}

/** GET /api/v1/invoices response envelope. */
export interface InvoicePage {
  invoices: Invoice[];
  total: number;
  limit: number;
  offset: number;
}

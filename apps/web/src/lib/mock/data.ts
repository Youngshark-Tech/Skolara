/**
 * Demo dataset for the in-memory mock transport (issue #142).
 *
 * Mirrors the API's own demo seed (services/api/internal/demo/demo.go) and
 * extends it so EVERY enrollment state of the lifecycle is visible in the UI.
 * All shapes come from the hand-written contract types (types/api.ts), which
 * mirror packages/contracts/openapi.yaml — the same payloads the real Go API
 * returns. When production connects the database, this module simply stops
 * being used (flag off) — nothing else changes.
 *
 * SECURITY: sample data only — synthetic names, no real people.
 */
import type {
  AcademicYear,
  ClassGroup,
  Enrollment,
  EnrollmentStatus,
  Invoice,
  Learner,
  WalletBalance,
} from "@/types/api";

export const DEMO_SCHOOL_ID = "school-demo-rvs001";

// Contract types live in types/api.ts (openapi.yaml is the source of truth);
// the demo dataset re-exports them so store/router imports stay stable.
export type { Invoice, WalletBalance };

export interface DemoUser {
  id: string;
  email: string;
  name: string;
  status: string;
  role: string;
}

/** The identity the mock session resolves to until a login overrides it. */
export const DEMO_ADMIN: DemoUser = {
  id: "usr-demo-admin",
  email: "admin@skolara.dev",
  name: "Demo School Admin",
  status: "active",
  role: "school_admin",
};

/** Full school-admin permission set — drives the role-aware shell nav. */
export const ADMIN_PERMISSIONS: Record<string, boolean> = {
  "student.read": true,
  "student.manage": true,
  "academics.read": true,
  "academics.manage": true,
  "attendance.record": true,
  "assignment.read": true,
  "assignment.manage": true,
  "finance.read": true,
  "finance.manage": true,
};

type LearnerSeed = Omit<Learner, "createdAt"> & { createdAt: string };

const learner = (
  n: number,
  firstName: string,
  middleName: string,
  lastName: string,
  gender: string,
  dob: string,
): LearnerSeed => ({
  id: `lrn-demo-${String(n).padStart(3, "0")}`,
  firstName,
  middleName,
  lastName,
  gender,
  dateOfBirth: dob,
  externalId: `DEMO-L${String(n).padStart(3, "0")}`,
  createdAt: `2026-01-0${(n % 8) + 1}T08:00:00Z`,
});

export const SEED_LEARNERS: LearnerSeed[] = [
  learner(1, "Amina", "Njeri", "Otieno", "female", "2012-03-14"),
  learner(2, "Brian", "Kiprop", "Mutai", "male", "2012-07-02"),
  learner(3, "Cynthia", "Awuor", "Ochieng", "female", "2011-11-30"),
  learner(4, "Daniel", "Mwangi", "Kamau", "male", "2012-01-19"),
  learner(5, "Esther", "Wanjiku", "Njoroge", "female", "2012-05-08"),
  learner(6, "Faiza", "Abdi", "Noor", "female", "2012-09-21"),
  learner(7, "George", "Kariuki", "Kamau", "male", "2011-06-11"),
  learner(8, "Halima", "Yusuf", "Omar", "female", "2011-02-27"),
  learner(9, "Ian", "Otieno", "Ochieng", "male", "2012-04-03"),
  learner(10, "Joy", "Wambui", "Njoroge", "female", "2012-08-16"),
  learner(11, "Kevin", "Mutua", "Musyoka", "male", "2012-10-05"),
  learner(12, "Lydia", "Chepkemoi", "Rono", "female", "2012-12-24"),
];

export const SEED_YEARS: AcademicYear[] = [
  {
    id: "yr-demo-2025",
    schoolId: DEMO_SCHOOL_ID,
    name: "2025",
    startDate: "2025-01-06",
    endDate: "2025-11-21",
    status: "closed",
    createdAt: "2025-01-02T08:00:00Z",
  },
  {
    id: "yr-demo-2026",
    schoolId: DEMO_SCHOOL_ID,
    name: "2026",
    startDate: "2026-01-05",
    endDate: "2026-11-20",
    status: "active",
    createdAt: "2026-01-02T08:00:00Z",
  },
];

export const SEED_CLASSES: ClassGroup[] = [
  {
    id: "cls-demo-8b",
    schoolId: DEMO_SCHOOL_ID,
    academicYearId: "yr-demo-2026",
    name: "Grade 8 - Blue",
  },
  {
    id: "cls-demo-9g",
    schoolId: DEMO_SCHOOL_ID,
    academicYearId: "yr-demo-2026",
    name: "Grade 9 - Green",
  },
];

/**
 * One enrollment per seeded learner, covering ALL NINE lifecycle states so
 * the enrollments table exercises every badge, filter, and transition affordance.
 */
const SEED_ENROLLMENTS: Array<{
  learner: number;
  status: EnrollmentStatus;
  started: string;
  ended?: string;
  classId?: string;
}> = [
  { learner: 1, status: "active", started: "2026-01-05T08:00:00Z", classId: "cls-demo-8b" },
  { learner: 2, status: "active", started: "2026-01-05T08:05:00Z", classId: "cls-demo-8b" },
  { learner: 3, status: "admitted", started: "2026-02-02T09:00:00Z", classId: "cls-demo-8b" },
  { learner: 4, status: "applicant", started: "2026-02-16T09:00:00Z" },
  { learner: 5, status: "suspended", started: "2026-01-12T08:00:00Z", classId: "cls-demo-8b" },
  { learner: 6, status: "transfer_pending", started: "2026-01-19T08:00:00Z", classId: "cls-demo-9g" },
  { learner: 7, status: "graduated", started: "2025-01-06T08:00:00Z", classId: "cls-demo-9g" },
  { learner: 8, status: "alumni", started: "2024-01-08T08:00:00Z", ended: "2025-11-21T10:00:00Z" },
  { learner: 9, status: "withdrawn", started: "2026-01-07T08:00:00Z", ended: "2026-02-09T10:00:00Z" },
  { learner: 10, status: "transferred_out", started: "2025-05-05T08:00:00Z", ended: "2026-01-30T10:00:00Z" },
  { learner: 11, status: "active", started: "2026-01-06T08:00:00Z", classId: "cls-demo-9g" },
  { learner: 12, status: "admitted", started: "2026-02-23T09:00:00Z", classId: "cls-demo-9g" },
];

export function seedEnrollments(): Enrollment[] {
  return SEED_ENROLLMENTS.map((e, i) => ({
    id: `enr-demo-${String(i + 1).padStart(3, "0")}`,
    schoolId: DEMO_SCHOOL_ID,
    learnerId: `lrn-demo-${String(e.learner).padStart(3, "0")}`,
    classGroupId: e.classId ?? null,
    academicYearId: e.status === "applicant" ? null : "yr-demo-2026",
    status: e.status,
    startedAt: e.started,
    endedAt: e.ended ?? null,
    createdAt: e.started,
  }));
}

export const SEED_WALLET: WalletBalance[] = [
  { purpose: "main", balanceMinor: 128450000, currency: "KES" },
  { purpose: "operations", balanceMinor: 41200000, currency: "KES" },
  { purpose: "tuition", balanceMinor: 96750000, currency: "KES" },
];

export const SEED_INVOICES: Invoice[] = [
  { id: "inv-demo-001", learnerId: "lrn-demo-001", status: "open", amountMinor: 1850000, currency: "KES", dueDate: "2026-03-01" },
  { id: "inv-demo-002", learnerId: "lrn-demo-002", status: "open", amountMinor: 1850000, currency: "KES", dueDate: "2026-03-01" },
  { id: "inv-demo-003", learnerId: "lrn-demo-003", status: "open", amountMinor: 2475000, currency: "KES", dueDate: "2026-03-15" },
  { id: "inv-demo-004", learnerId: "lrn-demo-011", status: "open", amountMinor: 1850000, currency: "KES", dueDate: "2026-04-01" },
  { id: "inv-demo-005", learnerId: "lrn-demo-005", status: "paid", amountMinor: 1850000, currency: "KES", dueDate: "2026-02-01" },
  { id: "inv-demo-006", learnerId: "lrn-demo-009", status: "void", amountMinor: 925000, currency: "KES", dueDate: "2026-02-01" },
];

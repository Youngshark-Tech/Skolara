/**
 * Client-side permission model. The API resolves the authoritative permission
 * set server-side (/api/v1/me); these helpers only gate UI affordances —
 * never enforce anything (§ security: server decides).
 */

export interface Me {
  id: string;
  email: string;
  name: string;
  status: string;
  roles: string[];
  permissions: Record<string, boolean>;
}

export interface Membership {
  userId: string;
  schoolId: string;
  role: string;
  status: string;
}

/** can reports whether the session holds the named permission. */
export function can(me: Me | null | undefined, permission: string): boolean {
  return me?.permissions?.[permission] === true;
}

/** canAny reports whether ANY of the permissions is held. */
export function canAny(me: Me | null | undefined, permissions: string[]): boolean {
  return permissions.some((p) => can(me, p));
}

/** Navigation entries for the role-aware shell. */
export interface NavEntry {
  href: string;
  label: string;
  /** Any one of these permissions makes the entry visible. */
  anyPermission: string[];
}

/**
 * Navigation entries for the role-aware shell.
 *
 * Gating notes (#57):
 * - Attendance gates on `attendance.record` ONLY (least privilege): the
 *   workspace's single action is recording roll calls, so a read-only auditor
 *   gets no nav entry (the page gate is aligned in attendance/page.tsx).
 * - Assignments has no screen yet — the entry is still listed so the role can
 *   discover the coming-soon placeholder instead of the feature being silent.
 * - Enrollments shares the student permission pair: the API guards the
 *   enrollment list with student.read and mutations with student.manage
 *   (services/api/internal/students/http.go).
 */
export const NAV_ENTRIES: NavEntry[] = [
  { href: "/", label: "Command Center", anyPermission: [] },
  { href: "/students", label: "Students", anyPermission: ["student.read", "student.manage"] },
  { href: "/enrollments", label: "Enrollments", anyPermission: ["student.read", "student.manage"] },
  { href: "/academics", label: "Academics", anyPermission: ["academics.read", "academics.manage"] },
  { href: "/attendance", label: "Attendance", anyPermission: ["attendance.record"] },
  { href: "/assignments", label: "Assignments", anyPermission: ["assignment.read", "assignment.manage"] },
  { href: "/finance", label: "Finance", anyPermission: ["finance.read", "finance.manage"] },
];

/** visibleNav filters navigation by the session's permissions. */
export function visibleNav(me: Me | null | undefined): NavEntry[] {
  return NAV_ENTRIES.filter((e) => e.anyPermission.length === 0 || canAny(me, e.anyPermission));
}

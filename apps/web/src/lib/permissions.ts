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

export const NAV_ENTRIES: NavEntry[] = [
  { href: "/", label: "Command Center", anyPermission: [] },
  { href: "/students", label: "Students", anyPermission: ["student.read", "student.manage"] },
  { href: "/academics", label: "Academics", anyPermission: ["academics.read", "academics.manage"] },
  { href: "/attendance", label: "Attendance", anyPermission: ["attendance.read", "attendance.record"] },
  { href: "/finance", label: "Finance", anyPermission: ["finance.read", "finance.manage"] },
];

/** visibleNav filters navigation by the session's permissions. */
export function visibleNav(me: Me | null | undefined): NavEntry[] {
  return NAV_ENTRIES.filter((e) => e.anyPermission.length === 0 || canAny(me, e.anyPermission));
}

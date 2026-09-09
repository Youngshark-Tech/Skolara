import { describe, it, expect } from "vitest";
import { can, canAny, visibleNav, type Me } from "./permissions";

function meWith(permissions: Record<string, boolean>): Me {
  return {
    id: "u1",
    email: "u@school.example",
    name: "User",
    status: "active",
    roles: [],
    permissions,
  };
}

describe("permission gating", () => {
  it("can() reflects the server-resolved permission set", () => {
    const me = meWith({ "student.read": true });
    expect(can(me, "student.read")).toBe(true);
    expect(can(me, "finance.manage")).toBe(false);
  });

  it("can() denies for a logged-out session", () => {
    expect(can(null, "student.read")).toBe(false);
  });

  it("canAny() needs at least one match", () => {
    const me = meWith({ "attendance.record": true });
    expect(canAny(me, ["attendance.read", "attendance.record"])).toBe(true);
    expect(canAny(me, ["finance.read", "finance.manage"])).toBe(false);
  });

  it("visibleNav() hides entries the role cannot use and keeps the dashboard", () => {
    const teacher = meWith({ "attendance.record": true, "academics.read": true });
    const nav = visibleNav(teacher).map((n) => n.href);
    expect(nav).toContain("/");
    expect(nav).toContain("/attendance");
    expect(nav).toContain("/academics");
    expect(nav).not.toContain("/finance");

    const registrar = meWith({ "student.manage": true, "finance.manage": true });
    expect(visibleNav(registrar).map((n) => n.href)).toContain("/finance");

    const lockedOut = meWith({});
    expect(visibleNav(lockedOut).map((n) => n.href)).toEqual(["/"]);
  });
});

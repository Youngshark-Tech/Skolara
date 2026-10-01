import { describe, it, expect } from "vitest";
import { safeNextPath } from "./next-path";

describe("safeNextPath (#57 login ?next= sanitization)", () => {
  it("honors plain internal paths", () => {
    expect(safeNextPath("/students")).toBe("/students");
    expect(safeNextPath("/enrollments?offset=20")).toBe("/enrollments?offset=20");
    expect(safeNextPath("/")).toBe("/");
  });

  it("rejects absent values", () => {
    expect(safeNextPath(null)).toBeNull();
    expect(safeNextPath(undefined)).toBeNull();
    expect(safeNextPath("")).toBeNull();
  });

  it("rejects protocol-relative and backslash-smuggled cross-origin targets", () => {
    expect(safeNextPath("//evil.example")).toBeNull();
    expect(safeNextPath("/\\evil.example")).toBeNull();
  });

  it("rejects absolute URLs and schemes", () => {
    expect(safeNextPath("https://evil.example")).toBeNull();
    expect(safeNextPath("javascript:alert(1)")).toBeNull();
    expect(safeNextPath("mailto:ops@school.example")).toBeNull();
  });

  it("rejects control characters and oversized values", () => {
    expect(safeNextPath("/students\r\nSet-Cookie: x=1")).toBeNull();
    expect(safeNextPath(`/${"a".repeat(600)}`)).toBeNull();
  });
});

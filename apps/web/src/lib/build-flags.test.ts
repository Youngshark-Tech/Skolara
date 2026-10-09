import { describe, expect, it } from "vitest";
import { resolveDemoModeFlag } from "./build-flags.mjs";

/**
 * Pins the zero-config demo default (issue #153): the flag is injected by
 * next.config.mjs ONLY when the operator expressed no preference. The strict
 * parsing of the value itself lives in lib/auth-bypass.ts (covered there) —
 * this layer only decides what ABSENT means.
 */
describe("resolveDemoModeFlag (#153)", () => {
  it("defaults to demo ON when the flag is unset", () => {
    expect(resolveDemoModeFlag(undefined)).toBe("true");
  });

  it("treats an empty value as unset (cleared Vercel vars arrive as empty strings)", () => {
    expect(resolveDemoModeFlag("")).toBe("true");
  });

  it("passes an explicit true through unchanged", () => {
    expect(resolveDemoModeFlag("true")).toBe("true");
  });

  it("passes an explicit false through — the go-live switch", () => {
    expect(resolveDemoModeFlag("false")).toBe("false");
  });

  it("passes any other explicit value through verbatim (downstream parsing stays strict)", () => {
    expect(resolveDemoModeFlag("TRUE")).toBe("TRUE");
    expect(resolveDemoModeFlag("1")).toBe("1");
    expect(resolveDemoModeFlag("true ")).toBe("true ");
  });
});

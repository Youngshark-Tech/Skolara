import { describe, it, expect, vi } from "vitest";
import {
  authBypassEnabled,
  bypassCredentials,
  mockDataEnabled,
  DEFAULT_BYPASS_EMAIL,
  DEFAULT_BYPASS_PASSWORD,
} from "./auth-bypass";

describe("authBypassEnabled (#141)", () => {
  it("is off unless the flag is exactly the string true", () => {
    expect(authBypassEnabled({})).toBe(false);
    expect(authBypassEnabled({ NEXT_PUBLIC_AUTH_BYPASS: undefined })).toBe(false);
    expect(authBypassEnabled({ NEXT_PUBLIC_AUTH_BYPASS: "" })).toBe(false);
    expect(authBypassEnabled({ NEXT_PUBLIC_AUTH_BYPASS: "1" })).toBe(false);
    expect(authBypassEnabled({ NEXT_PUBLIC_AUTH_BYPASS: "TRUE" })).toBe(false);
    expect(authBypassEnabled({ NEXT_PUBLIC_AUTH_BYPASS: "true " })).toBe(false);
  });

  it("turns on only for the literal true", () => {
    expect(authBypassEnabled({ NEXT_PUBLIC_AUTH_BYPASS: "true" })).toBe(true);
  });
});

describe("bypassCredentials (#141)", () => {
  it("defaults to the documented demo pair", () => {
    expect(bypassCredentials({})).toEqual({
      email: DEFAULT_BYPASS_EMAIL,
      password: DEFAULT_BYPASS_PASSWORD,
    });
  });

  it("honors explicit overrides (deployments seeded with SKOLARA_DEMO_PASSWORD)", () => {
    expect(
      bypassCredentials({
        NEXT_PUBLIC_BYPASS_EMAIL: "owner@school.example",
        NEXT_PUBLIC_BYPASS_PASSWORD: "another-pass-123",
      }),
    ).toEqual({ email: "owner@school.example", password: "another-pass-123" });
  });

  it("treats empty overrides as absent so the defaults still apply", () => {
    expect(
      bypassCredentials({ NEXT_PUBLIC_BYPASS_EMAIL: "", NEXT_PUBLIC_BYPASS_PASSWORD: "" }),
    ).toEqual({
      email: DEFAULT_BYPASS_EMAIL,
      password: DEFAULT_BYPASS_PASSWORD,
    });
  });
});

describe("build-time inlining regression (caught by the #142 browser E2E)", () => {
  it("prefers the statically inlined build value over the runtime env shim", async () => {
    // Import a FRESH copy of the module while the build-time expression sees
    // "true" — mirroring what the Next compiler inlines into client bundles.
    vi.stubEnv("NEXT_PUBLIC_DEMO_MODE", "true");
    vi.resetModules();
    const withBuild = await import("./auth-bypass");
    expect(withBuild.mockDataEnabled()).toBe(true);

    // Now flip the runtime process.env — the build constant must WIN. In the
    // browser this is the empty process.env shim: a dynamic lookup used to
    // read undefined here and silently disable the whole mode.
    vi.stubEnv("NEXT_PUBLIC_DEMO_MODE", "false");
    expect(withBuild.mockDataEnabled()).toBe(true);
    vi.unstubAllEnvs();
    vi.resetModules();
  });

  it("still reads the env parameter when no build value was inlined (tests)", () => {
    expect(mockDataEnabled({ NEXT_PUBLIC_DEMO_MODE: "true" })).toBe(true);
    expect(mockDataEnabled({})).toBe(false);
  });
});

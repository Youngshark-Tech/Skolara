import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
  apiFetch,
  ApiError,
  getAccessToken,
  getSchoolId,
  refreshSession,
  resolveApiUrl,
  setAccessToken,
  setOnSessionExpired,
  setSchoolId,
} from "./api";

const fetchMock = vi.fn();
vi.stubGlobal("fetch", fetchMock);

function jsonResponse(status: number, body: unknown) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  };
}

const session = { accessToken: "fresh", tokenType: "Bearer", expiresIn: 900 };

describe("apiFetch", () => {
  beforeEach(() => {
    fetchMock.mockReset();
    localStorage.clear();
    setAccessToken(null);
    setSchoolId(null);
    setOnSessionExpired(null);
  });

  afterEach(() => {
    setOnSessionExpired(null);
  });

  it("attaches the bearer token and tenant header", async () => {
    setAccessToken("tok-123");
    setSchoolId("school-1");
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { ok: true }));

    await apiFetch("/api/v1/learners");

    const [, init] = fetchMock.mock.calls[0];
    expect(init.headers.Authorization).toBe("Bearer tok-123");
    expect(init.headers["X-School-ID"]).toBe("school-1");
  });

  it("retries once through the refresh flow on 401", async () => {
    setAccessToken("stale");
    fetchMock
      .mockResolvedValueOnce(jsonResponse(401, { error: { code: "unauthorized" } }))
      .mockResolvedValueOnce(jsonResponse(200, session))
      .mockResolvedValueOnce(jsonResponse(200, { ok: true }));

    const result = await apiFetch<{ ok: boolean }>("/api/v1/me");

    expect(result.ok).toBe(true);
    expect(getAccessToken()).toBe("fresh");
    // call 1: original, call 2: refresh, call 3: retried original
    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(fetchMock.mock.calls[1][0]).toContain("/api/v1/auth/refresh");
  });

  it("coalesces concurrent 401s into exactly one refresh call (single-flight)", async () => {
    setAccessToken("stale");
    fetchMock
      .mockResolvedValueOnce(jsonResponse(401, { error: { code: "unauthorized" } }))
      .mockResolvedValueOnce(jsonResponse(401, { error: { code: "unauthorized" } }))
      .mockResolvedValueOnce(jsonResponse(200, session))
      .mockResolvedValueOnce(jsonResponse(200, { a: true }))
      .mockResolvedValueOnce(jsonResponse(200, { b: true }));

    const [first, second] = await Promise.all([
      apiFetch<{ a: boolean }>("/api/v1/one"),
      apiFetch<{ b: boolean }>("/api/v1/two"),
    ]);

    expect(first).toEqual({ a: true });
    expect(second).toEqual({ b: true });
    expect(getAccessToken()).toBe("fresh");

    const refreshCalls = fetchMock.mock.calls.filter((call) =>
      String(call[0]).includes("/api/v1/auth/refresh"),
    );
    // Two parallel 401s must share ONE refresh: a second refresh would present
    // the already-rotated token and trip family revocation (#54).
    expect(refreshCalls).toHaveLength(1);
    expect(fetchMock).toHaveBeenCalledTimes(5);
  });

  it("clears token + school context and notifies onSessionExpired when the refresh cookie is rejected", async () => {
    setAccessToken("stale");
    setSchoolId("school-1");
    const expired = vi.fn();
    setOnSessionExpired(expired);
    fetchMock
      .mockResolvedValueOnce(
        jsonResponse(401, { error: { code: "unauthorized", message: "expired" } }),
      )
      .mockResolvedValueOnce(jsonResponse(401, { error: { code: "unauthorized" } }));

    await expect(apiFetch("/api/v1/me")).rejects.toMatchObject({
      status: 401,
      code: "unauthorized",
    });
    expect(getAccessToken()).toBeNull();
    expect(getSchoolId()).toBeNull();
    expect(expired).toHaveBeenCalledTimes(1);
  });

  it("does not declare the session expired when the refresh fails for network reasons", async () => {
    setAccessToken("stale");
    setSchoolId("school-1");
    const expired = vi.fn();
    setOnSessionExpired(expired);
    fetchMock
      .mockResolvedValueOnce(jsonResponse(401, { error: { code: "unauthorized" } }))
      .mockRejectedValueOnce(new TypeError("Failed to fetch"));

    await expect(apiFetch("/api/v1/me")).rejects.toMatchObject({ status: 401 });
    // Transient failure: the provider must NOT be told the session is over,
    // and the tenant preference survives for the retry.
    expect(expired).not.toHaveBeenCalled();
    expect(getSchoolId()).toBe("school-1");
  });

  it("maps error envelopes into ApiError with code and status", async () => {
    setAccessToken("tok");
    fetchMock.mockResolvedValueOnce(
      jsonResponse(409, { error: { code: "conflict", message: "duplicate open enrollment" } }),
    );

    const err = await apiFetch("/api/v1/enrollments", {
      method: "POST",
      body: {},
      noRetry: true,
    }).catch((e: unknown) => e);

    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).status).toBe(409);
    expect((err as ApiError).code).toBe("conflict");
    expect((err as ApiError).message).toBe("duplicate open enrollment");
  });
});

describe("token storage (#55)", () => {
  beforeEach(() => {
    fetchMock.mockReset();
    localStorage.clear();
    setAccessToken(null);
    setSchoolId(null);
  });

  it("keeps the access token in memory only", () => {
    setAccessToken("tok-123");
    setSchoolId("school-1");
    expect(getAccessToken()).toBe("tok-123");
    // Stronger than a key-name check: the ONLY persisted value is the
    // non-secret school preference — no credential ever touches storage.
    expect(window.localStorage.length).toBe(1);
    expect(window.localStorage.getItem("skolara_school_id")).toBe("school-1");
  });

  it("still persists the non-secret school preference", () => {
    setSchoolId("school-1");
    expect(window.localStorage.getItem("skolara_school_id")).toBe("school-1");
  });
});

describe("refreshSession", () => {
  beforeEach(() => {
    fetchMock.mockReset();
    setAccessToken(null);
  });

  it("shares one in-flight refresh between concurrent callers", async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, session));

    const [first, second, third] = await Promise.all([
      refreshSession(),
      refreshSession(),
      refreshSession(),
    ]);

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(first.ok && second.ok && third.ok).toBe(true);
    if (first.ok) expect(first.session.accessToken).toBe("fresh");
  });

  it("normalizes an unauthenticated refresh into ok:false (no throw)", async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse(401, { error: { code: "unauthorized" } }),
    );

    const outcome = await refreshSession();

    expect(outcome).toEqual({ ok: false, reason: "unauthenticated" });
  });

  it("normalizes network failure into ok:false instead of a raw TypeError (#54)", async () => {
    fetchMock.mockRejectedValue(new TypeError("Failed to fetch"));

    const outcome = await refreshSession();

    expect(outcome).toEqual({ ok: false, reason: "network" });
  });
});

describe("resolveApiUrl (#127)", () => {
  it("an explicitly set URL always wins in production", () => {
    expect(resolveApiUrl("https://api.example.com", true)).toBe(
      "https://api.example.com",
    );
  });

  it("an explicitly set EMPTY string wins — same-origin opt-in", () => {
    expect(resolveApiUrl("", true)).toBe("");
    expect(resolveApiUrl("", false)).toBe("");
  });

  it("production without an explicit value defaults to same-origin", () => {
    expect(resolveApiUrl(undefined, true)).toBe("");
  });

  it("development without an explicit value keeps the local Go server", () => {
    expect(resolveApiUrl(undefined, false)).toBe("http://localhost:8080");
  });
});

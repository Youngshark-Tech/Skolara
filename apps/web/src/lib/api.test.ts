import { describe, it, expect, vi, beforeEach } from "vitest";
import { apiFetch, ApiError, setAccessToken, setSchoolId, getAccessToken } from "./api";

const fetchMock = vi.fn();
vi.stubGlobal("fetch", fetchMock);

function jsonResponse(status: number, body: unknown) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  };
}

describe("apiFetch", () => {
  beforeEach(() => {
    fetchMock.mockReset();
    localStorage.clear();
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
      .mockResolvedValueOnce(jsonResponse(200, { accessToken: "fresh", tokenType: "Bearer", expiresIn: 900 }))
      .mockResolvedValueOnce(jsonResponse(200, { ok: true }));

    const result = await apiFetch<{ ok: boolean }>("/api/v1/me");

    expect(result.ok).toBe(true);
    expect(getAccessToken()).toBe("fresh");
    // call 1: original, call 2: refresh, call 3: retried original
    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(fetchMock.mock.calls[1][0]).toContain("/api/v1/auth/refresh");
  });

  it("clears the token and surfaces the error envelope on final 401", async () => {
    setAccessToken("stale");
    setSchoolId(null);
    fetchMock
      .mockResolvedValueOnce(jsonResponse(401, { error: { code: "unauthorized", message: "expired" } }))
      .mockResolvedValueOnce(jsonResponse(401, { error: { code: "unauthorized" } }));

    await expect(apiFetch("/api/v1/me")).rejects.toMatchObject({
      status: 401,
      code: "unauthorized",
    });
    expect(getAccessToken()).toBeNull();
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

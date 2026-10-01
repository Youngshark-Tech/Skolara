import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, cleanup, fireEvent } from "@testing-library/react";
import { SessionProvider, useSession } from "./session";
import { getAccessToken, getSchoolId, setAccessToken, setSchoolId } from "./api";

const fetchMock = vi.fn();
vi.stubGlobal("fetch", fetchMock);

const routerReplace = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: routerReplace, push: vi.fn() }),
}));

function jsonResponse(status: number, body: unknown) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  };
}

const me = {
  id: "u1",
  email: "u@school.example",
  name: "User",
  status: "active",
  roles: ["admin"],
  permissions: { "student.read": true },
};
const memberships = [
  { userId: "u1", schoolId: "school-1", role: "admin", status: "active" },
  { userId: "u1", schoolId: "school-2", role: "teacher", status: "active" },
];

function SessionProbe() {
  const { me, loading, error, activeSchoolId, logout, switchSchool } = useSession();
  return (
    <div>
      <span data-testid="loading">{String(loading)}</span>
      <span data-testid="me">{me ? me.email : "none"}</span>
      <span data-testid="error">{error ?? ""}</span>
      <span data-testid="school">{activeSchoolId ?? ""}</span>
      <button data-testid="logout" onClick={() => void logout()}>
        sign out
      </button>
      <button data-testid="switch" onClick={() => switchSchool("school-2")}>
        switch school
      </button>
    </div>
  );
}

function clearHintCookie() {
  document.cookie = "skolara_auth_hint=; Path=/; Max-Age=0";
}

beforeEach(() => {
  fetchMock.mockReset();
  routerReplace.mockReset();
  localStorage.clear();
  clearHintCookie();
  setAccessToken(null);
  setSchoolId(null);
});

afterEach(() => {
  cleanup();
});

describe("SessionProvider boot (ADR-011)", () => {
  it("rebuilds the session from a silent refresh and restores the school context", async () => {
    fetchMock
      .mockResolvedValueOnce(
        jsonResponse(200, { accessToken: "boot", tokenType: "Bearer", expiresIn: 900 }),
      )
      .mockResolvedValueOnce(jsonResponse(200, me))
      .mockResolvedValueOnce(jsonResponse(200, memberships));

    render(
      <SessionProvider>
        <SessionProbe />
      </SessionProvider>,
    );

    await waitFor(() => expect(screen.getByTestId("loading").textContent).toBe("false"));
    expect(screen.getByTestId("me").textContent).toBe("u@school.example");
    expect(screen.getByTestId("school").textContent).toBe("school-1");
    expect(getAccessToken()).toBe("boot");
    // First fetch must be the silent refresh (memory-only token model).
    expect(fetchMock.mock.calls[0][0]).toContain("/api/v1/auth/refresh");
    expect(localStorage.getItem("skolara_school_id")).toBe("school-1");
    // Middleware hint cookie present for subsequent navigations.
    expect(document.cookie).toContain("skolara_auth_hint=1");
  });

  it("treats a bootstrap network blip as an explicit error state, not a silent logout", async () => {
    fetchMock.mockRejectedValue(new TypeError("Failed to fetch"));

    render(
      <SessionProvider>
        <SessionProbe />
      </SessionProvider>,
    );

    await waitFor(() =>
      expect(screen.getByTestId("error").textContent).toContain("Cannot reach the server"),
    );
    expect(screen.getByTestId("loading").textContent).toBe("false");
    // One silent retry means the refresh endpoint was hit twice.
    const refreshCalls = fetchMock.mock.calls.filter((call) =>
      String(call[0]).includes("/api/v1/auth/refresh"),
    );
    expect(refreshCalls).toHaveLength(2);
  });

  it("lands in the logged-out state when no refresh cookie is present", async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse(401, { error: { code: "unauthorized" } }),
    );

    render(
      <SessionProvider>
        <SessionProbe />
      </SessionProvider>,
    );

    await waitFor(() => expect(screen.getByTestId("loading").textContent).toBe("false"));
    expect(screen.getByTestId("me").textContent).toBe("none");
    expect(screen.getByTestId("error").textContent).toBe("");
    expect(getAccessToken()).toBeNull();
  });
});

describe("SessionProvider teardown", () => {
  it("logout clears token, persisted school id, hint cookie and provider state", async () => {
    // Boot with a token already in memory: no silent refresh needed.
    setAccessToken("tok");
    fetchMock
      .mockResolvedValueOnce(jsonResponse(200, me))
      .mockResolvedValueOnce(jsonResponse(200, memberships))
      .mockResolvedValueOnce({ ok: true, status: 204, json: async () => null });

    render(
      <SessionProvider>
        <SessionProbe />
      </SessionProvider>,
    );
    await waitFor(() => expect(screen.getByTestId("me").textContent).toBe("u@school.example"));
    expect(document.cookie).toContain("skolara_auth_hint=1");

    fireEvent.click(screen.getByTestId("logout"));

    await waitFor(() => expect(screen.getByTestId("me").textContent).toBe("none"));
    expect(getAccessToken()).toBeNull();
    expect(getSchoolId()).toBeNull();
    expect(localStorage.getItem("skolara_school_id")).toBeNull();
    expect(document.cookie).not.toContain("skolara_auth_hint=1");
  });

  it("switchSchool swaps the tenant in-context — a pure state update, no reload burst (#57)", async () => {
    setAccessToken("tok");
    fetchMock
      .mockResolvedValueOnce(jsonResponse(200, me))
      .mockResolvedValueOnce(jsonResponse(200, memberships));

    render(
      <SessionProvider>
        <SessionProbe />
      </SessionProvider>,
    );
    await waitFor(() => expect(screen.getByTestId("school").textContent).toBe("school-1"));

    fireEvent.click(screen.getByTestId("switch"));

    await waitFor(() => expect(screen.getByTestId("school").textContent).toBe("school-2"));
    // Only the boot's two calls: switching is a context refetch (pages key
    // their queries on activeSchoolId), NOT a page reload or re-login.
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(localStorage.getItem("skolara_school_id")).toBe("school-2");
  });
});

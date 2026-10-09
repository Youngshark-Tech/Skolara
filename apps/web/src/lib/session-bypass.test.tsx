import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, cleanup } from "@testing-library/react";
import { SessionProvider, useSession } from "./session";
import { setAccessToken, setSchoolId } from "./api";

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
  email: "admin@skolara.dev",
  name: "Demo School Admin",
  status: "active",
  roles: [],
  permissions: { "student.read": true },
};
const memberships = [
  { userId: "u1", schoolId: "school-1", role: "school_admin", status: "active" },
];

function SessionProbe() {
  const { me, loading, error } = useSession();
  return (
    <div>
      <span data-testid="loading">{String(loading)}</span>
      <span data-testid="me">{me ? me.email : "none"}</span>
      <span data-testid="error">{error ?? ""}</span>
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
  vi.unstubAllEnvs();
  cleanup();
});

describe("SessionProvider boot × open-access mode (#141)", () => {
  it("auto-logs-in with the documented demo credentials when the refresh cookie is rejected", async () => {
    vi.stubEnv("NEXT_PUBLIC_AUTH_BYPASS", "true");
    fetchMock
      .mockResolvedValueOnce(jsonResponse(401, { error: { code: "unauthorized" } }))
      .mockResolvedValueOnce(
        jsonResponse(200, { accessToken: "demo-token", tokenType: "Bearer", expiresIn: 900 }),
      )
      .mockResolvedValueOnce(jsonResponse(200, me))
      .mockResolvedValueOnce(jsonResponse(200, memberships));

    render(
      <SessionProvider>
        <SessionProbe />
      </SessionProvider>,
    );

    await waitFor(() =>
      expect(screen.getByTestId("loading").textContent).toBe("false"),
    );
    expect(screen.getByTestId("me").textContent).toBe("admin@skolara.dev");
    expect(screen.getByTestId("error").textContent).toBe("");

    // The automatic login must hit the real login endpoint with the demo pair.
    const loginCall = fetchMock.mock.calls.find(
      (c) => typeof c[0] === "string" && c[0].includes("/api/v1/auth/login"),
    );
    expect(loginCall).toBeDefined();
    expect(JSON.parse(loginCall![1].body)).toEqual({
      email: "admin@skolara.dev",
      password: "SkolaraDemo!2026",
    });
  });

  it("keeps the normal logged-out behavior when the flag is off (no login attempt)", async () => {
    vi.stubEnv("NEXT_PUBLIC_AUTH_BYPASS", "false");
    fetchMock.mockResolvedValueOnce(
      jsonResponse(401, { error: { code: "unauthorized" } }),
    );

    render(
      <SessionProvider>
        <SessionProbe />
      </SessionProvider>,
    );

    await waitFor(() =>
      expect(screen.getByTestId("loading").textContent).toBe("false"),
    );
    expect(screen.getByTestId("me").textContent).toBe("none");
    expect(screen.getByTestId("error").textContent).toBe("");
    expect(
      fetchMock.mock.calls.some(
        (c) => typeof c[0] === "string" && c[0].includes("/api/v1/auth/login"),
      ),
    ).toBe(false);
  });

  it("surfaces the real API error when the automatic demo login fails (seed not enabled)", async () => {
    vi.stubEnv("NEXT_PUBLIC_AUTH_BYPASS", "true");
    fetchMock
      .mockResolvedValueOnce(jsonResponse(401, { error: { code: "unauthorized" } }))
      .mockResolvedValueOnce(
        jsonResponse(401, {
          error: { code: "invalid_credentials", message: "email or password is incorrect" },
        }),
      );

    render(
      <SessionProvider>
        <SessionProbe />
      </SessionProvider>,
    );

    await waitFor(() =>
      expect(screen.getByTestId("loading").textContent).toBe("false"),
    );
    expect(screen.getByTestId("me").textContent).toBe("none");
    expect(screen.getByTestId("error").textContent).toContain(
      "Automatic demo sign-in failed: email or password is incorrect",
    );
  });

  it("boots entirely from the in-memory demo transport when NEXT_PUBLIC_DEMO_MODE=true — zero network calls", async () => {
    vi.stubEnv("NEXT_PUBLIC_DEMO_MODE", "true");
    // Deliberately NO fetch mocks: if the boot tried to reach the network at
    // all, the un-rigged fetchMock (vi.fn() returning undefined) would break
    // the boot — proving demo mode never leaves the browser (#142).
    render(
      <SessionProvider>
        <SessionProbe />
      </SessionProvider>,
    );

    await waitFor(
      () => expect(screen.getByTestId("loading").textContent).toBe("false"),
      { timeout: 4000 },
    );
    expect(screen.getByTestId("me").textContent).toBe("admin@skolara.dev");
    expect(screen.getByTestId("error").textContent).toBe("");
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

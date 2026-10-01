import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, cleanup, fireEvent, within } from "@testing-library/react";
import EnrollmentsPage from "./page";

const { sessionValue } = vi.hoisted(() => ({
  sessionValue: {
    me: {
      id: "u1",
      email: "u@school.example",
      name: "Registrar",
      status: "active",
      roles: ["admin"],
      permissions: { "student.read": true, "student.manage": true },
    },
    memberships: [{ userId: "u1", schoolId: "school-1", role: "admin", status: "active" }],
    activeSchoolId: "school-1",
    loading: false,
    error: null,
    login: async () => {},
    logout: async () => {},
    switchSchool: () => {},
    reload: async () => {},
  },
}));

vi.mock("@/lib/session", () => ({
  useSession: () => sessionValue,
}));

vi.mock("next/navigation", () => ({
  usePathname: () => "/enrollments",
  useRouter: () => ({ push: vi.fn(), replace: vi.fn(), refresh: vi.fn() }),
  useSearchParams: () => new URLSearchParams(window.location.search),
}));

const fetchMock = vi.fn();
vi.stubGlobal("fetch", fetchMock);

function jsonResponse(status: number, body: unknown, headers: Record<string, string> = {}) {
  return {
    ok: status >= 200 && status < 300,
    status,
    headers: new Headers(headers),
    json: async () => body,
  };
}

const learnersFixture = [
  {
    id: "learner-1-aaaa",
    firstName: "Ada",
    lastName: "Lovelace",
    createdAt: "2026-01-05T00:00:00Z",
  },
  {
    id: "learner-2-bbbb",
    firstName: "Grace",
    lastName: "Hopper",
    createdAt: "2026-01-06T00:00:00Z",
  },
];

const enrollmentsFixture = [
  {
    id: "enr-1",
    schoolId: "school-1",
    learnerId: "learner-1-aaaa",
    status: "active",
    startedAt: "2026-02-01T00:00:00Z",
    endedAt: null,
    createdAt: "2026-02-01T00:00:00Z",
  },
  {
    id: "enr-2",
    schoolId: "school-1",
    learnerId: "learner-2-bbbb",
    status: "withdrawn",
    startedAt: "2026-02-01T00:00:00Z",
    endedAt: "2026-03-01T00:00:00Z",
    createdAt: "2026-02-01T00:00:00Z",
  },
];

function routeFetch(url: string, init?: { method?: string; body?: string }) {
  const parsed = new URL(url);
  const method = init?.method ?? "GET";
  if (parsed.pathname === "/api/v1/enrollments" && method === "GET") {
    // Body total deliberately disagrees with the header: the pager must honor
    // X-Total-Count (#58).
    return jsonResponse(
      200,
      { enrollments: enrollmentsFixture, total: 99, limit: 20, offset: 0 },
      { "X-Total-Count": "7" },
    );
  }
  if (parsed.pathname === "/api/v1/learners" && method === "GET") {
    return jsonResponse(200, { learners: learnersFixture, total: 2, limit: 100, offset: 0 });
  }
  if (parsed.pathname === "/api/v1/learners/learner-1-aaaa" && method === "GET") {
    return jsonResponse(200, learnersFixture[0]);
  }
  // The API's known visibility gap: a just-created learner 404s on
  // GET /learners/{id} until its first enrollment lands (#58 deep link).
  if (parsed.pathname === "/api/v1/learners/learner-ghost" && method === "GET") {
    return jsonResponse(404, { error: { code: "not_found", message: "learner not found" } });
  }
  if (parsed.pathname === "/api/v1/enrollments/enr-1/transition" && method === "POST") {
    const body = JSON.parse(init?.body ?? "{}") as { to?: string };
    return jsonResponse(200, { ...enrollmentsFixture[0], status: body.to });
  }
  if (parsed.pathname === "/api/v1/enrollments" && method === "POST") {
    return jsonResponse(201, {
      id: "enr-3",
      schoolId: "school-1",
      learnerId: (JSON.parse(init?.body ?? "{}") as { learnerId: string }).learnerId,
      status: "admitted",
      createdAt: "2026-04-01T00:00:00Z",
    });
  }
  return jsonResponse(404, {
    error: { code: "not_found", message: `unmatched ${method} ${parsed.pathname}` },
  });
}

beforeEach(() => {
  fetchMock.mockReset();
  fetchMock.mockImplementation(routeFetch);
  window.history.replaceState(null, "", "/enrollments");
});

afterEach(() => {
  cleanup();
});

function callsTo(pattern: RegExp) {
  return fetchMock.mock.calls.filter((call) => pattern.test(String(call[0])));
}

describe("EnrollmentsView (#58)", () => {
  it("renders the roster with learner names, status badges and X-Total-Count pagination", async () => {
    render(<EnrollmentsPage />);

    await waitFor(() => expect(screen.getByText("Ada Lovelace")).toBeTruthy());
    expect(screen.getByText("Grace Hopper")).toBeTruthy();
    // Status badges live in the table (filter chips reuse the same labels).
    const table = within(screen.getByRole("table"));
    expect(table.getByText("Active")).toBeTruthy();
    const graceRow = table.getByText("Grace Hopper").closest("tr");
    expect(graceRow && within(graceRow).getByText("Withdrawn")).toBeTruthy();
    // Pagination honors the X-Total-Count header (7), not the body total (99);
    // the range end is clamped to min(page size, total).
    expect(screen.getByText("1–7 of 7")).toBeTruthy();
  });

  it("offers only LEGAL next states per row and nothing for terminal rows", async () => {
    render(<EnrollmentsPage />);
    await waitFor(() => expect(screen.getByText("Ada Lovelace")).toBeTruthy());

    const table = within(screen.getByRole("table"));
    // domain.go: active -> suspended | transfer_pending | graduated | withdrawn | alumni
    expect(table.getByRole("button", { name: "Suspended" })).toBeTruthy();
    expect(table.getByRole("button", { name: "Transfer pending" })).toBeTruthy();
    expect(table.getByRole("button", { name: "Graduated" })).toBeTruthy();
    expect(table.getByRole("button", { name: "Withdrawn" })).toBeTruthy();
    expect(table.getByRole("button", { name: "Alumni" })).toBeTruthy();

    // withdrawn is terminal: no transition buttons are rendered for it.
    expect(table.getByText("No further moves")).toBeTruthy();
  });

  it("confirms before a career-end move and only then POSTs the transition", async () => {
    render(<EnrollmentsPage />);
    await waitFor(() => expect(screen.getByText("Ada Lovelace")).toBeTruthy());

    const before = callsTo(/\/transition$/).length;
    fireEvent.click(within(screen.getByRole("table")).getByRole("button", { name: "Withdrawn" }));

    // Confirmation first: the inline confirm UI (Confirm/Cancel) renders and
    // NO transition request has been made yet.
    expect(screen.getByRole("button", { name: "Confirm" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeTruthy();
    expect(callsTo(/\/transition$/).length).toBe(before);

    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));

    await waitFor(() =>
      expect(screen.getByRole("status").textContent).toContain("moved to Withdrawn"),
    );
    const transitionCalls = callsTo(/\/transition$/);
    expect(transitionCalls.length).toBe(before + 1);
    const [, init] = transitionCalls[transitionCalls.length - 1];
    expect(init?.body).toBe(JSON.stringify({ to: "withdrawn" }));
  });

  it("filters by status chips with aria-pressed state", async () => {
    render(<EnrollmentsPage />);
    await waitFor(() => expect(screen.getByText("Ada Lovelace")).toBeTruthy());

    const activeChip = screen.getByRole("button", { name: "Active" });
    expect(activeChip.getAttribute("aria-pressed")).toBe("false");
    fireEvent.click(activeChip);

    await waitFor(() =>
      expect(callsTo(/status=active/).length).toBeGreaterThan(0),
    );
    expect(screen.getByRole("button", { name: "Active" }).getAttribute("aria-pressed")).toBe(
      "true",
    );
  });

  it("deep-links ?enroll=<id> into the enroll flow with the learner preselected", async () => {
    window.history.replaceState(null, "", "/enrollments?enroll=learner-1-aaaa");
    render(<EnrollmentsPage />);

    await waitFor(() => expect(screen.getByText(/Selected: Ada Lovelace/)).toBeTruthy());
    // The preselect resolved the learner by id through the school-scoped API.
    expect(callsTo(/\/api\/v1\/learners\/learner-1-aaaa$/).length).toBe(1);
  });

  it("keeps the admit story alive when the deep-linked learner is not listed yet (API gap)", async () => {
    window.history.replaceState(null, "", "/enrollments?enroll=learner-ghost");
    render(<EnrollmentsPage />);

    // Honest note instead of a dead end, and the id from our own deep link
    // stays preselected so the enrollment can proceed.
    await waitFor(() => expect(screen.getByText(/Selected: New learner/)).toBeTruthy());
    expect(screen.getByText(/isn't listed yet/)).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Enroll learner" }));
    await waitFor(() =>
      expect(screen.getByRole("status").textContent).toContain("enrolled"),
    );
    const postCall = callsTo(/\/api\/v1\/enrollments$/).find((call) => call[1]?.method === "POST");
    expect(JSON.parse(String(postCall?.[1]?.body)).learnerId).toBe("learner-ghost");
  });

  it("enrolls a picked learner via POST /api/v1/enrollments and announces success", async () => {
    render(<EnrollmentsPage />);
    await waitFor(() => expect(screen.getByRole("button", { name: "Enroll a learner" })).toBeTruthy());

    fireEvent.click(screen.getByRole("button", { name: "Enroll a learner" }));

    const radio = await screen.findByRole("radio", { name: /Ada Lovelace/ });
    fireEvent.click(radio);
    fireEvent.click(screen.getByRole("button", { name: "Enroll learner" }));

    await waitFor(() =>
      expect(callsTo(/\/api\/v1\/enrollments$/).some((call) => call[1]?.method === "POST")).toBe(
        true,
      ),
    );
    const postCall = callsTo(/\/api\/v1\/enrollments$/).find((call) => call[1]?.method === "POST");
    expect(JSON.parse(String(postCall?.[1]?.body)).learnerId).toBe("learner-1-aaaa");
    expect(JSON.parse(String(postCall?.[1]?.body)).status).toBe("admitted");

    // Panel closes and a role="status" notice announces the enrollment.
    await waitFor(() => expect(screen.getByRole("status").textContent).toContain("enrolled"));
  });
});

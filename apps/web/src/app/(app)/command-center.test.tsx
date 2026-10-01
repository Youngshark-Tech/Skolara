import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, cleanup, fireEvent } from "@testing-library/react";
import { CommandCenter } from "./command-center";

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

const fetchMock = vi.fn();
vi.stubGlobal("fetch", fetchMock);

function jsonResponse(status: number, body: unknown) {
  return {
    ok: status >= 200 && status < 300,
    status,
    headers: new Headers(),
    json: async () => body,
  };
}

const learnerCountCalls = () =>
  fetchMock.mock.calls.filter((call) => String(call[0]).endsWith("/api/v1/learners?limit=1"));

beforeEach(() => {
  fetchMock.mockReset();
  localStorage.clear();
  localStorage.setItem("skolara_school_id", "school-1");
});

afterEach(() => {
  cleanup();
});

describe("CommandCenter (#57 stat craft)", () => {
  it("refetches the Learners stat after a quick-admit instead of freezing on the stale value", async () => {
    fetchMock.mockImplementation((_url: string, init?: { method?: string }) => {
      if (init?.method === "POST") {
        return jsonResponse(201, {
          id: "learner-new",
          firstName: "Ada",
          lastName: "Lovelace",
          createdAt: "2026-04-01T00:00:00Z",
        });
      }
      return jsonResponse(200, { learners: [], total: 41, limit: 1, offset: 0 });
    });

    render(<CommandCenter />);
    await waitFor(() => expect(screen.getByText("41")).toBeTruthy());
    expect(learnerCountCalls()).toHaveLength(1);

    fireEvent.change(screen.getByLabelText("First name"), { target: { value: "Ada" } });
    fireEvent.change(screen.getByLabelText("Last name"), { target: { value: "Lovelace" } });
    fireEvent.click(screen.getByRole("button", { name: "Create learner" }));

    await waitFor(() => expect(learnerCountCalls().length).toBeGreaterThanOrEqual(2));
    // The admit story (#58): the success notice deep-links into the enroll flow.
    const enrollLink = await screen.findByRole("link", { name: "Enroll Ada →" });
    expect(enrollLink.getAttribute("href")).toBe("/enrollments?enroll=learner-new");
  });

  it("shows a failed stat load as an error with retry — never as an empty school", async () => {
    fetchMock.mockImplementation((_url: string, init?: { method?: string }) => {
      if (init?.method === "POST") {
        return jsonResponse(201, {
          id: "learner-new",
          firstName: "Ada",
          lastName: "Lovelace",
          createdAt: "2026-04-01T00:00:00Z",
        });
      }
      return jsonResponse(500, { error: { code: "internal", message: "boom" } });
    });

    render(<CommandCenter />);
    await waitFor(() =>
      expect(screen.getByText(/Learner count failed to load/)).toBeTruthy(),
    );
    expect(screen.getByRole("button", { name: "Retry" })).toBeTruthy();

    // Retry heals the stat: error note disappears, the count renders.
    fetchMock.mockImplementation(() =>
      jsonResponse(200, { learners: [], total: 7, limit: 1, offset: 0 }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(screen.getByText("7")).toBeTruthy());
    expect(screen.queryByText(/Learner count failed to load/)).toBeNull();
  });
});

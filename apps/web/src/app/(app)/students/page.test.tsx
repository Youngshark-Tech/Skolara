import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, cleanup, fireEvent } from "@testing-library/react";
import StudentsPage from "./page";

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

const searchCalls = (q: string) =>
  fetchMock.mock.calls.filter((call) => String(call[0]).includes(`q=${q}`));

beforeEach(() => {
  fetchMock.mockReset();
  localStorage.clear();
  localStorage.setItem("skolara_school_id", "school-1");
});

afterEach(() => {
  cleanup();
});

describe("StudentsPage (#57 search + pagination craft)", () => {
  it("debounces the roster search: one request per settled query, none per keystroke", async () => {
    fetchMock.mockImplementation(() =>
      jsonResponse(200, { learners: [], total: 0, limit: 20, offset: 0 }),
    );

    render(<StudentsPage />);
    // Initial page load fires exactly once, without a query.
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));

    // Two keystrokes inside the debounce window: only the SETTLED query may
    // ever reach the API (out-of-order stale responses used to win the race).
    const search = screen.getByLabelText("Search learners by name");
    fireEvent.change(search, { target: { value: "g" } });
    fireEvent.change(search, { target: { value: "gr" } });
    expect(searchCalls("g")).toHaveLength(0); // nothing fired synchronously

    await waitFor(() => expect(searchCalls("gr")).toHaveLength(1), { timeout: 2000 });
    // Exactly ONE search request went out in total — the superseded
    // intermediate query ("g") was never sent at all.
    expect(fetchMock.mock.calls.filter((c) => String(c[0]).includes("q="))).toHaveLength(1);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("renders an empty roster honestly (0 learners) instead of a 1–0 of 0 range", async () => {
    fetchMock.mockImplementation(() =>
      jsonResponse(200, { learners: [], total: 0, limit: 20, offset: 0 }),
    );

    render(<StudentsPage />);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));

    expect(screen.getByText("0 learners")).toBeTruthy(); // pager span
    expect(screen.getByText("No learners found")).toBeTruthy();
    expect(screen.queryByText(/1–0 of 0/)).toBeNull();
  });

  it("clamps the pager range to the total (1–7 of 7, not 1–20 of 7)", async () => {
    fetchMock.mockImplementation(() =>
      jsonResponse(200, { learners: [], total: 7, limit: 20, offset: 0 }),
    );

    render(<StudentsPage />);
    await waitFor(() => expect(screen.getByText("1–7 of 7")).toBeTruthy());
  });
});

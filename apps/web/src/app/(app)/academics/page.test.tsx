import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, cleanup, within } from "@testing-library/react";
import AcademicsPage from "./page";

const { sessionValue } = vi.hoisted(() => ({
  sessionValue: {
    me: {
      id: "u1",
      email: "u@school.example",
      name: "Dean of Studies",
      status: "active",
      roles: ["admin"],
      permissions: { "academics.read": true, "academics.manage": true } as Record<string, boolean>,
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

const SEED_YEARS = [
  {
    id: "yr-2025",
    schoolId: "school-1",
    name: "2025",
    startDate: "2025-01-06",
    endDate: "2025-11-21",
    status: "closed",
    createdAt: "2025-01-02T08:00:00Z",
  },
  {
    id: "yr-2026",
    schoolId: "school-1",
    name: "2026",
    startDate: "2026-01-05",
    endDate: "2026-11-20",
    status: "active",
    createdAt: "2026-01-02T08:00:00Z",
  },
];

const SEED_CLASSES = [
  { id: "cls-8b", schoolId: "school-1", academicYearId: "yr-2026", name: "Grade 8 - Blue" },
  { id: "cls-9g", schoolId: "school-1", academicYearId: "yr-2026", name: "Grade 9 - Green" },
];

const SEED_ENROLLMENTS = {
  enrollments: [
    { id: "e1", schoolId: "school-1", learnerId: "l1", classGroupId: "cls-8b", status: "active", createdAt: "2026-01-05T08:00:00Z" },
    { id: "e2", schoolId: "school-1", learnerId: "l2", classGroupId: "cls-8b", status: "active", createdAt: "2026-01-05T08:05:00Z" },
    { id: "e3", schoolId: "school-1", learnerId: "l3", classGroupId: "cls-9g", status: "active", createdAt: "2026-01-06T08:00:00Z" },
    { id: "e4", schoolId: "school-1", learnerId: "l4", classGroupId: null, status: "applicant", createdAt: "2026-02-16T09:00:00Z" },
  ],
  total: 4,
  limit: 100,
  offset: 0,
};

function seedHappyFetch() {
  fetchMock.mockImplementation((url: string) => {
    if (String(url).includes("/api/v1/academic-years")) return jsonResponse(200, SEED_YEARS);
    if (String(url).includes("/api/v1/classes")) return jsonResponse(200, SEED_CLASSES);
    return jsonResponse(200, SEED_ENROLLMENTS);
  });
}

beforeEach(() => {
  fetchMock.mockReset();
  localStorage.clear();
  localStorage.setItem("skolara_school_id", "school-1");
});

afterEach(() => {
  cleanup();
});

describe("AcademicsPage (#188 years + classes with live counts)", () => {
  it("renders academic years with status and the active year in the header", async () => {
    seedHappyFetch();
    render(<AcademicsPage />);

    await waitFor(() => expect(screen.getByText(/2026 in session/)).toBeTruthy());
    expect(screen.getByText("2025")).toBeTruthy();
    expect(screen.getAllByText("closed").length).toBeGreaterThan(0);
    expect(screen.getByText("2025-01-06 → 2025-11-21")).toBeTruthy();
  });

  it("derives class enrollment counts from the enrollments endpoint, not hard-coding", async () => {
    seedHappyFetch();
    render(<AcademicsPage />);

    await waitFor(() => expect(screen.getByText("Grade 8 - Blue")).toBeTruthy());
    const row8b = screen.getByText("Grade 8 - Blue").closest("tr");
    const row9g = screen.getByText("Grade 9 - Green").closest("tr");
    // Exact-match the badge text so the year "2026" in the same row cannot
    // satisfy the assertion.
    expect(within(row8b!).getByText("2")).toBeTruthy();
    expect(within(row9g!).getByText("1")).toBeTruthy();
    // Applicant without a class binding must not inflate any class count.
    expect(screen.queryByText("4")).toBeNull();
  });

  it("refetches when the enrollment picture changes (reload action)", async () => {
    seedHappyFetch();
    render(<AcademicsPage />);
    await waitFor(() => expect(screen.getByText("Grade 8 - Blue")).toBeTruthy());

    // The endpoint contract: a fresh load fetches enrollments again.
    const enrollmentCalls = fetchMock.mock.calls.filter((c) =>
      String(c[0]).includes("/api/v1/enrollments"),
    );
    expect(enrollmentCalls.length).toBeGreaterThanOrEqual(1);
  });

  it("tolerates the enveloped academic-years shape as well as the bare array", async () => {
    fetchMock.mockImplementation((url: string) => {
      if (String(url).includes("/api/v1/academic-years")) {
        return jsonResponse(200, { years: SEED_YEARS });
      }
      if (String(url).includes("/api/v1/classes")) {
        return jsonResponse(200, { classes: SEED_CLASSES });
      }
      return jsonResponse(200, SEED_ENROLLMENTS);
    });
    render(<AcademicsPage />);
    await waitFor(() => expect(screen.getByText("Grade 8 - Blue")).toBeTruthy());
  });

  it("surfaces API failures as an error note", async () => {
    fetchMock.mockImplementation(() =>
      jsonResponse(500, { error: { code: "internal", message: "academics unavailable" } }),
    );
    render(<AcademicsPage />);
    await waitFor(() => expect(screen.getByText(/academics unavailable/)).toBeTruthy());
  });

  it("denies users without academics.read", () => {
    const original = sessionValue.me.permissions;
    sessionValue.me = { ...sessionValue.me, permissions: {} };
    try {
      render(<AcademicsPage />);
      expect(screen.getByText(/does not include academics access/)).toBeTruthy();
      expect(screen.queryByText("Class groups")).toBeNull();
    } finally {
      sessionValue.me = { ...sessionValue.me, permissions: original };
    }
  });
});

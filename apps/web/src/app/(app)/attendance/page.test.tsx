import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, cleanup, fireEvent, within } from "@testing-library/react";
import AttendancePage from "./page";

const { sessionValue } = vi.hoisted(() => ({
  sessionValue: {
    me: {
      id: "u1",
      email: "u@school.example",
      name: "Class Teacher",
      status: "active",
      roles: ["admin"],
      permissions: { "attendance.record": true } as Record<string, boolean>,
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

const SEED_CLASSES = [
  { id: "cls-8b", schoolId: "school-1", academicYearId: "yr-2026", name: "Grade 8 - Blue" },
  { id: "cls-9g", schoolId: "school-1", academicYearId: "yr-2026", name: "Grade 9 - Green" },
];

const SEED_ROSTER = {
  learners: [
    { id: "lrn-1", firstName: "Amina", lastName: "Otieno", createdAt: "2026-01-01T00:00:00Z" },
    { id: "lrn-2", firstName: "Brian", lastName: "Mutai", createdAt: "2026-01-02T00:00:00Z" },
  ],
  total: 2,
  limit: 100,
  offset: 0,
};

const SEED_ENROLLMENTS = {
  enrollments: [
    { id: "e1", schoolId: "school-1", learnerId: "lrn-1", classGroupId: "cls-8b", status: "active", createdAt: "2026-01-05T08:00:00Z" },
    { id: "e2", schoolId: "school-1", learnerId: "lrn-2", classGroupId: "cls-8b", status: "admitted", createdAt: "2026-01-05T08:05:00Z" },
    { id: "e3", schoolId: "school-1", learnerId: "lrn-3", classGroupId: "cls-8b", status: "withdrawn", createdAt: "2026-01-07T08:00:00Z" },
    { id: "e4", schoolId: "school-1", learnerId: "lrn-4", classGroupId: null, status: "applicant", createdAt: "2026-02-16T09:00:00Z" },
  ],
  total: 4,
  limit: 100,
  offset: 0,
};

const SEED_REGISTER = {
  date: "2026-03-06",
  classGroupId: "cls-8b",
  records: [{ learnerId: "lrn-1", status: "present", recordedAt: "2026-03-06T07:55:00Z" }],
  total: 1,
};

interface RegisterBody {
  date: string;
  classGroupId: string;
  records: Array<{ learnerId: string; status: string; recordedAt: string }>;
  total: number;
}

/** Route fetches by URL shape: attendance GET/POST keyed by method. */
function seedAttendanceFetch(registerBody: RegisterBody = SEED_REGISTER, postBody: RegisterBody = { ...SEED_REGISTER, records: [], total: 0 }) {
  fetchMock.mockImplementation((url: string, init?: { method?: string }) => {
    const u = String(url);
    if (u.includes("/api/v1/classes")) return jsonResponse(200, SEED_CLASSES);
    if (u.includes("/api/v1/enrollments")) return jsonResponse(200, SEED_ENROLLMENTS);
    if (u.includes("/api/v1/learners")) return jsonResponse(200, SEED_ROSTER);
    if (u.includes("/api/v1/attendance")) {
      return init?.method === "POST"
        ? jsonResponse(200, postBody)
        : jsonResponse(200, registerBody);
    }
    return jsonResponse(404, { error: { code: "not_found", message: "unexpected fetch" } });
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

describe("AttendancePage (#189 roll call)", () => {
  it("renders the class roster with saved marks and unmarked learners honestly", async () => {
    seedAttendanceFetch();
    render(<AttendancePage />);

    await waitFor(() => expect(screen.getByText("Amina Otieno")).toBeTruthy());
    expect(screen.getByText("Brian Mutai")).toBeTruthy();
    // Withdrawn learner 3 and unbound applicant 4 never appear on the register.
    expect(screen.queryByText(/ghost/i)).toBeNull();

    // Amina: saved present → the present chip is aria-pressed.
    const aminaGroup = screen.getByRole("group", { name: "Attendance mark for Amina Otieno" });
    expect(aminaGroup.getAttribute("aria-label")).toBeTruthy();
    const presentChip = within(aminaGroup).getByRole("button", { name: "present" });
    expect(presentChip.getAttribute("aria-pressed")).toBe("true");

    // Brian: no saved record → unmarked (no chip pressed).
    const brianGroup = screen.getByRole("group", { name: "Attendance mark for Brian Mutai" });
    const pressed = within(brianGroup).queryAllByRole("button", { pressed: true });
    expect(pressed).toHaveLength(0);
  });

  it("marks a learner, flags unsaved changes, saves, and confirms the register", async () => {
    const postRegister = {
      ...SEED_REGISTER,
      records: [
        { learnerId: "lrn-1", status: "present", recordedAt: "2026-03-06T07:55:00Z" },
        { learnerId: "lrn-2", status: "late", recordedAt: "2026-03-06T08:00:00Z" },
      ],
      total: 2,
    };
    seedAttendanceFetch(SEED_REGISTER, postRegister);
    render(<AttendancePage />);

    await waitFor(() => expect(screen.getByText("Brian Mutai")).toBeTruthy());
    expect(screen.getByText(/All changes saved/)).toBeTruthy();

    fireEvent.click(
      within(screen.getByRole("group", { name: "Attendance mark for Brian Mutai" })).getByRole(
        "button",
        { name: "late" },
      ),
    );
    expect(screen.getByText(/Unsaved changes/)).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Save register" }));

    await waitFor(() => expect(screen.getByText(/Register saved: 2 learners marked/)).toBeTruthy());
    const postCall = fetchMock.mock.calls.find(
      (c) => String(c[0]).includes("/api/v1/attendance") && c[1]?.method === "POST",
    );
    expect(postCall).toBeTruthy();
    const postedBody = JSON.parse(String(postCall![1]?.body));
    // The workspace defaults the date to "today" (local) — shape-check it and
    // assert the parts the flow controls.
    expect(postedBody.date).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    expect(postedBody.classGroupId).toBe("cls-8b");
    // The register saves as a whole (idempotent upsert): the previously saved
    // mark re-sends alongside the fresh one.
    expect(postedBody.entries).toEqual([
      { learnerId: "lrn-1", status: "present" },
      { learnerId: "lrn-2", status: "late" },
    ]);
    // After save the dirty flag clears.
    expect(screen.getByText(/All changes saved/)).toBeTruthy();
  });

  it("surfaces the mock's 409 not_enrolled message when a save fails", async () => {
    fetchMock.mockImplementation((url: string, init?: { method?: string }) => {
      const u = String(url);
      if (u.includes("/api/v1/classes")) return jsonResponse(200, SEED_CLASSES);
      if (u.includes("/api/v1/enrollments")) return jsonResponse(200, SEED_ENROLLMENTS);
      if (u.includes("/api/v1/learners")) return jsonResponse(200, SEED_ROSTER);
      if (u.includes("/api/v1/attendance") && init?.method === "POST") {
        return jsonResponse(409, {
          error: { code: "not_enrolled", message: "learner is not enrolled in this class group" },
        });
      }
      return jsonResponse(200, SEED_REGISTER);
    });
    render(<AttendancePage />);
    await waitFor(() => expect(screen.getByText("Amina Otieno")).toBeTruthy());

    fireEvent.click(
      within(screen.getByRole("group", { name: "Attendance mark for Brian Mutai" })).getByRole(
        "button",
        { name: "present" },
      ),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save register" }));

    await waitFor(() =>
      expect(screen.getByText(/learner is not enrolled in this class group/)).toBeTruthy(),
    );
  });

  it("shows the empty-roster state for a class with no placed learners", async () => {
    seedAttendanceFetch();
    render(<AttendancePage />);
    await waitFor(() => expect(screen.getByText("Grade 9 - Green")).toBeTruthy());

    fireEvent.change(screen.getByLabelText("Class"), { target: { value: "cls-9g" } });
    await waitFor(() =>
      expect(screen.getByText(/No learners are placed in this class yet/)).toBeTruthy(),
    );
  });

  it("denies users without attendance.record", () => {
    const original = sessionValue.me.permissions;
    sessionValue.me = { ...sessionValue.me, permissions: {} };
    try {
      render(<AttendancePage />);
      expect(screen.getByText(/does not include attendance access/)).toBeTruthy();
      expect(screen.queryByText("Roll call")).toBeNull();
    } finally {
      sessionValue.me = { ...sessionValue.me, permissions: original };
    }
  });
});

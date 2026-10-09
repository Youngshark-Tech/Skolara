import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, cleanup, fireEvent, within } from "@testing-library/react";
import AssignmentsPage from "./page";

const { sessionValue } = vi.hoisted(() => ({
  sessionValue: {
    me: {
      id: "u1",
      email: "u@school.example",
      name: "Subject Teacher",
      status: "active",
      roles: ["admin"],
      permissions: { "assignment.read": true, "assignment.manage": true } as Record<string, boolean>,
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

const OVERDUE_DATE = "2026-01-15";
const FUTURE_DATE = "2099-06-30";

let book: Array<{
  id: string;
  title: string;
  description?: string;
  classGroupId: string;
  subject?: string;
  dueDate: string;
  createdAt: string;
}>;

function seedAssignmentsFetch() {
  fetchMock.mockImplementation((url: string, init?: { method?: string; body?: string }) => {
    const u = String(url);
    if (u.includes("/api/v1/classes")) return jsonResponse(200, SEED_CLASSES);
    if (u.includes("/api/v1/assignments")) {
      if (init?.method === "POST") {
        const body = JSON.parse(String(init.body));
        const created = {
          id: `asg-new-${book.length + 1}`,
          title: body.title as string,
          classGroupId: body.classGroupId as string,
          subject: body.subject as string | undefined,
          description: body.description as string | undefined,
          dueDate: body.dueDate as string,
          createdAt: "2026-10-09T08:00:00Z",
        };
        book.push(created);
        return jsonResponse(201, created);
      }
      const url_ = new URL(u, "http://local");
      const classFilter = url_.searchParams.get("classGroupId") ?? "";
      const statusFilter = url_.searchParams.get("status") ?? "all";
      const today = new Date().toISOString().slice(0, 10);
      let rows = book.filter((a) => !classFilter || a.classGroupId === classFilter);
      if (statusFilter === "overdue") rows = rows.filter((a) => a.dueDate < today);
      if (statusFilter === "open") rows = rows.filter((a) => a.dueDate >= today);
      rows = [...rows].sort((a, b) => a.dueDate.localeCompare(b.dueDate));
      return jsonResponse(200, { assignments: rows, total: rows.length, limit: 50, offset: 0 });
    }
    return jsonResponse(404, { error: { code: "not_found", message: "unexpected fetch" } });
  });
}

beforeEach(() => {
  fetchMock.mockReset();
  localStorage.clear();
  localStorage.setItem("skolara_school_id", "school-1");
  book = [
    {
      id: "asg-1",
      title: "Algebra worksheet 4",
      classGroupId: "cls-8b",
      subject: "Mathematics",
      dueDate: OVERDUE_DATE,
      createdAt: "2026-01-01T09:00:00Z",
    },
    {
      id: "asg-2",
      title: "Physics problem set: forces",
      classGroupId: "cls-9g",
      subject: "Physics",
      dueDate: FUTURE_DATE,
      createdAt: "2026-01-02T09:00:00Z",
    },
  ];
});

afterEach(() => {
  cleanup();
});

describe("AssignmentsPage (#190 work book)", () => {
  it("renders the work book with derived overdue/open badges", async () => {
    seedAssignmentsFetch();
    render(<AssignmentsPage />);

    await waitFor(() => expect(screen.getByText("Algebra worksheet 4")).toBeTruthy());
    const row = screen.getByText("Algebra worksheet 4").closest("tr");
    expect(within(row!).getByText("overdue")).toBeTruthy();
    expect(within(row!).getByText("Mathematics")).toBeTruthy();

    const physicsRow = screen.getByText("Physics problem set: forces").closest("tr");
    expect(within(physicsRow!).getByText("open")).toBeTruthy();
  });

  it("publishes an assignment through the form and it lands in the book", async () => {
    seedAssignmentsFetch();
    render(<AssignmentsPage />);
    await waitFor(() => expect(screen.getByText("Algebra worksheet 4")).toBeTruthy());

    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Reading log week 6" } });
    fireEvent.change(screen.getByLabelText("Class", { selector: "#assignment-class" }), {
      target: { value: "cls-9g" },
    });
    fireEvent.change(screen.getByLabelText("Subject (optional)"), {
      target: { value: "English" },
    });
    fireEvent.change(screen.getByLabelText("Due date"), { target: { value: FUTURE_DATE } });
    fireEvent.click(screen.getByRole("button", { name: "Publish assignment" }));

    await waitFor(() => expect(screen.getByText(/Reading log week 6.*published/)).toBeTruthy());
    await waitFor(() => expect(screen.getByText("Reading log week 6")).toBeTruthy());
    expect(book).toHaveLength(3);
  });

  it("surfaces the mock's validation error for an invalid publish", async () => {
    fetchMock.mockImplementation((url: string, init?: { method?: string }) => {
      if (String(url).includes("/api/v1/classes")) return jsonResponse(200, SEED_CLASSES);
      if (String(url).includes("/api/v1/assignments") && init?.method === "POST") {
        return jsonResponse(400, {
          error: {
            code: "validation_error",
            message: "title must be at least 3 characters.",
          },
        });
      }
      return jsonResponse(200, { assignments: [], total: 0, limit: 50, offset: 0 });
    });
    render(<AssignmentsPage />);
    await waitFor(() => expect(screen.getByRole("button", { name: "Publish assignment" })).toBeTruthy());

    // The mock 400s every POST in this case — use a form-valid title so the
    // browser's constraint validation doesn't swallow the submit; the point
    // under test is the honest surfacing of the API error.
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Valid title" } });
    fireEvent.change(screen.getByLabelText("Class", { selector: "#assignment-class" }), {
      target: { value: "cls-9g" },
    });
    fireEvent.change(screen.getByLabelText("Due date"), { target: { value: FUTURE_DATE } });
    fireEvent.click(screen.getByRole("button", { name: "Publish assignment" }));

    await waitFor(() =>
      expect(screen.getByText(/title must be at least 3 characters/)).toBeTruthy(),
    );
  });

  it("applies the class filter as a query param", async () => {
    seedAssignmentsFetch();
    render(<AssignmentsPage />);
    await waitFor(() => expect(screen.getByText("Algebra worksheet 4")).toBeTruthy());

    fireEvent.change(screen.getByLabelText("Class", { selector: "#assignment-class-filter" }), {
      target: { value: "cls-9g" },
    });

    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(
          (c) => String(c[0]).includes("/api/v1/assignments") && String(c[0]).includes("classGroupId=cls-9g"),
        ),
      ).toBeTruthy(),
    );
  });

  it("denies users without assignment.read", () => {
    const original = sessionValue.me.permissions;
    sessionValue.me = { ...sessionValue.me, permissions: {} };
    try {
      render(<AssignmentsPage />);
      expect(screen.getByText(/does not include assignment access/)).toBeTruthy();
      expect(screen.queryByText("Work book")).toBeNull();
    } finally {
      sessionValue.me = { ...sessionValue.me, permissions: original };
    }
  });
});

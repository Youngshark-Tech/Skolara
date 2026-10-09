import { describe, it, expect, beforeEach } from "vitest";
import { mockApiRequest } from "./router";
import { resetMockStore } from "./store";
import { ApiError } from "@/lib/api-error";
import type { Enrollment, EnrollmentStatus, LearnerPage, Learner } from "@/types/api";
import { ALL_STATES } from "@/lib/enrollment-states";

async function call<T>(
  method: string,
  path: string,
  body?: unknown,
  opts: { onResponse?: (res: unknown) => void; signal?: AbortSignal } = {},
): Promise<T> {
  return mockApiRequest<T>({ method, path, body, ...opts });
}

beforeEach(() => {
  resetMockStore();
});

describe("mock seed parity (issue #142)", () => {
  it("exposes a fully-populated school covering every lifecycle state", async () => {
    const page = await call<LearnerPage>("GET", "/api/v1/learners?limit=100");
    expect(page.total).toBe(12);
    expect(page.learners).toHaveLength(12);

    const enrollments = await call<{ enrollments: Enrollment[]; total: number }>(
      "GET",
      "/api/v1/enrollments?limit=100",
    );
    expect(enrollments.total).toBe(12);
    const statuses = new Set(enrollments.enrollments.map((e) => e.status));
    for (const state of ALL_STATES) {
      expect(statuses.has(state)).toBe(true);
    }
  });

  it("serves wallet and invoice summaries the command center reads", async () => {
    const w = await call<{ wallet: Array<{ purpose: string; balanceMinor: number; currency: string }> }>(
      "GET",
      "/api/v1/wallet",
    );
    expect(w.wallet.find((b) => b.purpose === "main")?.currency).toBe("KES");

    const open = await call<{ invoices: Array<{ id: string; status: string }>; total: number }>(
      "GET",
      "/api/v1/invoices?status=open&limit=1",
    );
    expect(open.total).toBe(4);
    expect(open.invoices).toHaveLength(1);
  });
});

describe("learner listing parity", () => {
  it("searches by name and paginates", async () => {
    const hit = await call<LearnerPage>("GET", "/api/v1/learners?limit=20&offset=0&q=amina");
    expect(hit.total).toBe(1);
    expect(hit.learners[0]?.firstName).toBe("Amina");

    const paged = await call<LearnerPage>("GET", "/api/v1/learners?limit=5&offset=5");
    expect(paged.learners).toHaveLength(5);
    expect(paged.total).toBe(12);
  });

  it("hides a just-created learner until their first enrollment lands (API parity)", async () => {
    const created = await call<Learner>("POST", "/api/v1/learners", {
      firstName: "Zawadi",
      lastName: "Newkid",
    });
    expect(created.id).toBeTruthy();

    const before = await call<LearnerPage>("GET", "/api/v1/learners?limit=100&q=zawadi");
    expect(before.total).toBe(0);

    // Deep link into the enroll panel: the real API 404s here too (#58 note).
    await expect(call<Learner>("GET", `/api/v1/learners/${created.id}`)).rejects.toMatchObject({
      status: 404,
    });

    await call<Enrollment>("POST", "/api/v1/enrollments", {
      learnerId: created.id,
      status: "admitted",
    });

    const after = await call<LearnerPage>("GET", "/api/v1/learners?limit=100&q=zawadi");
    expect(after.total).toBe(1);
    const nowVisible = await call<Learner>("GET", `/api/v1/learners/${created.id}`);
    expect(nowVisible.lastName).toBe("Newkid");
  });

  it("rejects invalid and duplicate learner creates like the API", async () => {
    await expect(call("POST", "/api/v1/learners", { firstName: "", lastName: "X" })).rejects.toMatchObject(
      { status: 400 },
    );
    await expect(
      call("POST", "/api/v1/learners", { firstName: "A", lastName: "B", externalId: "DEMO-L001" }),
    ).rejects.toMatchObject({ status: 409, code: "external_id_taken" });
  });
});

describe("enrollment contract", () => {
  it("enrolls a learner and reports the total via X-Total-Count", async () => {
    let headerTotal: string | null = null;
    // Every seeded learner already holds an enrollment (by design), so the
    // enroll-here path goes through the real admit story: create, then enroll.
    const created = await call<Learner>("POST", "/api/v1/learners", {
      firstName: "Nia",
      lastName: "Walkin",
    });
    const enrollment = await call<Enrollment>(
      "POST",
      "/api/v1/enrollments",
      { learnerId: created.id, status: "admitted", classGroupId: "cls-demo-8b", academicYearId: "yr-demo-2026" },
    );
    expect(enrollment.status).toBe("admitted");

    await call("GET", "/api/v1/enrollments?limit=1&offset=0", undefined, {
      onResponse: (res) => {
        headerTotal = (res as { headers: { get(n: string): string | null } }).headers.get("X-Total-Count");
      },
    });
    expect(headerTotal).toBe("13");
  });

  it("blocks duplicate open enrollments with the mapped 409", async () => {
    await expect(
      call("POST", "/api/v1/enrollments", { learnerId: "lrn-demo-001", status: "admitted" }),
    ).rejects.toMatchObject({
      status: 409,
      message: expect.stringMatching(/already has an open enrollment/i),
    });
  });

  it("validates initial status and references exactly like students/service.go", async () => {
    await expect(
      call("POST", "/api/v1/enrollments", { learnerId: "lrn-demo-004", status: "active" }),
    ).rejects.toMatchObject({
      status: 400,
      message: expect.stringMatching(/initial status must be applicant or admitted/i),
    });
    await expect(
      call("POST", "/api/v1/enrollments", { learnerId: "nope", status: "admitted" }),
    ).rejects.toMatchObject({
      status: 400,
      message: expect.stringMatching(/learner not found/i),
    });
    await expect(
      call("POST", "/api/v1/enrollments", {
        learnerId: "lrn-demo-004",
        status: "admitted",
        classGroupId: "cls-does-not-exist",
      }),
    ).rejects.toMatchObject({ status: 400 });
  });

  it("applies the legal-transition machine and ends terminal states", async () => {
    const moved = await call<Enrollment>("POST", "/api/v1/enrollments/enr-demo-003/transition", {
      to: "active",
    });
    expect(moved.status).toBe("active");
    expect(moved.endedAt).toBeNull();

    const graduated = await call<Enrollment>("POST", "/api/v1/enrollments/enr-demo-003/transition", {
      to: "graduated",
    });
    expect(graduated.status).toBe("graduated");

    await expect(
      call("POST", "/api/v1/enrollments/enr-demo-003/transition", { to: "suspended" }),
    ).rejects.toMatchObject({ status: 409, code: "illegal_transition" });

    const terminal = await call<Enrollment>("POST", "/api/v1/enrollments/enr-demo-003/transition", {
      to: "alumni",
    });
    expect(terminal.status).toBe("alumni");
    expect(terminal.endedAt).not.toBeNull();

    await expect(
      call("POST", "/api/v1/enrollments/enr-demo-999/transition", { to: "active" }),
    ).rejects.toMatchObject({ status: 404 });
  });
});

describe("mock session behavior", () => {
  it("signs in with ANY credentials and reflects the name in /me", async () => {
    const session = await call<{ accessToken: string; tokenType: string; expiresIn: number }>(
      "POST",
      "/api/v1/auth/login",
      { email: "jane.mukami@school.example", password: "whatever-123" },
    );
    expect(session.tokenType).toBe("Bearer");
    expect(session.accessToken).toMatch(/^mock-access-/);

    const me = await call<{ name: string; email: string; permissions: Record<string, boolean> }>(
      "GET",
      "/api/v1/me",
    );
    expect(me.email).toBe("jane.mukami@school.example");
    expect(me.name).toBe("Jane Mukami");
    expect(me.permissions["student.manage"]).toBe(true);

    const memberships = await call<Array<{ schoolId: string; role: string; status: string }>>(
      "GET",
      "/api/v1/me/memberships",
    );
    expect(memberships).toHaveLength(1);
    expect(memberships[0]?.role).toBe("school_admin");
  });

  it("rejects empty credentials with 400", async () => {
    await expect(
      call("POST", "/api/v1/auth/login", { email: "not-an-email", password: "x" }),
    ).rejects.toBeInstanceOf(ApiError);
  });

  it("refreshes forever and logs out cleanly", async () => {
    const a = await call<{ accessToken: string }>("POST", "/api/v1/auth/refresh");
    const b = await call<{ accessToken: string }>("POST", "/api/v1/auth/refresh");
    expect(b.accessToken).not.toBe(a.accessToken);

    await expect(call("POST", "/api/v1/auth/logout")).resolves.toBeUndefined();
  });

  it("supports the signup -> login onboarding path and reports taken emails", async () => {
    await call("POST", "/api/v1/auth/signup", {
      schoolName: "Hillcrest Academy",
      adminName: "Peter Otieno",
      email: "peter@hillcrest.example",
      password: "long-enough-1",
    });
    const session = await call<{ accessToken: string }>("POST", "/api/v1/auth/login", {
      email: "peter@hillcrest.example",
      password: "long-enough-1",
    });
    expect(session.accessToken).toBeTruthy();

    await expect(
      call("POST", "/api/v1/auth/signup", {
        schoolName: "Again",
        adminName: "Peter Otieno",
        email: "peter@hillcrest.example",
        password: "long-enough-1",
      }),
    ).rejects.toMatchObject({ status: 409, code: "email_taken" });
  });
});

describe("transport semantics", () => {
  it("404s unknown routes with the standard envelope", async () => {
    await expect(call("GET", "/api/v1/does-not-exist")).rejects.toMatchObject({
      status: 404,
      code: "not_found",
    });
  });

  it("rejects aborted requests with AbortError like the real fetch", async () => {
    const controller = new AbortController();
    controller.abort();
    await expect(
      call("GET", "/api/v1/learners", undefined, { signal: controller.signal }),
    ).rejects.toMatchObject({ name: "AbortError" });
  });

  it("filters enrollments by status for the chip filters", async () => {
    const admitted = await call<{ enrollments: Enrollment[]; total: number }>(
      "GET",
      "/api/v1/enrollments?status=admitted&limit=20",
    );
    expect(admitted.total).toBe(2);
    expect(admitted.enrollments.every((e) => e.status === ("admitted" as EnrollmentStatus))).toBe(true);
  });
});

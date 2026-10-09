import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, cleanup, fireEvent } from "@testing-library/react";
import FinancePage from "./page";

const { sessionValue } = vi.hoisted(() => ({
  sessionValue: {
    me: {
      id: "u1",
      email: "u@school.example",
      name: "Bursar",
      status: "active",
      roles: ["admin"],
      permissions: { "finance.read": true, "finance.manage": true } as Record<string, boolean>,
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

const SEED_WALLET = {
  wallet: [
    { purpose: "main", balanceMinor: 128450000, currency: "KES" },
    { purpose: "operations", balanceMinor: 41200000, currency: "KES" },
    { purpose: "tuition", balanceMinor: 96750000, currency: "KES" },
  ],
};

const SEED_INVOICES = {
  invoices: [
    { id: "inv-001", learnerId: "lrn-1", status: "open", amountMinor: 1850000, currency: "KES", dueDate: "2026-03-01" },
    { id: "inv-005", learnerId: "lrn-2", status: "paid", amountMinor: 925000, currency: "KES", dueDate: "2026-02-01" },
  ],
  total: 2,
  limit: 100,
  offset: 0,
};

const SEED_ROSTER = {
  learners: [
    { id: "lrn-1", firstName: "Amina", lastName: "Otieno", createdAt: "2026-01-01T00:00:00Z" },
    { id: "lrn-2", firstName: "Brian", lastName: "Mutai", createdAt: "2026-01-02T00:00:00Z" },
  ],
  total: 2,
  limit: 100,
  offset: 0,
};

function seedHappyFetch() {
  fetchMock.mockImplementation((url: string) => {
    if (String(url).includes("/api/v1/wallet")) return jsonResponse(200, SEED_WALLET);
    if (String(url).includes("/api/v1/learners")) return jsonResponse(200, SEED_ROSTER);
    return jsonResponse(200, SEED_INVOICES);
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

describe("FinancePage (#187 wallet + invoice book)", () => {
  it("renders wallet balances, learner names, and the outstanding/collected summary", async () => {
    seedHappyFetch();
    render(<FinancePage />);

    await waitFor(() => expect(screen.getByText("1,284,500.00 KES")).toBeTruthy());
    expect(screen.getByText("412,000.00 KES")).toBeTruthy();
    expect(screen.getByText("967,500.00 KES")).toBeTruthy();

    // Invoice rows resolve learner names through the roster, not raw ids.
    expect(screen.getByText("Amina Otieno")).toBeTruthy();
    expect(screen.getByText("Brian Mutai")).toBeTruthy();

    // Summary strip: outstanding = open sum (inv-001), collected = paid sum (inv-005).
    expect(screen.getByText("18,500.00 KES", { selector: "strong" })).toBeTruthy();
    expect(screen.getByText("9,250.00 KES", { selector: "strong" })).toBeTruthy();
    expect(screen.getByText(/Open invoices:/)).toBeTruthy();
  });

  it("applies the status filter as a query param and re-fetches", async () => {
    seedHappyFetch();
    render(<FinancePage />);
    await waitFor(() => expect(screen.getByText("Amina Otieno")).toBeTruthy());

    fireEvent.change(screen.getByLabelText("Status"), { target: { value: "paid" } });

    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some((c) => String(c[0]).includes("status=paid")),
      ).toBeTruthy(),
    );
  });

  it("shows an empty-table message when the filter matches nothing", async () => {
    seedHappyFetch();
    fetchMock.mockImplementation((url: string) => {
      if (String(url).includes("/api/v1/wallet")) return jsonResponse(200, SEED_WALLET);
      if (String(url).includes("/api/v1/learners")) return jsonResponse(200, SEED_ROSTER);
      return jsonResponse(200, { invoices: [], total: 0, limit: 100, offset: 0 });
    });
    render(<FinancePage />);
    await waitFor(() => expect(screen.getByText(/No invoices match this filter/)).toBeTruthy());
  });

  it("surfaces API failures as an error note instead of a blank page", async () => {
    fetchMock.mockImplementation(() =>
      jsonResponse(500, { error: { code: "internal", message: "ledger unavailable" } }),
    );
    render(<FinancePage />);
    await waitFor(() => expect(screen.getByText(/ledger unavailable/)).toBeTruthy());
  });

  it("denies users without finance.read", () => {
    const original = sessionValue.me.permissions;
    sessionValue.me = { ...sessionValue.me, permissions: {} };
    try {
      render(<FinancePage />);
      expect(screen.getByText(/does not include finance access/)).toBeTruthy();
      expect(screen.queryByText("Invoice book")).toBeNull();
    } finally {
      sessionValue.me = { ...sessionValue.me, permissions: original };
    }
  });
});

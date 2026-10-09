import { describe, it, expect } from "vitest";
import { formatMoneyMinor } from "./money";

describe("formatMoneyMinor (#187)", () => {
  it("renders minor units as a grouped major-unit amount with currency", () => {
    expect(formatMoneyMinor(1850000, "KES")).toBe("18,500.00 KES");
  });

  it("keeps cents honest — not everything divides into whole units", () => {
    expect(formatMoneyMinor(1850050, "KES")).toBe("18,500.50 KES");
    expect(formatMoneyMinor(5, "KES")).toBe("0.05 KES");
  });

  it("handles negative balances (overdrafts) with a leading sign", () => {
    expect(formatMoneyMinor(-41200000, "KES")).toBe("-412,000.00 KES");
  });

  it("renders zero without a sign", () => {
    expect(formatMoneyMinor(0, "KES")).toBe("0.00 KES");
  });

  it("passes the currency through verbatim (multi-currency forward-compat, #177)", () => {
    expect(formatMoneyMinor(100000, "USD")).toBe("1,000.00 USD");
  });
});

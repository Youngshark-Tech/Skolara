/**
 * Money formatting for amounts carried in minor units (the API contract:
 * `amountMinor` / `balanceMinor` fields across invoices and wallet balances).
 *
 * One shared helper so the conversion lives in exactly one auditable place —
 * every surface (command center, finance) shows the same currency rendering.
 * The command center's inline `(abs / 100).toLocaleString()` predates this
 * helper and stays as-is until its next touch; new surfaces use this.
 */
export function formatMoneyMinor(amountMinor: number, currency: string): string {
  const sign = amountMinor < 0 ? "-" : "";
  const major = Math.abs(amountMinor) / 100;
  const rendered = major.toLocaleString("en-KE", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });
  return `${sign}${rendered} ${currency}`;
}

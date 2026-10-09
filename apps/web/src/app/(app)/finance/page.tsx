"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { apiFetch } from "@/lib/api";
import { useSession } from "@/lib/session";
import { can } from "@/lib/permissions";
import { formatMoneyMinor } from "@/lib/money";
import { Badge, Card, ErrorNote } from "@/components/ui";
import type { Invoice, InvoicePage, Learner, LearnerPage, WalletResponse } from "@/types/api";

type StatusFilter = "all" | "open" | "paid" | "void";
const STATUS_FILTERS: StatusFilter[] = ["all", "open", "paid", "void"];
const INVOICE_PAGE_SIZE = 100; // demo book is small; one page, honest pager later

const BADGE_TONE: Record<Invoice["status"], "blue" | "green" | "slate"> = {
  open: "blue",
  paid: "green",
  void: "slate",
};

function isAbortError(err: unknown): boolean {
  return err instanceof Error && err.name === "AbortError";
}

/**
 * Finance surface (#187): wallet balances by ledger purpose plus the invoice
 * book with a status filter. Reads the same endpoints the command center uses
 * (`/api/v1/wallet`, `/api/v1/invoices`) so demo mode and the connected
 * database show identical behavior — contract parity per #142.
 *
 * Learner names resolve through the roster endpoint (one fetch); any learner
 * missing from the roster falls back to its id — never a blank cell.
 */
export default function FinancePage() {
  const { me, activeSchoolId } = useSession();
  const allowed = can(me, "finance.read") || can(me, "finance.manage");

  const [wallet, setWallet] = useState<WalletResponse["wallet"] | null>(null);
  const [invoices, setInvoices] = useState<Invoice[]>([]);
  const [invoiceTotal, setInvoiceTotal] = useState(0);
  const [learners, setLearners] = useState<Map<string, string>>(new Map());
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(
    async (signal?: AbortSignal) => {
      if (!activeSchoolId || !can(me, "finance.read")) return;
      try {
        const params = new URLSearchParams({ limit: String(INVOICE_PAGE_SIZE), offset: "0" });
        if (statusFilter !== "all") params.set("status", statusFilter);
        const [walletRes, invoiceRes, rosterRes] = await Promise.all([
          apiFetch<WalletResponse>("/api/v1/wallet", { signal }),
          apiFetch<InvoicePage>(`/api/v1/invoices?${params.toString()}`, { signal }),
          apiFetch<LearnerPage>("/api/v1/learners?limit=100", { signal }),
        ]);
        setWallet(walletRes.wallet);
        setInvoices(invoiceRes.invoices);
        setInvoiceTotal(invoiceRes.total);
        setLearners(
          new Map(rosterRes.learners.map((l: Learner) => [l.id, `${l.firstName} ${l.lastName}`])),
        );
        setError(null);
      } catch (err) {
        if (isAbortError(err)) return; // a newer request superseded this one
        setError(err instanceof Error ? err.message : "failed to load finance data");
      } finally {
        setLoading(false);
      }
    },
    [activeSchoolId, me, statusFilter],
  );

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  const summary = useMemo(() => {
    const outstanding = invoices
      .filter((i) => i.status === "open")
      .reduce((acc, i) => acc + i.amountMinor, 0);
    const collected = invoices
      .filter((i) => i.status === "paid")
      .reduce((acc, i) => acc + i.amountMinor, 0);
    const openCount = invoices.filter((i) => i.status === "open").length;
    const currency = invoices[0]?.currency ?? "KES";
    return { outstanding, collected, openCount, currency };
  }, [invoices]);

  if (!allowed) {
    return (
      <div className="space-y-4">
        <h1 className="text-xl font-semibold">Finance</h1>
        <ErrorNote message="Your role does not include finance access." />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-xl font-semibold">Finance</h1>
        <p className="text-sm text-slate-500">
          Wallet balances and the invoice book at the active school
        </p>
      </header>

      <ErrorNote message={error} />

      {loading && (
        <p className="text-sm text-slate-500" aria-live="polite">
          Loading finance data…
        </p>
      )}

      {!loading && wallet && (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          {wallet.map((b) => (
            <Card key={b.purpose} title={`Wallet · ${b.purpose}`}>
              <p className="text-2xl font-semibold tracking-tight">
                {formatMoneyMinor(b.balanceMinor, b.currency)}
              </p>
              <p className="mt-1 text-xs text-slate-500">Ledger purpose balance</p>
            </Card>
          ))}
        </div>
      )}

      {!loading && (
        <Card
          title="Invoice book"
          action={
            <div className="flex items-center gap-2">
              <label htmlFor="invoice-status-filter" className="text-xs text-slate-500">
                Status
              </label>
              <select
                id="invoice-status-filter"
                value={statusFilter}
                onChange={(e) => setStatusFilter(e.target.value as StatusFilter)}
                className="rounded-md border border-slate-300 bg-white px-2 py-1 text-sm"
              >
                {STATUS_FILTERS.map((s) => (
                  <option key={s} value={s}>
                    {s}
                  </option>
                ))}
              </select>
            </div>
          }
        >
          <div className="mb-4 flex flex-wrap gap-x-6 gap-y-1 text-xs text-slate-600">
            <span aria-live="polite">
              Outstanding:{" "}
              <strong>{formatMoneyMinor(summary.outstanding, summary.currency)}</strong>
            </span>
            <span>
              Collected: <strong>{formatMoneyMinor(summary.collected, summary.currency)}</strong>
            </span>
            <span>
              Open invoices: <strong>{summary.openCount}</strong>
            </span>
            <span>
              Showing {invoices.length} of {invoiceTotal}
            </span>
          </div>

          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-slate-200 text-left text-xs uppercase tracking-wide text-slate-500">
                  <th scope="col" className="pb-2">Invoice</th>
                  <th scope="col" className="pb-2">Learner</th>
                  <th scope="col" className="pb-2">Status</th>
                  <th scope="col" className="pb-2">Amount</th>
                  <th scope="col" className="pb-2">Due date</th>
                </tr>
              </thead>
              <tbody>
                {invoices.map((i) => (
                  <tr key={i.id} className="border-b border-slate-100 last:border-0">
                    <td className="py-2 font-mono text-xs">{i.id}</td>
                    <td className="py-2 font-medium">{learners.get(i.learnerId) ?? i.learnerId}</td>
                    <td className="py-2">
                      <Badge tone={BADGE_TONE[i.status]}>{i.status}</Badge>
                    </td>
                    <td className="py-2">{formatMoneyMinor(i.amountMinor, i.currency)}</td>
                    <td className="py-2 text-slate-500">{i.dueDate}</td>
                  </tr>
                ))}
                {invoices.length === 0 && (
                  <tr>
                    <td colSpan={5} className="py-6 text-center text-slate-500">
                      No invoices match this filter
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </Card>
      )}
    </div>
  );
}

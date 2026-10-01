"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { apiFetch, getSchoolId } from "@/lib/api";
import { useSession } from "@/lib/session";
import { can } from "@/lib/permissions";
import { Button, Card, ErrorNote, Input, Label, Notice, Stat } from "@/components/ui";
import type { Learner, LearnerPage } from "@/types/api";

interface WalletBalance {
  purpose: string;
  balanceMinor: number;
  currency: string;
}

interface InvoicePage {
  invoices: { id: string; status: string }[];
  total: number;
}

/**
 * Command center: the school-wide operating dashboard.
 *
 * #57: stats load through an explicit loadStats callback (also re-run after a
 * quick-admit so the Learners stat can never go stale), and a failed load is
 * shown as an error with a retry — never as a misleading empty state.
 */
export function CommandCenter() {
  const { me, activeSchoolId } = useSession();
  const [learnerTotal, setLearnerTotal] = useState<number | null>(null);
  const [walletMain, setWalletMain] = useState<string | null>(null);
  const [openInvoices, setOpenInvoices] = useState<number | null>(null);
  const [statsError, setStatsError] = useState<string | null>(null);
  const [statsLoading, setStatsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [firstName, setFirstName] = useState("");
  const [lastName, setLastName] = useState("");
  const [notice, setNotice] = useState<string | null>(null);
  /** The learner just created — the deep link into the enroll flow (#58). */
  const [createdLearner, setCreatedLearner] = useState<Learner | null>(null);

  const loadStats = useCallback(async () => {
    if (!activeSchoolId || !can(me, "student.read")) return;
    setStatsLoading(true);
    setStatsError(null);
    try {
      const page = await apiFetch<LearnerPage>("/api/v1/learners?limit=1");
      setLearnerTotal(page.total);
    } catch (err) {
      setStatsError(err instanceof Error ? err.message : "failed to load learner count");
    } finally {
      setStatsLoading(false);
    }
  }, [activeSchoolId, me]);

  const loadFinance = useCallback(async () => {
    if (!activeSchoolId || !can(me, "finance.read")) return;
    try {
      const w = await apiFetch<{ wallet: WalletBalance[] }>("/api/v1/wallet");
      const main = w.wallet.find((b) => b.purpose === "main");
      if (main) {
        const sign = main.balanceMinor < 0 ? "-" : "";
        const abs = Math.abs(main.balanceMinor);
        setWalletMain(`${sign}${(abs / 100).toLocaleString()} ${main.currency}`);
      }
    } catch {
      setWalletMain(null);
    }
    try {
      const page = await apiFetch<InvoicePage>("/api/v1/invoices?status=open&limit=1");
      setOpenInvoices(page.total);
    } catch {
      setOpenInvoices(null);
    }
  }, [activeSchoolId, me]);

  useEffect(() => {
    void loadStats();
  }, [loadStats]);

  useEffect(() => {
    void loadFinance();
  }, [loadFinance]);

  const createLearner = async (e: React.FormEvent) => {
    e.preventDefault();
    setCreating(true);
    setNotice(null);
    setError(null);
    try {
      const created = await apiFetch<Learner>("/api/v1/learners", {
        method: "POST",
        body: { firstName, lastName },
      });
      setCreatedLearner(created);
      setNotice(
        `Learner record created for ${firstName} ${lastName}. Enroll them at this school to finish admission.`,
      );
      setFirstName("");
      setLastName("");
      // Refetch immediately — the Learners stat must reflect the new record.
      void loadStats();
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to create learner");
    } finally {
      setCreating(false);
    }
  };

  const noSchool = !activeSchoolId;

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-xl font-semibold">Command Center</h1>
        <p className="text-sm text-slate-500">
          Welcome back{me ? `, ${me.name}` : ""} — here is your school at a glance.
        </p>
      </header>

      <ErrorNote message={noSchool ? "No active school membership — ask an admin to add you to a school." : error} />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <div>
          <Stat
            label="Learners"
            value={learnerTotal === null ? "—" : String(learnerTotal)}
            hint="Enrolled at the active school"
          />
          {/* A failed stat load is an explicit error with a retry, never a
              quiet "—" that looks like an empty school. */}
          {statsError && (
            <div className="mt-2 flex items-center gap-2">
              <ErrorNote message={`Learner count failed to load: ${statsError}`} />
              <Button variant="secondary" onClick={() => void loadStats()} disabled={statsLoading}>
                Retry
              </Button>
            </div>
          )}
        </div>
        <Stat label="Wallet · main" value={walletMain ?? "—"} hint="Derived from the ledger" />
        <Stat label="Open invoices" value={openInvoices === null ? "—" : String(openInvoices)} />
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        {can(me, "student.manage") && (
          <Card title="Quick action — create a learner record">
            <form className="space-y-3" onSubmit={createLearner}>
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <Label htmlFor="dashboard-first-name">First name</Label>
                  <Input
                    id="dashboard-first-name"
                    required
                    value={firstName}
                    onChange={(e) => setFirstName(e.target.value)}
                  />
                </div>
                <div>
                  <Label htmlFor="dashboard-last-name">Last name</Label>
                  <Input
                    id="dashboard-last-name"
                    required
                    value={lastName}
                    onChange={(e) => setLastName(e.target.value)}
                  />
                </div>
              </div>
              {notice && (
                <Notice>
                  {notice}{" "}
                  {createdLearner && (
                    <Link
                      className="font-semibold underline hover:no-underline"
                      href={`/enrollments?enroll=${createdLearner.id}`}
                    >
                      Enroll {createdLearner.firstName} →
                    </Link>
                  )}
                </Notice>
              )}
              <Button type="submit" disabled={creating || !getSchoolId()}>
                {creating ? "Creating…" : "Create learner"}
              </Button>
            </form>
          </Card>
        )}

        <Card title="Where to next">
          <ul className="space-y-2 text-sm">
            <li>
              <Link className="text-primary hover:underline" href="/students">
                Students →
              </Link>{" "}
              <span className="text-slate-500">roster, guardians, learner records</span>
            </li>
            <li>
              <Link className="text-primary hover:underline" href="/enrollments">
                Enrollments →
              </Link>{" "}
              <span className="text-slate-500">admission lifecycle — applicant to alumni</span>
            </li>
            <li>
              <Link className="text-primary hover:underline" href="/academics">
                Academics →
              </Link>{" "}
              <span className="text-slate-500">years, terms, subjects, classes</span>
            </li>
            <li>
              <Link className="text-primary hover:underline" href="/attendance">
                Attendance →
              </Link>{" "}
              <span className="text-slate-500">offline-tolerant roll calls</span>
            </li>
            <li>
              <Link className="text-primary hover:underline" href="/finance">
                Finance →
              </Link>{" "}
              <span className="text-slate-500">ledger, invoices, wallet</span>
            </li>
          </ul>
        </Card>
      </div>
    </div>
  );
}

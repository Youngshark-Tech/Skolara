"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { apiFetch, getSchoolId } from "@/lib/api";
import { useSession } from "@/lib/session";
import { can } from "@/lib/permissions";
import { Button, Card, ErrorNote, Input, Label, Stat } from "@/components/ui";

interface LearnerPage {
  learners: { id: string; firstName: string; lastName: string; externalId?: string }[];
  total: number;
}

interface WalletBalance {
  purpose: string;
  balanceMinor: number;
  currency: string;
}

interface InvoicePage {
  invoices: { id: string; status: string }[];
  total: number;
}

/** Command center: the school-wide operating dashboard. */
export default function CommandCenter() {
  const { me, activeSchoolId } = useSession();
  const [learnerTotal, setLearnerTotal] = useState<number | null>(null);
  const [walletMain, setWalletMain] = useState<string | null>(null);
  const [openInvoices, setOpenInvoices] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [firstName, setFirstName] = useState("");
  const [lastName, setLastName] = useState("");
  const [notice, setNotice] = useState<string | null>(null);

  useEffect(() => {
    if (!activeSchoolId || !can(me, "student.read")) return;
    apiFetch<LearnerPage>("/api/v1/learners?limit=1")
      .then((page) => setLearnerTotal(page.total))
      .catch(() => setLearnerTotal(null));
  }, [activeSchoolId, me]);

  useEffect(() => {
    if (!activeSchoolId || !can(me, "finance.read")) return;
    apiFetch<{ wallet: WalletBalance[] }>("/api/v1/wallet")
      .then((w) => {
        const main = w.wallet.find((b) => b.purpose === "main");
        if (main) {
          const sign = main.balanceMinor < 0 ? "-" : "";
          const abs = Math.abs(main.balanceMinor);
          setWalletMain(`${sign}${(abs / 100).toLocaleString()} ${main.currency}`);
        }
      })
      .catch(() => setWalletMain(null));
    apiFetch<InvoicePage>("/api/v1/invoices?status=open&limit=1")
      .then((page) => setOpenInvoices(page.total))
      .catch(() => setOpenInvoices(null));
  }, [activeSchoolId, me]);

  const createLearner = async (e: React.FormEvent) => {
    e.preventDefault();
    setCreating(true);
    setNotice(null);
    setError(null);
    try {
      await apiFetch("/api/v1/learners", {
        method: "POST",
        body: { firstName, lastName },
      });
      setNotice(`Learner ${firstName} ${lastName} created`);
      setFirstName("");
      setLastName("");
      setLearnerTotal(null); // refetch
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
        <Stat
          label="Learners"
          value={learnerTotal === null ? "—" : String(learnerTotal)}
          hint="Enrolled at the active school"
        />
        <Stat label="Wallet · main" value={walletMain ?? "—"} hint="Derived from the ledger" />
        <Stat label="Open invoices" value={openInvoices === null ? "—" : String(openInvoices)} />
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        {can(me, "student.manage") && (
          <Card title="Quick action — admit a learner">
            <form className="space-y-3" onSubmit={createLearner}>
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <Label>First name</Label>
                  <Input required value={firstName} onChange={(e) => setFirstName(e.target.value)} />
                </div>
                <div>
                  <Label>Last name</Label>
                  <Input required value={lastName} onChange={(e) => setLastName(e.target.value)} />
                </div>
              </div>
              {notice && <p className="text-xs text-emerald-600">{notice}</p>}
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
              <span className="text-slate-500">roster, guardians, enrollment lifecycle</span>
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

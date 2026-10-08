import type { Metadata } from "next";
import { CommandCenter } from "../command-center";

export const metadata: Metadata = { title: "Command Center" };

/**
 * Server wrapper so the dashboard route carries a real document title (#57).
 * Moved from / to /dashboard by #129 so the public landing page owns "/",
 * while logged-in users still land on their workspace after sign-in.
 */
export default function DashboardPage() {
  return <CommandCenter />;
}

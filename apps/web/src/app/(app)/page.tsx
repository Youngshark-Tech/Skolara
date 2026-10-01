import type { Metadata } from "next";
import { CommandCenter } from "./command-center";

export const metadata: Metadata = { title: "Command Center" };

/** Server wrapper so the dashboard route carries a real document title (#57). */
export default function DashboardPage() {
  return <CommandCenter />;
}

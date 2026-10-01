import type { Metadata } from "next";

export const metadata: Metadata = { title: "Attendance" };

export default function AttendanceLayout({ children }: { children: React.ReactNode }) {
  return children;
}

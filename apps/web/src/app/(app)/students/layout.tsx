import type { Metadata } from "next";

export const metadata: Metadata = { title: "Students" };

export default function StudentsLayout({ children }: { children: React.ReactNode }) {
  return children;
}

import type { Metadata } from "next";
import { SessionProvider } from "../components/session";
import "./globals.css";
export const metadata: Metadata = { title: { default: "Ballast — your development workspace", template: "%s · Ballast" }, description: "A local workspace for isolated tasks, clear reviews, and deliberate Git integration.", robots: { index: false, follow: false } };
export default function RootLayout({ children }: { children: React.ReactNode }) { return <html lang="en"><body><SessionProvider>{children}</SessionProvider></body></html>; }

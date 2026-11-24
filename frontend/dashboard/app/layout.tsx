import type { Metadata, Viewport } from "next";
import "./globals.css";
import AuthProvider from "@/components/AuthProvider";
import DashboardShell from "@/components/DashboardShell";
import { LocaleProvider } from "@/lib/i18n";

export const metadata: Metadata = {
  title: "CryptoEngine",
  description: "Event-driven crypto trading and execution dashboard",
};

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  viewportFit: "cover",
};

const demo = process.env.NEXT_PUBLIC_DEMO_MODE === "1";

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang={demo ? "en" : "ja"}>
      <body className="min-h-screen text-slate-200 antialiased">
        <LocaleProvider>
          <AuthProvider>
            <DashboardShell>{children}</DashboardShell>
          </AuthProvider>
        </LocaleProvider>
      </body>
    </html>
  );
}

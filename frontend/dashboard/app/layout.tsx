import type { Metadata, Viewport } from "next";
import "./globals.css";
import AuthProvider from "@/components/AuthProvider";
import DashboardShell from "@/components/DashboardShell";
import { LocaleProvider } from "@/lib/i18n";

const SITE_URL = "https://hisashi9899.github.io/cryptoEngine-portfolio/";
const AUTHOR_URL = "https://contra.com/neutron_d0wbklm2/work";
const GITHUB_URL = "https://github.com/Hisashi9899";

const DESCRIPTION =
  "Event-driven crypto trading and execution platform: deterministic single-writer state, cost-aware execution gates, layered risk limits, replay research, and a real-time dashboard. The public demo runs on synthetic data only.";

const STRUCTURED_DATA = {
  "@context": "https://schema.org",
  "@graph": [
    {
      "@type": "Person",
      "@id": `${AUTHOR_URL}#person`,
      name: "Hisashi Aoki",
      jobTitle: "Full-Spectrum Product Engineer",
      url: AUTHOR_URL,
      sameAs: [GITHUB_URL, AUTHOR_URL],
      knowsAbout: [
        "Go",
        "Python",
        "FastAPI",
        "Next.js",
        "distributed systems",
        "trading infrastructure",
        "CI/CD",
      ],
    },
    {
      "@type": "SoftwareApplication",
      name: "CryptoEngine",
      applicationCategory: "FinanceApplication",
      operatingSystem: "Web",
      url: SITE_URL,
      description: DESCRIPTION,
      author: { "@id": `${AUTHOR_URL}#person` },
    },
  ],
};

export const metadata: Metadata = {
  metadataBase: new URL("https://hisashi9899.github.io"),
  title: "CryptoEngine — Event-driven trading and execution platform",
  description: DESCRIPTION,
  keywords: [
    "event-driven trading system",
    "crypto trading engine",
    "Go backend",
    "FastAPI",
    "risk controls",
    "backtest replay",
    "portfolio case study",
  ],
  authors: [{ name: "Hisashi Aoki", url: AUTHOR_URL }],
  creator: "Hisashi Aoki",
  alternates: { canonical: SITE_URL },
  robots: { index: true, follow: true },
  openGraph: {
    title: "CryptoEngine — Event-driven trading and execution platform",
    description: DESCRIPTION,
    url: SITE_URL,
    siteName: "CryptoEngine",
    type: "website",
    locale: "en_US",
  },
  twitter: {
    card: "summary",
    title: "CryptoEngine — Event-driven trading and execution platform",
    description: DESCRIPTION,
  },
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
        <script
          type="application/ld+json"
          dangerouslySetInnerHTML={{ __html: JSON.stringify(STRUCTURED_DATA) }}
        />
        <LocaleProvider>
          <AuthProvider>
            <DashboardShell>{children}</DashboardShell>
          </AuthProvider>
        </LocaleProvider>
      </body>
    </html>
  );
}

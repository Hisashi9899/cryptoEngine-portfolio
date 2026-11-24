"use client";

import { usePathname } from "next/navigation";
import Sidebar from "./Sidebar";
import MobileNav from "./MobileNav";
import LanguageToggle from "./LanguageToggle";
import { normalizePath } from "@/lib/path";

/**
 * ダッシュボードのシェル。ログイン画面ではサイドバーを表示せず、
 * 通常ページでは固定サイドバー分の余白を確保する。
 */
export default function DashboardShell({
  children,
}: {
  children: React.ReactNode;
}) {
  const pathname = usePathname();
  const isLogin = normalizePath(pathname) === "/login";

  if (isLogin) {
    return <>{children}</>;
  }

  return (
    <>
      <Sidebar />
      <div className="fixed right-3 top-3 z-30 lg:hidden">
        <LanguageToggle />
      </div>
      <div className="lg:pl-64">
        <main className="mx-auto max-w-7xl px-4 pb-24 pt-12 sm:px-6 lg:px-10 lg:py-8 lg:pb-8">
          {children}
        </main>
      </div>
      <MobileNav />
    </>
  );
}

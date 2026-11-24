"use client";

import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { isLoggedIn, refreshAccessToken } from "@/lib/http";

/**
 * 認証ガード。
 *
 * - /login 以外の全ページで未認証なら /login へリダイレクト
 * - access token が期限切れの場合は refresh token での再取得を試みる
 * - チェック中は loading プレースホルダを表示
 */
export default function AuthProvider({
  children,
}: {
  children: React.ReactNode;
}) {
  const pathname = usePathname();
  const router = useRouter();
  const [checking, setChecking] = useState(true);

  useEffect(() => {
    let active = true;
    if (pathname === "/login") {
      setChecking(false);
      return;
    }
    setChecking(true);
    (async () => {
      if (isLoggedIn()) {
        if (active) setChecking(false);
        return;
      }
      const ok = await refreshAccessToken();
      if (!active) return;
      if (ok && isLoggedIn()) {
        setChecking(false);
      } else {
        router.replace("/login");
      }
    })();
    return () => {
      active = false;
    };
  }, [pathname, router]);

  if (pathname === "/login") return <>{children}</>;

  if (checking) {
    return (
      <div className="grid min-h-screen place-items-center bg-navy-950">
        <div className="flex flex-col items-center gap-3">
          <div className="h-8 w-8 animate-spin rounded-full border-2 border-emerald-400 border-t-transparent" />
          <p className="text-xs text-slate-500">認証を確認中...</p>
        </div>
      </div>
    );
  }

  return <>{children}</>;
}

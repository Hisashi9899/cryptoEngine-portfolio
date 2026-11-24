"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  Activity,
  ArrowLeftRight,
  BookOpenText,
  Grid3x3,
  Layers,
  LayoutDashboard,
  Percent,
  Radar,
  Server,
  Wallet,
} from "lucide-react";
import { cn } from "@/lib/cn";
import { useI18n } from "@/lib/i18n";
import { normalizePath } from "@/lib/path";

const NAV = [
  { href: "/", ja: "概要", en: "Home", icon: LayoutDashboard },
  { href: "/balance", ja: "残高", en: "Balance", icon: Wallet },
  { href: "/positions", ja: "建玉", en: "Positions", icon: Layers },
  { href: "/trades", ja: "取引", en: "Trades", icon: ArrowLeftRight },
  { href: "/signals", ja: "信号", en: "Signals", icon: Activity },
  { href: "/vt", ja: "V.T", en: "V.T", icon: Radar },
  { href: "/vt-signals", ja: "V.T信号", en: "V.T Sig", icon: Activity },
  { href: "/funding", ja: "FR", en: "FR", icon: Percent },
  { href: "/grid", ja: "格子", en: "Grid", icon: Grid3x3 },
  { href: "/orderbook", ja: "板", en: "Book", icon: BookOpenText },
  { href: "/system", ja: "系", en: "System", icon: Server },
];

/**
 * スマホ用ボトムタブナビ（lg未満のみ表示）。
 * サイドバーが hidden のため、モバイルの唯一の導線になる。
 */
export default function MobileNav() {
  const pathname = usePathname();
  const { tr } = useI18n();
  const current = normalizePath(pathname);

  return (
    <nav
      aria-label={tr("モバイルナビ", "Mobile navigation")}
      className="fixed inset-x-0 bottom-0 z-20 border-t border-white/10 bg-navy-900/90 pb-[env(safe-area-inset-bottom)] backdrop-blur-xl lg:hidden"
    >
      <ul className="grid grid-cols-11">
        {NAV.map((item) => {
          const active = current === item.href;
          const Icon = item.icon;
          return (
            <li key={item.href}>
              <Link
                href={item.href}
                aria-current={active ? "page" : undefined}
                className={cn(
                  "flex flex-col items-center gap-1 px-1 py-2.5 text-[10px] transition-colors",
                  active ? "text-emerald-300" : "text-slate-500 hover:text-slate-200"
                )}
              >
                <Icon className="h-5 w-5" />
                {tr(item.ja, item.en)}
                {active && <span className="h-1 w-1 rounded-full bg-emerald-400" />}
              </Link>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}

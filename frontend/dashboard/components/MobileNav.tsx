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

const NAV = [
  { href: "/", label: "概要", icon: LayoutDashboard },
  { href: "/balance", label: "残高", icon: Wallet },
  { href: "/positions", label: "建玉", icon: Layers },
  { href: "/trades", label: "取引", icon: ArrowLeftRight },
  { href: "/signals", label: "信号", icon: Activity },
  { href: "/vt", label: "V.T", icon: Radar },
  { href: "/vt-signals", label: "V.T信号", icon: Activity },
  { href: "/funding", label: "FR", icon: Percent },
  { href: "/grid", label: "格子", icon: Grid3x3 },
  { href: "/orderbook", label: "板", icon: BookOpenText },
  { href: "/system", label: "系", icon: Server },
];

/**
 * スマホ用ボトムタブナビ（lg未満のみ表示）。
 * サイドバーが hidden のため、モバイルの唯一の導線になる。
 */
export default function MobileNav() {
  const pathname = usePathname();

  return (
    <nav
      aria-label="モバイルナビ"
      className="fixed inset-x-0 bottom-0 z-20 border-t border-white/10 bg-navy-900/90 pb-[env(safe-area-inset-bottom)] backdrop-blur-xl lg:hidden"
    >
      <ul className="grid grid-cols-11">
        {NAV.map((item) => {
          const active = pathname === item.href;
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
                {item.label}
                {active && <span className="h-1 w-1 rounded-full bg-emerald-400" />}
              </Link>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}

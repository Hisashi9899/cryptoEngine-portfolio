"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import {
  Activity,
  ArrowLeftRight,
  BookOpenText,
  Gauge,
  Grid3x3,
  Layers,
  LayoutDashboard,
  LogOut,
  Percent,
  Radar,
  Server,
  Wallet,
} from "lucide-react";
import { cn } from "@/lib/cn";
import { logout } from "@/lib/http";
import { useI18n } from "@/lib/i18n";
import LanguageToggle from "./LanguageToggle";
import { DEMO_MODE } from "@/lib/demo";
import { normalizePath } from "@/lib/path";

const NAV = [
  { href: "/", ja: "概要", en: "Overview", icon: LayoutDashboard },
  { href: "/balance", ja: "残高", en: "Balance", icon: Wallet },
  { href: "/positions", ja: "建玉", en: "Positions", icon: Layers },
  { href: "/trades", ja: "取引", en: "Trades", icon: ArrowLeftRight },
  { href: "/signals", ja: "信号", en: "Signals", icon: Activity },
  { href: "/vt", ja: "V.T Monitor", en: "V.T Monitor", icon: Radar },
  { href: "/vt-signals", ja: "V.T信号", en: "V.T Signals", icon: Activity },
  { href: "/funding", ja: "FR監視", en: "Funding", icon: Percent },
  { href: "/grid", ja: "格子評価", en: "Grid", icon: Grid3x3 },
  { href: "/orderbook", ja: "板情報", en: "Orderbook", icon: BookOpenText },
  { href: "/system", ja: "システム", en: "System", icon: Server },
];

export default function Sidebar() {
  const pathname = usePathname();
  const router = useRouter();
  const { tr } = useI18n();
  const current = normalizePath(pathname);

  const onLogout = async () => {
    await logout();
    router.replace("/login");
  };

  return (
    <aside className="fixed inset-y-0 left-0 z-20 hidden w-64 border-r border-white/5 bg-navy-900/70 backdrop-blur-xl lg:block">
      <div className="flex h-full flex-col px-4 py-6">
        <div className="mb-8 flex items-center gap-3 px-2">
          <div className="grid h-10 w-10 place-items-center rounded-card bg-gradient-to-br from-emerald-400 to-emerald-600 shadow-glow">
            <Gauge className="h-5 w-5 text-navy-950" />
          </div>
          <div className="min-w-0">
            <p className="text-sm font-bold tracking-tight text-white">
              CryptoEngine
            </p>
            <p className="text-xs text-slate-400">Auto Trading Monitor</p>
            {DEMO_MODE && (
              <span className="mt-1 inline-block rounded bg-amber-500/15 px-1.5 py-0.5 text-[9px] font-bold uppercase tracking-wider text-amber-300">
                Demo
              </span>
            )}
          </div>
          <LanguageToggle className="ml-auto" />
        </div>

        <nav className="flex flex-1 flex-col gap-1">
          {NAV.map((item) => {
            const active = current === item.href;
            const Icon = item.icon;
            return (
              <Link
                key={item.href}
                href={item.href}
                className={cn(
                  "group flex items-center gap-3 rounded-card px-3 py-2.5 text-sm transition-all",
                  active
                    ? "bg-gradient-to-r from-emerald-500/15 to-transparent font-semibold text-emerald-300 shadow-glow-soft ring-1 ring-emerald-500/20"
                    : "text-slate-400 hover:bg-white/5 hover:text-slate-100"
                )}
              >
                <Icon className="h-4 w-4" />
                {tr(item.ja, item.en)}
                {active && (
                  <span className="ml-auto h-1.5 w-1.5 rounded-full bg-emerald-400" />
                )}
              </Link>
            );
          })}
        </nav>

        <div className="rounded-card border border-white/5 bg-white/[0.03] p-3">
          <p className="text-[11px] uppercase tracking-wider text-slate-500">
            Status
          </p>
          <div className="mt-1.5 flex items-center gap-2 text-xs text-slate-300">
            <span className="relative flex h-2 w-2">
              <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-60" />
              <span className="relative inline-flex h-2 w-2 rounded-full bg-emerald-400" />
            </span>
            Core engine active
          </div>

          <button
            type="button"
            onClick={onLogout}
            className="mt-3 inline-flex w-full items-center justify-center gap-2 rounded-card border border-white/5 bg-white/[0.03] px-3 py-2 text-xs font-medium text-slate-400 transition-colors hover:border-red-500/30 hover:bg-red-500/10 hover:text-red-300"
          >
            <LogOut className="h-3.5 w-3.5" />
            {tr("ログアウト", "Log out")}
          </button>

          <a
            href="https://contra.com/neutron_d0wbklm2/work"
            target="_blank"
            rel="noopener noreferrer"
            className="mt-3 block border-t border-white/5 pt-3 text-[11px] leading-relaxed text-slate-500 transition-colors hover:text-slate-300"
          >
            Built by <span className="font-medium text-slate-300">Hisashi Aoki</span>
            <span className="mt-0.5 block text-emerald-400/80">
              {tr("制作の依頼はこちら →", "Hire me on Contra →")}
            </span>
          </a>
        </div>
      </div>
    </aside>
  );
}

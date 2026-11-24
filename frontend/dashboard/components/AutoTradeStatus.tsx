"use client";

import { Power, ShieldCheck, ShieldX } from "lucide-react";
import { api, usePoll } from "@/lib/api";
import { cn } from "@/lib/cn";
import { useI18n } from "@/lib/i18n";

/**
 * Overview最上部の常時稼働ステータス（操作なし）。
 * 自動売買はtraderコンテナが常時担当するため、ON/OFFボタンは持たない。
 */
export default function AutoTradeStatus() {
  const { tr } = useI18n();
  const { data } = usePoll(api.autoTrade, 15000);
  const { data: system } = usePoll(api.system, 15000);
  const stopped = data?.risk_stopped ?? false;
  const trader = system?.trader;
  // trader heartbeat が無い/古い場合は「稼働中」と表示しない。
  // 従来は常時「稼働中」表示で、trader 未起動（profile 未指定の up 等）の
  // 約定ゼロを見逃す原因になっていた。
  const traderLive = trader?.status === "running";
  const traderStale = trader?.status === "stale";

  return (
    <div className="flex flex-wrap items-center justify-between gap-4 rounded-card border border-emerald-500/30 bg-emerald-500/10 px-5 py-4 shadow-card">
      <div className="flex items-center gap-4">
        <span className="grid h-10 w-10 place-items-center rounded-lg bg-emerald-500 text-navy-950">
          <Power className="h-5 w-5" />
        </span>
        <div>
          <p className="text-xs font-medium uppercase tracking-wider text-slate-400">
            {tr("Auto Trade（完全自動・操作不要）", "AUTO TRADE (fully automated)")}
          </p>
          <div className="mt-1 flex flex-wrap items-center gap-2">
            {traderLive ? (
              <span className="inline-flex items-center gap-2 rounded-pill bg-emerald-500/15 px-3 py-1 text-sm font-bold text-emerald-300 ring-1 ring-emerald-500/40">
                <span className="h-2 w-2 rounded-full bg-emerald-400" />
                {tr("稼働中", "RUNNING")}
              </span>
            ) : (
              <span className="inline-flex items-center gap-2 rounded-pill bg-red-500/15 px-3 py-1 text-sm font-bold text-red-300 ring-1 ring-red-500/40">
                <span className="h-2 w-2 rounded-full bg-red-400" />
                {traderStale ? tr("応答なし", "NO RESPONSE") : tr("停止中", "STOPPED")}
              </span>
            )}
            {stopped ? (
              <span className="inline-flex items-center gap-1.5 text-xs font-medium text-red-300">
                <ShieldX className="h-3.5 w-3.5" />
                {tr("Risk Status: 停止", "Risk Status: HALTED")}
              </span>
            ) : (
              <span className="inline-flex items-center gap-1.5 text-xs font-medium text-slate-400">
                <ShieldCheck className="h-3.5 w-3.5 text-emerald-400" />
                {tr("Risk Status: 正常", "Risk Status: Normal")}
              </span>
            )}
            {traderLive ? (
              <span className="text-xs text-slate-400">
                trader {trader?.ticks} {tr("tick", "ticks")} / {trader?.orders} {tr("注文", "orders")}
                {trader?.last_signal ? ` / ${trader.last_signal}` : ""}
              </span>
            ) : (
              <span className="text-xs text-red-300">
                {tr(
                  "trader 未起動（--profile trading で起動・heartbeat なし）",
                  "Trader not running (start with --profile trading; no heartbeat)"
                )}
              </span>
            )}
          </div>
        </div>
      </div>
      <span
        className={cn(
          "text-xs",
          stopped ? "text-red-300" : "text-slate-500"
        )}
      >
        {stopped
          ? tr("日次損失リミットで自動停止中", "Auto-halted by the daily loss limit")
          : tr("シグナル・資金・リスクを自動判定して発注します", "Evaluates signals, capital, and risk before placing orders")}
      </span>
    </div>
  );
}

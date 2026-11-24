"use client";

import { useState } from "react";
import {
  Activity,
  Layers,
  TrendingUp,
  Wallet,
} from "lucide-react";
import PageHeader from "@/components/PageHeader";
import Card from "@/components/Card";
import StatusCard from "@/components/StatusCard";
import Chart from "@/components/Chart";

import { SignalBadge } from "@/components/Badge";
import { api, usePoll } from "@/lib/api";
import { fmt, fmtPLColor, fmtYen, translateReason, translateReasonShort } from "@/lib/format";
import { cn } from "@/lib/cn";
import GmoAffordabilityAlert from "@/components/GmoAffordabilityAlert";
import AutoTradeStatus from "@/components/AutoTradeStatus";
import MarketWeather from "@/components/MarketWeather";
import EquitySourceToggle, { type EquitySource } from "@/components/EquitySourceToggle";

function InfoRow({
  label,
  value,
  ok,
}: {
  label: string;
  value: string;
  ok?: boolean;
}) {
  return (
    <div className="flex items-center justify-between py-1.5">
      <span className="text-slate-500">{label}</span>
      <span className="flex items-center gap-2 text-slate-200">
        {ok !== undefined && (
          <span
            className={cn(
              "h-2 w-2 rounded-full",
              ok ? "bg-emerald-400" : "bg-red-400"
            )}
          />
        )}
        {value}
      </span>
    </div>
  );
}

export default function OverviewPage() {
  const balance = usePoll(api.balance, 15000);
  const equity = usePoll(api.equityLive, 15000);
  const positions = usePoll(api.positions, 15000);
  const signals = usePoll(() => api.signals(), 10000);
  const system = usePoll(api.system, 15000);
  const [equitySource, setEquitySource] = useState<EquitySource>("live");

  const b = balance.data;
  const liveOk = equity.data?.status === "ok";
  const useLive = equitySource === "live" && liveOk;
  const live = equity.data;
  // LIVE: GMO実資産。PAPER: 従来のpaper計算（初期資金100万円基準）。
  const totalAssets = useLive ? live?.total_jpy : b?.total_assets;
  const equityPnl = useLive ? live?.pnl_24h ?? null : b?.profit_loss;
  const totalSub = useLive
    ? live?.stale
      ? "GMO実資産（更新停止中・最終値）"
      : "GMO実資産（24h損益）"
    : `paper（初期資金100万円基準）${(b?.profit_loss_rate ?? 0) >= 0 ? "+" : ""}${fmt(b?.profit_loss_rate)}%`;
  const equityHistory = useLive
    ? (live?.history ?? [])
    : (b?.history ?? []);
  const totalPnl = positions.data?.total_unrealized_pnl ?? 0;
  const coreOk = system.data?.core.status === "running";

  return (
    <>
      <AutoTradeStatus />

      <PageHeader
        title="Overview"
        subtitle="資金・ポジション・シグナルの全体サマリー"
      />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatusCard
          label={useLive ? "Total Assets（GMO実資産）" : "Total Assets（paper）"}
          value={fmtYen(totalAssets)}
          icon={Wallet}
          delta={fmtYen(equityPnl)}
          deltaPositive={(equityPnl ?? 0) >= 0}
          sub={totalSub}
        />
        <StatusCard
          label="Open Positions（保有ポジション）"
          value={String(positions.data?.positions.length ?? 0)}
          icon={Layers}
          delta={fmtYen(totalPnl)}
          deltaPositive={totalPnl >= 0}
          sub="含み損益合計"
          tone="sky"
        />
        <StatusCard
          label="Signals (24h)（シグナル）"
          value={String(signals.data?.signals.length ?? 0)}
          icon={Activity}
          sub="買い / 売り / 様子見"
          tone="amber"
        />
        <StatusCard
          label="API Latency（API 応答速度）"
          value={`${system.data?.api.latency_ms ?? "-"} ms`}
          icon={TrendingUp}
          sub="コアエンジン稼働中"
          tone="navy"
        />
      </div>

      <div className="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-3">
        <Card
          title={useLive ? "Equity Curve（GMO実資産推移）" : "Equity Curve（資産推移・paper）"}
          icon={TrendingUp}
          className="lg:col-span-2"
          action={
            <EquitySourceToggle
              source={equitySource}
              onChange={setEquitySource}
              liveOk={liveOk}
            />
          }
        >
          <Chart
            data={equityHistory.map((p) => ({
              timestamp: p.timestamp,
              balance: p.balance,
            }))}
            height={280}
          />
        </Card>
        <Card title="Recent Signals（直近シグナル）" icon={Activity}>
          <ul className="space-y-3">
            {signals.data?.signals.slice(0, 5).map((s) => (
              <li key={s.id} className="flex items-center justify-between gap-2">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <SignalBadge type={s.type} />
                    <span className="truncate text-sm font-medium text-slate-200">
                      {s.symbol}
                    </span>
                  </div>
                  <p
                    className="mt-0.5 truncate text-xs text-slate-500"
                    title={translateReason(s.reason)}
                  >
                    {translateReasonShort(s.reason)}
                  </p>
                </div>
                <span className="shrink-0 font-mono text-xs text-slate-400">
                  {Math.round(s.confidence * 100)}%
                </span>
              </li>
            ))}
            {(!signals.data || signals.data.signals.length === 0) && (
              <li className="py-6 text-center text-sm text-slate-500">
                シグナルデータがありません
              </li>
            )}
          </ul>
        </Card>
      </div>

      <div className="mt-4">
        <GmoAffordabilityAlert />
      </div>

      <div className="mt-4">
        <MarketWeather />
      </div>

      <div className="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-3">
        <Card title="Open Positions（保有ポジション）" icon={Layers} className="lg:col-span-2">
          <ul className="space-y-3">
            {positions.data?.positions.map((p) => (
              <li
                key={p.symbol}
                className="flex items-center justify-between rounded-lg bg-white/[0.03] px-3 py-2.5"
              >
                <div>
                  <p className="text-sm font-semibold text-slate-100">
                    {p.symbol}
                  </p>
                  <p className="text-xs text-slate-500">
                    {p.size} · avg {fmt(p.avg_price)}
                  </p>
                </div>
                <div className="text-right">
                  <p
                    className={cn(
                      "font-mono text-sm font-medium",
                      fmtPLColor(p.unrealized_pnl)
                    )}
                  >
                    {fmtYen(p.unrealized_pnl)}
                  </p>
                  <p
                    className={cn(
                      "font-mono text-xs",
                      fmtPLColor(p.unrealized_pnl_rate)
                    )}
                  >
                    {p.unrealized_pnl_rate >= 0 ? "+" : ""}
                    {p.unrealized_pnl_rate.toFixed(2)}%
                  </p>
                </div>
              </li>
            ))}
            {(!positions.data || positions.data.positions.length === 0) && (
              <li className="py-6 text-center text-sm text-slate-500">
                保有ポジションはありません
              </li>
            )}
          </ul>
        </Card>
        <Card title="Engine（エンジン）" icon={TrendingUp}>
          <div className="space-y-1 text-sm">
            <InfoRow
              label="Core（コア）"
              value={system.data?.core.status ?? "…"}
              ok={coreOk}
            />
            <InfoRow
              label="API"
              value={system.data?.api.status ?? "…"}
              ok={system.data?.api.status === "ok"}
            />
            <InfoRow
              label="Trader（自動売買）"
              value={`${system.data?.trader?.status ?? "…"} ${system.data?.trader?.ticks ?? 0}tick/${system.data?.trader?.orders ?? 0}注文`}
              ok={system.data?.trader?.status === "running"}
            />
            <InfoRow
              label="Containers（コンテナ）"
              value={String(system.data?.docker.containers.length ?? 0)}
            />
            <InfoRow
              label="Heartbeat（最終応答）"
              value={system.data?.core.last_heartbeat ?? "…"}
            />
          </div>
        </Card>
      </div>
    </>
  );
}

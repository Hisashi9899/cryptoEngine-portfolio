"use client";

import { useState } from "react";
import {
  Banknote,
  PiggyBank,
  TrendingDown,
  TrendingUp,
  Wallet,
} from "lucide-react";
import PageHeader from "@/components/PageHeader";
import Card from "@/components/Card";
import StatusCard from "@/components/StatusCard";
import Chart from "@/components/Chart";
import { LiveBadge } from "@/components/Badge";
import GmoAffordabilityAlert from "@/components/GmoAffordabilityAlert";
import EquitySourceToggle, { type EquitySource } from "@/components/EquitySourceToggle";
import { api, usePoll } from "@/lib/api";
import { fmt, fmtYen } from "@/lib/format";

export default function BalancePage() {
  const { data } = usePoll(api.balance, 15000);
  const equity = usePoll(api.equityLive, 15000);
  const [equitySource, setEquitySource] = useState<EquitySource>("live");
  const liveOk = equity.data?.status === "ok";
  const useLive = equitySource === "live" && liveOk;
  const live = equity.data;
  const total = useLive ? live?.total_jpy : data?.total_assets;
  const available = useLive ? live?.jpy_available : data?.available;
  const invested =
    useLive && total != null && available != null
      ? total - available
      : data?.invested;
  const positive = useLive
    ? (live?.pnl_24h ?? 0) >= 0
    : (data?.profit_loss ?? 0) >= 0;
  const delta = useLive ? live?.pnl_24h : data?.profit_loss;

  return (
    <>
      <PageHeader title="Balance" subtitle="資金状況と評価額推移">
        <LiveBadge />
      </PageHeader>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatusCard
          label={useLive ? "Total Assets（GMO実資産）" : "Total Assets（総資産）"}
          value={fmtYen(total)}
          icon={Wallet}
          delta={fmtYen(delta)}
          deltaPositive={positive}
          sub={
            useLive
              ? live?.stale
                ? "GMO実資産（更新停止中・最終値）"
                : "GMO実資産（24h損益）"
              : `${positive ? "+" : ""}${fmt(data?.profit_loss_rate)}%`
          }
        />
        <StatusCard
          label="Available（利用可能額）"
          value={fmtYen(available)}
          icon={Banknote}
          tone="sky"
        />
        <StatusCard
          label="Invested（投資額）"
          value={fmtYen(invested)}
          icon={TrendingUp}
          tone="amber"
          sub="ポジション評価額"
        />
        <StatusCard
          label="Initial Capital（初期資金）"
          value={useLive ? "—" : fmtYen(data?.initial_capital)}
          icon={PiggyBank}
          tone="navy"
          sub={useLive ? "GMO口座に初期資金の概念なし" : "初期資金"}
        />
      </div>

      <div className="mt-4">
        <GmoAffordabilityAlert />
      </div>

      <div className="mt-4">
        <Card
          title={
            useLive
              ? "Equity Curve（GMO実資産推移）"
              : "Equity Curve（資産推移・paper）"
          }
          icon={positive ? TrendingUp : TrendingDown}
          action={
            <span className="flex items-center gap-3">
              <span className="text-xs text-slate-500">
                {useLive ? "GMO実測スナップショット" : "過去48時間"}
              </span>
              <EquitySourceToggle
                source={equitySource}
                onChange={setEquitySource}
                liveOk={liveOk}
              />
            </span>
          }
        >
          <Chart
            data={((useLive ? live?.history : data?.history) ?? []).map((p) => ({
              timestamp: p.timestamp,
              balance: p.balance,
            }))}
            height={320}
          />
        </Card>
      </div>
    </>
  );
}

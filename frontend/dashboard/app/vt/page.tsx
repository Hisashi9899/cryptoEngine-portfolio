"use client";

import { useState } from "react";
import { Radar } from "lucide-react";
import PageHeader from "@/components/PageHeader";
import Card from "@/components/Card";
import StatusCard from "@/components/StatusCard";
import Table, { type Column } from "@/components/Table";
import { api, usePoll, type VtScoreStat } from "@/lib/api";
import { Activity } from "lucide-react";

type StatRow = { key: string } & VtScoreStat;

function toRows(rec: Record<string, VtScoreStat> | undefined): StatRow[] {
  if (!rec) return [];
  return Object.entries(rec).map(([key, v]) => ({ key, ...v }));
}

const statColumns: (label: string) => Column<StatRow>[] = (label) => [
  {
    key: "key",
    label,
    sortValue: (r) => r.key,
    render: (r) => <span className="font-medium text-slate-100">{r.key}</span>,
  },
  {
    key: "pairs",
    label: "Pairs",
    sortValue: (r) => r.pairs,
    align: "right",
    render: (r) => <span>{r.pairs}</span>,
  },
  {
    key: "wins",
    label: "Wins",
    sortValue: (r) => r.wins,
    align: "right",
    render: (r) => <span className="text-emerald-300">{r.wins}</span>,
  },
  {
    key: "pnl",
    label: "PnL",
    sortValue: (r) => r.pnl,
    align: "right",
    render: (r) => (
      <span className={r.pnl >= 0 ? "text-emerald-300" : "text-red-300"}>
        {r.pnl.toFixed(2)}
      </span>
    ),
  },
  {
    key: "avg_hold_sec",
    label: "AvgHold(s)",
    sortValue: (r) => r.avg_hold_sec,
    align: "right",
    render: (r) => <span className="text-slate-400">{r.avg_hold_sec}</span>,
  },
];

function ScoreTables({ prefix, days }: { prefix: string; days: number }) {
  const { data, error } = usePoll(() => api.vtScore({ days }), 60000);
  const s = data;
  if (error) return <p className="py-8 text-center text-sm text-red-400">{error}</p>;
  if (!s) return <p className="py-8 text-center text-sm text-slate-500">取得中...</p>;
  const winRate = s.total.pairs > 0 ? Math.round((s.total.wins / s.total.pairs) * 100) : 0;
  return (
    <>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatusCard
          label="Pairs（対戦数）"
          value={String(s.total.pairs)}
          icon={Radar}
          tone="navy"
          sub={`win ${winRate}% / unpaired ${s.unpaired_entries}`}
        />
        <StatusCard
          label="Wins（勝ち）"
          value={String(s.total.wins)}
          icon={Activity}
          sub={`${s.files} files / ${s.rows} rows`}
        />
        <StatusCard
          label="PnL（損益）"
          value={s.total.pnl.toFixed(2)}
          icon={Activity}
          tone={s.total.pnl >= 0 ? undefined : "red"}
          sub={`avg hold ${s.total.avg_hold_sec}s`}
        />
        <StatusCard
          label="Scope（範囲）"
          value={`${s.days}d/${s.src}`}
          icon={Activity}
          tone="amber"
          sub="vt_signals集計"
        />
      </div>
      <div className="mt-4 grid grid-cols-1 gap-4 xl:grid-cols-2">
        <Card title="By Pattern（パターン別）" icon={Radar}>
          <Table
            columns={statColumns("Pattern")}
            rows={toRows(s.by_pattern)}
            rowKey={(r) => `${prefix}-pat-${r.key}`}
            defaultSort={{ key: "pnl", dir: "desc" }}
            emptyMessage="データがありません"
          />
        </Card>
        <Card title="By Regime（レジーム別）" icon={Radar}>
          <Table
            columns={statColumns("Regime")}
            rows={toRows(s.by_regime)}
            rowKey={(r) => `${prefix}-reg-${r.key}`}
            defaultSort={{ key: "pnl", dir: "desc" }}
            emptyMessage="データがありません"
          />
        </Card>
        <Card title="By Bars（足種別）" icon={Radar}>
          <Table
            columns={statColumns("Bars")}
            rows={toRows(s.by_bars)}
            rowKey={(r) => `${prefix}-bars-${r.key}`}
            defaultSort={{ key: "pnl", dir: "desc" }}
            emptyMessage="データがありません"
          />
        </Card>
        <Card title="By Exit（出口理由別）" icon={Radar}>
          <Table
            columns={statColumns("Exit")}
            rows={toRows(s.by_exit_reason)}
            rowKey={(r) => `${prefix}-exit-${r.key}`}
            defaultSort={{ key: "pnl", dir: "desc" }}
            emptyMessage="データがありません"
          />
        </Card>
      </div>
    </>
  );
}

export default function VtPage() {
  const [days, setDays] = useState(7);
  const { data: status } = usePoll(() => api.vtStatus(), 60000);
  return (
    <>
      <PageHeader
        title="V.T Monitor"
        subtitle={`V.Tエンジン採点（記録のみ・発注なし / 記録鮮度: ${status?.status ?? "?"} ${status?.latest_age_sec ?? "?"}s前）`}
      />
      <div className="mt-4 flex gap-2 text-xs">
        {([7, 30, 0] as const).map((d) => (
          <button
            key={d}
            type="button"
            onClick={() => setDays(d)}
            className={`rounded-card border px-3 py-1.5 ${
              days === d
                ? "border-emerald-500/40 bg-emerald-500/10 text-emerald-300"
                : "border-white/10 bg-white/[0.03] text-slate-400"
            }`}
          >
            {d === 0 ? "all" : `${d}d`}
          </button>
        ))}
      </div>
      <div className="mt-4">
        <ScoreTables key={days} prefix={`d${days}`} days={days} />
      </div>
    </>
  );
}

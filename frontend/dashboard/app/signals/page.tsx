"use client";

import { Activity, Radar } from "lucide-react";
import PageHeader from "@/components/PageHeader";
import Card from "@/components/Card";
import StatusCard from "@/components/StatusCard";
import Table, { type Column } from "@/components/Table";
import { SignalBadge } from "@/components/Badge";
import { api, usePoll, type Signal } from "@/lib/api";
import { fmt, fmtTime, translateReason, translateReasonShort } from "@/lib/format";

function Confidence({ value }: { value: number }) {
  return (
    <div className="flex w-28 items-center gap-2">
      <div className="h-1.5 flex-1 overflow-hidden rounded-pill bg-white/10">
        <div
          className="h-full rounded-pill bg-gradient-to-r from-emerald-600 to-emerald-300"
          style={{ width: `${Math.round(value * 100)}%` }}
        />
      </div>
      <span className="w-9 font-mono text-xs text-slate-400">
        {Math.round(value * 100)}%
      </span>
    </div>
  );
}

export default function SignalsPage() {
  const { data, error } = usePoll(() => api.signals(), 10000);
  const signals = data?.signals ?? [];
  const count = (t: string) => signals.filter((s) => s.type === t).length;

  const columns: Column<Signal>[] = [
    {
      key: "timestamp",
      label: "Time（時刻）",
      sortValue: (s) => s.timestamp,
      render: (s) => <span className="text-slate-400">{fmtTime(s.timestamp)}</span>,
    },
    {
      key: "symbol",
      label: "Pair（ペア）",
      sortValue: (s) => s.symbol,
      render: (s) => <span className="font-medium text-slate-100">{s.symbol}</span>,
    },
    {
      key: "type",
      label: "Type（種別）",
      sortValue: (s) => s.type,
      align: "center",
      render: (s) => <SignalBadge type={s.type} />,
    },
    {
      key: "confidence",
      label: "Confidence（信頼度）",
      sortValue: (s) => s.confidence,
      render: (s) => <Confidence value={s.confidence} />,
    },
    {
      key: "mid_price",
      label: "Mid（中間価格）",
      sortValue: (s) => s.mid_price,
      align: "right",
      render: (s) => <span>{fmt(s.mid_price, 2)}</span>,
    },
    {
      key: "spread",
      label: "Spread（スプレッド）",
      sortValue: (s) => s.spread,
      align: "right",
      render: (s) => <span>{s.spread.toFixed(6)}</span>,
    },
    {
      key: "reason",
      label: "Reason（理由）",
      render: (s) => (
        <span
          className="block max-w-xs truncate text-slate-500"
          title={translateReason(s.reason)}
        >
          {translateReasonShort(s.reason)}
        </span>
      ),
    },
  ];

  return (
    <>
      <PageHeader
        title="Signals"
        subtitle="v2 GMOエンジンのシグナルログ（GMO現物・BUY/SELLのみ保存）"
      />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatusCard
          label="Signals (24h)（シグナル）"
          value={String(signals.length)}
          icon={Radar}
          tone="navy"
          sub="総シグナル数"
        />
        <StatusCard
          label="Buy（買い）"
          value={String(count("buy"))}
          icon={Activity}
          sub="買いシグナル"
        />
        <StatusCard
          label="Sell（売り）"
          value={String(count("sell"))}
          icon={Activity}
          tone="red"
          sub="売りシグナル"
        />
        <StatusCard
          label="Hold（様子見）"
          value={String(count("hold"))}
          icon={Activity}
          tone="amber"
          sub="様子見"
        />
      </div>

      <div className="mt-4">
        <Card
          title="Signal Log（シグナルログ）"
          icon={Radar}
          action={<span className="text-xs text-slate-500">{signals.length} 件</span>}
        >
          {error ? (
            <p className="py-8 text-center text-sm text-red-400">{error}</p>
          ) : (
            <Table
              columns={columns}
              rows={signals}
              rowKey={(s) => s.id}
              defaultSort={{ key: "timestamp", dir: "desc" }}
              emptyMessage="シグナルデータがありません"
            />
          )}
        </Card>
      </div>
    </>
  );
}

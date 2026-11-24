"use client";

import { Activity, Radar } from "lucide-react";
import PageHeader from "@/components/PageHeader";
import Card from "@/components/Card";
import StatusCard from "@/components/StatusCard";
import Table, { type Column } from "@/components/Table";
import { SignalBadge } from "@/components/Badge";
import { api, usePoll, type Signal } from "@/lib/api";
import { fmt, fmtTime, translateReason, translateReasonShort } from "@/lib/format";
import { useI18n } from "@/lib/i18n";

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
  const { tr } = useI18n();
  const { data, error } = usePoll(() => api.signals(), 10000);
  const signals = data?.signals ?? [];
  const count = (t: string) => signals.filter((s) => s.type === t).length;

  const columns: Column<Signal>[] = [
    {
      key: "timestamp",
      label: tr("Time（時刻）", "Time"),
      sortValue: (s) => s.timestamp,
      render: (s) => <span className="text-slate-400">{fmtTime(s.timestamp)}</span>,
    },
    {
      key: "symbol",
      label: tr("Pair（ペア）", "Pair"),
      sortValue: (s) => s.symbol,
      render: (s) => <span className="font-medium text-slate-100">{s.symbol}</span>,
    },
    {
      key: "type",
      label: tr("Type（種別）", "Type"),
      sortValue: (s) => s.type,
      align: "center",
      render: (s) => <SignalBadge type={s.type} />,
    },
    {
      key: "confidence",
      label: tr("Confidence（信頼度）", "Confidence"),
      sortValue: (s) => s.confidence,
      render: (s) => <Confidence value={s.confidence} />,
    },
    {
      key: "mid_price",
      label: tr("Mid（中間価格）", "Mid"),
      sortValue: (s) => s.mid_price,
      align: "right",
      render: (s) => <span>{fmt(s.mid_price, 2)}</span>,
    },
    {
      key: "spread",
      label: tr("Spread（スプレッド）", "Spread"),
      sortValue: (s) => s.spread,
      align: "right",
      render: (s) => <span>{s.spread.toFixed(6)}</span>,
    },
    {
      key: "reason",
      label: tr("Reason（理由）", "Reason"),
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
        subtitle={tr(
          "v2 GMOエンジンのシグナルログ（GMO現物・BUY/SELLのみ保存）",
          "Signal log from the GMO spot engine (BUY/SELL signals persisted)"
        )}
      />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatusCard
          label={tr("Signals (24h)（シグナル）", "Signals (24h)")}
          value={String(signals.length)}
          icon={Radar}
          tone="navy"
          sub={tr("総シグナル数", "Total signals")}
        />
        <StatusCard
          label={tr("Buy（買い）", "Buy")}
          value={String(count("buy"))}
          icon={Activity}
          sub={tr("買いシグナル", "Buy signals")}
        />
        <StatusCard
          label={tr("Sell（売り）", "Sell")}
          value={String(count("sell"))}
          icon={Activity}
          tone="red"
          sub={tr("売りシグナル", "Sell signals")}
        />
        <StatusCard
          label={tr("Hold（様子見）", "Hold")}
          value={String(count("hold"))}
          icon={Activity}
          tone="amber"
          sub={tr("様子見", "Hold signals")}
        />
      </div>

      <div className="mt-4">
        <Card
          title={tr("Signal Log（シグナルログ）", "Signal Log")}
          icon={Radar}
          action={<span className="text-xs text-slate-500">{signals.length} {tr("件", "rows")}</span>}
        >
          {error ? (
            <p className="py-8 text-center text-sm text-red-400">{error}</p>
          ) : (
            <Table
              columns={columns}
              rows={signals}
              rowKey={(s) => s.id}
              defaultSort={{ key: "timestamp", dir: "desc" }}
              emptyMessage={tr("シグナルデータがありません", "No signal data")}
            />
          )}
        </Card>
      </div>
    </>
  );
}

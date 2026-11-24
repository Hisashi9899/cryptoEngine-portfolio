"use client";

import { useState } from "react";
import { Activity } from "lucide-react";
import PageHeader from "@/components/PageHeader";
import Card from "@/components/Card";
import Table, { type Column } from "@/components/Table";
import { SignalBadge } from "@/components/Badge";
import { api, usePoll, type VtSignal } from "@/lib/api";
import { fmtTime } from "@/lib/format";

const columns: Column<VtSignal>[] = [
  {
    key: "ts",
    label: "Time（時刻）",
    sortValue: (s) => s.ts,
    render: (s) => (
      <span className="text-slate-400">
        {fmtTime(new Date(s.ts).toISOString())}
      </span>
    ),
  },
  {
    key: "symbol",
    label: "Pair",
    sortValue: (s) => `${s.symbol}:${s.bars_sec}`,
    render: (s) => (
      <span className="font-medium text-slate-100">
        {s.symbol} <span className="text-slate-500">/{s.bars_sec}s</span>
      </span>
    ),
  },
  {
    key: "type",
    label: "Type",
    sortValue: (s) => s.type,
    align: "center",
    render: (s) => (
      <SignalBadge type={s.type === "exit" ? "hold" : (s.type as "buy" | "sell" | "hold")} />
    ),
  },
  {
    key: "pattern",
    label: "Pattern",
    sortValue: (s) => s.pattern,
    render: (s) => <span className="text-slate-300">{s.pattern}</span>,
  },
  {
    key: "regime",
    label: "Regime",
    sortValue: (s) => s.regime,
    render: (s) => <span className="text-slate-400">{s.regime || "-"}</span>,
  },
  {
    key: "confidence",
    label: "Conf",
    sortValue: (s) => s.confidence,
    align: "right",
    render: (s) => <span>{Math.round((s.confidence || 0) * 100)}%</span>,
  },
  {
    key: "price",
    label: "Price",
    sortValue: (s) => s.price,
    align: "right",
    render: (s) => <span>{s.price}</span>,
  },
  {
    key: "qty",
    label: "Qty",
    sortValue: (s) => s.qty,
    align: "right",
    render: (s) => (
      <span className={(s.qty || 0) > 0 ? "" : "text-red-400"}>{s.qty}</span>
    ),
  },
  {
    key: "reason",
    label: "Reason",
    render: (s) => (
      <span className="block max-w-xs truncate text-slate-500" title={s.reason}>
        {s.reason}
      </span>
    ),
  },
];

const SYMBOLS = [
  "",
  "BTC",
  "XRP",
  "XLM",
  "BCH",
  "LTC",
  "ADA",
  "ASTR",
  "ATOM",
  "DOGE",
  "DOT",
  "ETH",
  "FCR",
  "LINK",
  "NAC",
  "SOL",
  "SUI",
  "WILD",
];
const BARSS = [0, 60, 300, 900, 3600];

function VtSignalsTable({ symbol, bars }: { symbol: string; bars: number }) {
  const { data, error } = usePoll(
    () => api.vtSignals({ symbol: symbol || undefined, bars: bars || undefined, days: 7, limit: 200 }),
    30000
  );
  const rows = data?.signals ?? [];
  return (
    <Card
      title="V.T Signals（判定記録）"
      icon={Activity}
      action={<span className="text-xs text-slate-500">{data?.count ?? 0} 件</span>}
    >
      {error ? (
        <p className="py-8 text-center text-sm text-red-400">{error}</p>
      ) : (
        <Table
          columns={columns}
          rows={rows}
          rowKey={(r) => `${r.ts}:${r.symbol}:${r.bars_sec}:${r.type}:${r.pattern}`}
          defaultSort={{ key: "ts", dir: "desc" }}
          emptyMessage="V.T記録がありません"
        />
      )}
    </Card>
  );
}

export default function VtSignalsPage() {
  const [symbol, setSymbol] = useState("");
  const [bars, setBars] = useState(0);
  return (
    <>
      <PageHeader
        title="V.T Signals"
        subtitle="V.Tエンジン判定記録（発注なし・qty:0は赤表示）"
      />
      <div className="mt-4 flex flex-wrap gap-2 text-xs">
        {SYMBOLS.map((s) => (
          <button
            key={s || "all"}
            type="button"
            onClick={() => setSymbol(s)}
            className={`rounded-card border px-3 py-1.5 ${
              symbol === s
                ? "border-emerald-500/40 bg-emerald-500/10 text-emerald-300"
                : "border-white/10 bg-white/[0.03] text-slate-400"
            }`}
          >
            {s || "all"}
          </button>
        ))}
      </div>
      <div className="mt-2 flex flex-wrap gap-2 text-xs">
        {BARSS.map((b) => (
          <button
            key={b}
            type="button"
            onClick={() => setBars(b)}
            className={`rounded-card border px-3 py-1.5 ${
              bars === b
                ? "border-emerald-500/40 bg-emerald-500/10 text-emerald-300"
                : "border-white/10 bg-white/[0.03] text-slate-400"
            }`}
          >
            {b === 0 ? "all bars" : `${b}s`}
          </button>
        ))}
      </div>
      <div className="mt-4">
        <VtSignalsTable key={`${symbol}:${bars}`} symbol={symbol} bars={bars} />
      </div>
    </>
  );
}

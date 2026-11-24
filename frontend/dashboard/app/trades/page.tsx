"use client";

import { useMemo, useState } from "react";
import { ArrowLeftRight, Link2 } from "lucide-react";
import PageHeader from "@/components/PageHeader";
import Card from "@/components/Card";
import Table, { type Column } from "@/components/Table";
import { SideBadge, LiveBadge } from "@/components/Badge";
import { api, usePoll, type Trade, type TradePair } from "@/lib/api";
import { fmt, fmtDuration, fmtPLColor, fmtUSDT, fmtYen, fmtTime } from "@/lib/format";
import { cn } from "@/lib/cn";

const SYMBOLS = ["ADA", "ASTR", "ATOM", "BCH", "BTC", "DOGE", "DOT", "ETH", "FCR", "LINK", "LTC", "NAC", "SOL", "SUI", "WILD", "XLM", "XRP", "BTCUSDT", "ETHUSDT", "XRPUSDT", "SOLUSDT", "ADAUSDT"];

const SELECT_CLASS =
  "rounded-lg border border-white/10 bg-navy-800 px-3 py-1.5 text-sm text-slate-200 outline-none transition-colors focus:border-emerald-500/50";

function PnlCell({ value }: { value: number }) {
  return (
    <span className={cn("font-medium", fmtPLColor(value))}>
      {value > 0 ? "+" : ""}
      {fmtYen(value, 0)}
    </span>
  );
}

export default function TradesPage() {
  const { data, error } = usePoll(() => api.trades(), 15000);
  const pairs = usePoll(() => api.tradePairs(), 15000);
  const [symbol, setSymbol] = useState("all");
  const [side, setSide] = useState("all");

  const trades = useMemo(() => {
    const rows = data?.trades ?? [];
    return rows.filter(
      (t) =>
        (symbol === "all" || t.symbol === symbol) &&
        (side === "all" || t.side === side)
    );
  }, [data, symbol, side]);

  const columns: Column<Trade>[] = [
    {
      key: "timestamp",
      label: "Time（時刻）",
      sortValue: (t) => t.timestamp,
      render: (t) => <span className="text-slate-400">{fmtTime(t.timestamp)}</span>,
    },
    {
      key: "symbol",
      label: "Pair（ペア）",
      sortValue: (t) => t.symbol,
      render: (t) => <span className="font-medium text-slate-100">{t.symbol}</span>,
    },
    {
      key: "side",
      label: "Side（売買）",
      sortValue: (t) => t.side,
      align: "center",
      render: (t) => <SideBadge side={t.side} />,
    },
    {
      key: "price",
      label: "Price（価格）",
      sortValue: (t) => t.price,
      align: "right",
      render: (t) => <span>{fmtUSDT(t.price)}</span>,
    },
    {
      key: "volume",
      label: "Volume（数量）",
      sortValue: (t) => t.volume,
      align: "right",
      render: (t) => <span>{fmt(t.volume, 4)}</span>,
    },
    {
      key: "pnl",
      label: "P/L（損益）",
      sortValue: (t) => t.pnl ?? 0,
      align: "right",
      render: (t) =>
        t.pnl === null ? (
          <span
            className="text-gray-400"
            title="この取引はまだ決済されていません"
          >
            未決済
          </span>
        ) : (
          <span className={cn("font-medium", fmtPLColor(t.pnl))}>
            {fmtYen(t.pnl, 0)}
          </span>
        ),
    },
  ];

  const pairColumns: Column<TradePair>[] = [
    {
      key: "symbol",
      label: "Pair（ペア）",
      sortValue: (p) => p.symbol,
      render: (p) => <span className="font-medium text-slate-100">{p.symbol}</span>,
    },
    {
      key: "entry_time",
      label: "Entry Time（エントリー）",
      sortValue: (p) => p.entry_time,
      render: (p) => <span className="text-slate-400">{fmtTime(p.entry_time)}</span>,
    },
    {
      key: "exit_time",
      label: "Exit Time（クローズ）",
      sortValue: (p) => p.exit_time,
      render: (p) => <span className="text-slate-400">{fmtTime(p.exit_time)}</span>,
    },
    {
      key: "entry_price",
      label: "Entry Price",
      sortValue: (p) => p.entry_price,
      align: "right",
      render: (p) => <span>{fmtUSDT(p.entry_price)}</span>,
    },
    {
      key: "exit_price",
      label: "Exit Price",
      sortValue: (p) => p.exit_price,
      align: "right",
      render: (p) => <span>{fmtUSDT(p.exit_price)}</span>,
    },
    {
      key: "pnl",
      label: "P/L（損益）",
      sortValue: (p) => p.pnl,
      align: "right",
      render: (p) => <PnlCell value={p.pnl} />,
    },
    {
      key: "duration_seconds",
      label: "Duration（保有時間）",
      sortValue: (p) => p.duration_seconds,
      align: "right",
      render: (p) => <span>{fmtDuration(p.duration_seconds)}</span>,
    },
  ];

  return (
    <>
      <PageHeader
        title="Trades"
        subtitle="v2 GMOの売買実行履歴（ソート・フィルタ対応）"
      >
        <div className="flex flex-wrap items-center gap-2">
          <LiveBadge />
          <select
            value={symbol}
            onChange={(e) => setSymbol(e.target.value)}
            className={SELECT_CLASS}
          >
            <option value="all">全ペア</option>
            {SYMBOLS.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
          <select
            value={side}
            onChange={(e) => setSide(e.target.value)}
            className={SELECT_CLASS}
          >
            <option value="all">全売買</option>
            <option value="buy">買い（Buy）</option>
            <option value="sell">売り（Sell）</option>
          </select>
        </div>
      </PageHeader>

      <Card
        title="Trade History（取引履歴）"
        icon={ArrowLeftRight}
        action={<span className="text-xs text-slate-500">{trades.length} 件</span>}
      >
        {error ? (
          <p className="py-8 text-center text-sm text-red-400">{error}</p>
        ) : (
          <Table
            columns={columns}
            rows={trades}
            rowKey={(t) => t.id}
            defaultSort={{ key: "timestamp", dir: "desc" }}
            emptyMessage="該当する取引がありません"
          />
        )}
      </Card>

      <Card
        title="Paired Trades（ペアリング済みトレード）"
        icon={Link2}
        action={
          <span className="text-xs text-slate-500">
            {pairs.data?.trade_pairs.length ?? 0} 件
          </span>
        }
      >
        {pairs.error ? (
          <p className="py-8 text-center text-sm text-red-400">{pairs.error}</p>
        ) : (
          <Table
            columns={pairColumns}
            rows={pairs.data?.trade_pairs ?? []}
            rowKey={(p) => p.id}
            defaultSort={{ key: "exit_time", dir: "desc" }}
            emptyMessage="ペアリング済みトレードはまだありません"
          />
        )}
      </Card>
    </>
  );
}

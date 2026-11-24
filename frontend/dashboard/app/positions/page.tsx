"use client";

import { Layers, Percent, TrendingUp } from "lucide-react";
import PageHeader from "@/components/PageHeader";
import Card from "@/components/Card";
import StatusCard from "@/components/StatusCard";
import Table, { type Column } from "@/components/Table";
import { api, usePoll, type Position } from "@/lib/api";
import { LiveBadge } from "@/components/Badge";
import { fmt, fmtPLColor, fmtUSDT, fmtYen } from "@/lib/format";
import { cn } from "@/lib/cn";

export default function PositionsPage() {
  const { data, error } = usePoll(api.positions, 15000);
  const totalPnl = data?.total_unrealized_pnl ?? 0;

  const columns: Column<Position>[] = [
    {
      key: "symbol",
      label: "Pair（ペア）",
      sortValue: (p) => p.symbol,
      render: (p) => <span className="font-medium text-slate-100">{p.symbol}</span>,
    },
    {
      key: "size",
      label: "Size（数量）",
      sortValue: (p) => p.size,
      align: "right",
      render: (p) => <span>{fmt(p.size, 4)}</span>,
    },
    {
      key: "avg_price",
      label: "Avg Price（平均価格）",
      sortValue: (p) => p.avg_price,
      align: "right",
      render: (p) => <span>{fmtUSDT(p.avg_price)}</span>,
    },
    {
      key: "last_price",
      label: "Last Price（現在価格）",
      sortValue: (p) => p.last_price,
      align: "right",
      render: (p) => <span>{fmtUSDT(p.last_price)}</span>,
    },
    {
      key: "unrealized_pnl",
      label: "Unrealized P/L（含み損益）",
      sortValue: (p) => p.unrealized_pnl,
      align: "right",
      render: (p) => (
        <span className={cn("font-medium", fmtPLColor(p.unrealized_pnl))}>
          {fmtYen(p.unrealized_pnl, 0)}
        </span>
      ),
    },
    {
      key: "unrealized_pnl_rate",
      label: "P/L %（損益率）",
      sortValue: (p) => p.unrealized_pnl_rate,
      align: "right",
      render: (p) => (
        <span className={cn("font-mono", fmtPLColor(p.unrealized_pnl_rate))}>
          {p.unrealized_pnl_rate >= 0 ? "+" : ""}
          {p.unrealized_pnl_rate.toFixed(2)}%
        </span>
      ),
    },
  ];

  return (
    <>
      <PageHeader
        title="Positions"
        subtitle="現在保有中のポジションと含み損益"
      >
        <LiveBadge />
      </PageHeader>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
        <StatusCard
          label="Open Positions（保有ポジション）"
          value={String(data?.positions.length ?? 0)}
          icon={Layers}
          tone="sky"
          sub="保有件数"
        />
        <StatusCard
          label="Unrealized P/L（評価損益）"
          value={`${totalPnl >= 0 ? "+" : ""}${fmtYen(totalPnl)}`}
          icon={Percent}
          deltaPositive={totalPnl >= 0}
          sub="評価損益の合計"
          title="保有ポジションの評価損益の合計です"
        />
        <StatusCard
          label="Total Market Value（評価額合計）"
          value={fmtYen(
            data?.positions.reduce((acc, p) => acc + p.market_value, 0)
          )}
          icon={TrendingUp}
          tone="amber"
          sub="ポジション時価"
          title="保有ポジションの評価額の合計です"
        />
      </div>

      <div className="mt-4">
        <Card
          title="Positions（ポジション）"
          icon={TrendingUp}
          action={
            <span className="text-xs text-slate-500">
              {data?.positions.length ?? 0} ポジション
            </span>
          }
        >
          {error ? (
            <p className="py-8 text-center text-sm text-red-400">{error}</p>
          ) : (
            <Table
              columns={columns}
              rows={data?.positions ?? []}
              rowKey={(p) => p.symbol}
              defaultSort={{ key: "unrealized_pnl", dir: "desc" }}
              emptyMessage="保有ポジションはありません"
            />
          )}
        </Card>
      </div>
    </>
  );
}

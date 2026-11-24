"use client";

import { useState } from "react";
import { BookOpenText } from "lucide-react";
import PageHeader from "@/components/PageHeader";
import Card from "@/components/Card";
import StatusCard from "@/components/StatusCard";
import { api, usePoll, type OrderLevel } from "@/lib/api";
import { fmt, fmtUSDT } from "@/lib/format";
import { cn } from "@/lib/cn";

const SYMBOLS = ["ADA", "ASTR", "ATOM", "BCH", "BTC", "DOGE", "DOT", "ETH", "FCR", "LINK", "LTC", "NAC", "SOL", "SUI", "WILD", "XLM", "XRP", "BTCUSDT", "ETHUSDT", "XRPUSDT", "SOLUSDT", "ADAUSDT"];

const SELECT_CLASS =
  "rounded-lg border border-white/10 bg-navy-800 px-3 py-1.5 text-sm text-slate-200 outline-none transition-colors focus:border-emerald-500/50";

export default function OrderbookPage() {
  const [symbol, setSymbol] = useState("BTC");
  const { data, error } = usePoll(() => api.orderbook(symbol), 3000);

  const bids = data?.bids ?? [];
  const asks = data?.asks ?? [];
  const maxDepth = Math.max(
    ...bids.map((b) => b.volume),
    ...asks.map((a) => a.volume),
    1e-9
  );

  return (
    <>
      <PageHeader
        title="Orderbook"
        subtitle={`${symbol} の板情報と約定深度`}
      >
        <select
          value={symbol}
          onChange={(e) => setSymbol(e.target.value)}
          className={SELECT_CLASS}
        >
          {SYMBOLS.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
      </PageHeader>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <StatusCard
          label="Mid Price"
          value={fmtUSDT(data?.mid_price)}
          icon={BookOpenText}
          sub={symbol}
        />
        <StatusCard
          label="Spread"
          value={fmt(data?.spread, 2)}
          icon={BookOpenText}
          tone="amber"
          sub="best ask − best bid"
        />
        <StatusCard
          label="Depth Levels"
          value={`${bids.length + asks.length}`}
          icon={BookOpenText}
          tone="sky"
          sub="bids + asks"
        />
      </div>

      {error && (
        <p className="mt-4 rounded-card border border-red-500/20 bg-red-500/10 px-4 py-3 text-sm text-red-300">
          {error}
        </p>
      )}

      <div className="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Card
          title="Bids"
          icon={BookOpenText}
          action={
            <span className="text-xs font-medium text-emerald-400">
              買い注文
            </span>
          }
        >
          <DepthList levels={bids} side="bid" max={maxDepth} />
        </Card>
        <Card
          title="Asks"
          icon={BookOpenText}
          action={<span className="text-xs font-medium text-red-400">売り注文</span>}
        >
          <DepthList levels={asks} side="ask" max={maxDepth} />
        </Card>
      </div>
    </>
  );
}

function DepthList({
  levels,
  side,
  max,
}: {
  levels: OrderLevel[];
  side: "bid" | "ask";
  max: number;
}) {
  if (levels.length === 0) {
    return <p className="py-8 text-center text-sm text-slate-500">データがありません</p>;
  }
  return (
    <ul className="space-y-1.5">
      {levels.map((l, i) => (
        <li
          key={i}
          className="relative overflow-hidden rounded-lg px-3 py-1.5"
        >
          <div
            className={cn(
              "absolute inset-y-0 right-0 rounded-lg",
              side === "bid" ? "bg-emerald-500/10" : "bg-red-500/10"
            )}
            style={{ width: `${Math.min((l.volume / max) * 100, 100)}%` }}
          />
          <div className="relative flex items-center justify-between text-sm">
            <span
              className={cn(
                "font-mono",
                side === "bid" ? "text-emerald-300" : "text-red-300"
              )}
            >
              {fmtUSDT(l.price)}
            </span>
            <span className="font-mono text-slate-300">{fmt(l.volume, 4)}</span>
          </div>
        </li>
      ))}
    </ul>
  );
}

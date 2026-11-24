"use client";

import { useState } from "react";
import { Percent } from "lucide-react";
import PageHeader from "@/components/PageHeader";
import Card from "@/components/Card";
import Table, { type Column } from "@/components/Table";
import { api, usePoll, type FundingRate } from "@/lib/api";
import { fmtTime } from "@/lib/format";

const columns: Column<FundingRate>[] = [
  {
    key: "exchange",
    label: "Ex",
    sortValue: (r) => `${r.exchange}:${r.symbol}`,
    render: (r) => <span className="text-slate-400">{r.exchange}</span>,
  },
  {
    key: "symbol",
    label: "Symbol",
    sortValue: (r) => r.symbol,
    render: (r) => <span className="font-medium text-slate-100">{r.symbol}</span>,
  },
  {
    key: "funding_rate",
    label: "FR (8h)",
    sortValue: (r) => r.funding_rate,
    align: "right",
    render: (r) => (
      <span className={r.funding_rate >= 0 ? "text-emerald-300" : "text-red-300"}>
        {(r.funding_rate * 100).toFixed(4)}%
      </span>
    ),
  },
  {
    key: "apr_pct",
    label: "APR(推定)",
    sortValue: (r) => r.apr_pct,
    align: "right",
    render: (r) => <span>{r.apr_pct.toFixed(2)}%</span>,
  },
  {
    key: "next_funding_time",
    label: "Next",
    sortValue: (r) => r.next_funding_time,
    render: (r) => <span className="text-slate-400">{fmtTime(r.next_funding_time)}</span>,
  },
];

function FundingTable({ exchange }: { exchange: string }) {
  // exchange切替時はkeyで再マウントさせ、最大60秒の反映待ちを無くす
  const { data, error, loading } = usePoll(() => api.fundingRates(undefined, exchange), 60000);
  const rates = data?.rates ?? [];

  return (
    <Card
      title="Funding Rates（ファンディングレート）"
      icon={Percent}
      action={<span className="text-xs text-slate-500">{rates.length} 件</span>}
    >
      {error ? (
        <p className="py-8 text-center text-sm text-red-400">{error}</p>
      ) : loading && rates.length === 0 ? (
        <p className="py-8 text-center text-sm text-slate-500">取得中...</p>
      ) : (
        <Table
          columns={columns}
          rows={rates}
          rowKey={(r) => `${r.exchange}:${r.symbol}`}
          defaultSort={{ key: "apr_pct", dir: "desc" }}
          emptyMessage="FRデータがありません"
        />
      )}
    </Card>
  );
}

export default function FundingPage() {
  const [exchange, setExchange] = useState("binance");

  return (
    <>
      <PageHeader
        title="Funding"
        subtitle="Binance / Bybit FR監視（読み取り専用・発注なし）"
      />
      <div className="mt-4 flex gap-2 text-xs">
        {(["binance", "bybit", "all"] as const).map((ex) => (
          <button
            key={ex}
            type="button"
            onClick={() => setExchange(ex)}
            className={`rounded-card border px-3 py-1.5 ${
              exchange === ex
                ? "border-emerald-500/40 bg-emerald-500/10 text-emerald-300"
                : "border-white/10 bg-white/[0.03] text-slate-400"
            }`}
          >
            {ex}
          </button>
        ))}
      </div>
      <div className="mt-4">
        <FundingTable key={exchange} exchange={exchange} />
        <p className="mt-2 text-xs text-slate-500">
          APRは FR×3×365 の推定値。マイナスは支払い側。実弾裁定の前に乖離・手数料・清算距離の確認が必要です。
        </p>
      </div>
    </>
  );
}

"use client";

import { AlertTriangle, History, Power, ShieldAlert, Wallet } from "lucide-react";
import Card from "@/components/Card";
import StatusCard from "@/components/StatusCard";
import Table, { type Column } from "@/components/Table";
import Badge from "@/components/Badge";
import {
  api,
  usePoll,
  type AffordabilitySymbol,
  type AutoTradeData,
  type AutoTradeLogEntry,
  type Position,
} from "@/lib/api";
import { fmt, fmtTime, fmtUSDT, fmtYen, fmtPLColor } from "@/lib/format";
import { cn } from "@/lib/cn";

const REASON_LABELS: Record<string, string> = {
  signal: "シグナル",
  tp: "利確",
  sl: "損切り",
  risk: "リスク",
};

export default function AutoTradePanel() {
  const { data, error } = usePoll(api.autoTrade, 5000);
  const { data: funds } = usePoll(api.affordability, 30000);
  const { data: system } = usePoll(api.system, 15000);

  const stopped = data?.risk_stopped ?? false;
  const dailyLoss = data?.risk.daily_loss_usdt ?? 0;
  const maxLoss = data?.risk.max_daily_loss_usdt ?? 0;
  const trader = system?.trader;

  const positionColumns: Column<Position>[] = [
    {
      key: "symbol",
      label: "Pair（ペア）",
      render: (p) => <span className="font-medium text-slate-100">{p.symbol}</span>,
    },
    {
      key: "size",
      label: "Size（数量）",
      align: "right",
      render: (p) => <span>{fmt(p.size, 6)}</span>,
    },
    {
      key: "avg_price",
      label: "Avg Price（平均価格）",
      align: "right",
      render: (p) => <span>{fmtUSDT(p.avg_price)}</span>,
    },
    {
      key: "market_value",
      label: "Market Value（時価）",
      align: "right",
      render: (p) => <span>{fmtUSDT(p.market_value)}</span>,
    },
    {
      key: "unrealized_pnl",
      label: "P/L（損益）",
      align: "right",
      render: (p) => (
        <span className={cn("font-medium", fmtPLColor(p.unrealized_pnl))}>
          {fmtUSDT(p.unrealized_pnl)}
        </span>
      ),
    },
  ];

  const logColumns: Column<AutoTradeLogEntry>[] = [
    {
      key: "timestamp",
      label: "Time（時刻）",
      render: (e) => <span className="text-slate-400">{fmtTime(e.timestamp)}</span>,
    },
    {
      key: "action",
      label: "Action（売買）",
      render: (e) =>
        e.action === "BUY" ? (
          <Badge tone="emerald">▲ BUY</Badge>
        ) : e.action === "SELL" ? (
          <Badge tone="red">▼ SELL</Badge>
        ) : (
          <Badge tone="slate">{e.action}</Badge>
        ),
    },
    {
      key: "symbol",
      label: "Pair（ペア）",
      render: (e) => <span className="font-mono text-xs text-slate-300">{e.symbol}</span>,
    },
    {
      key: "price",
      label: "Price（価格）",
      align: "right",
      render: (e) => <span>{e.price != null ? fmt(e.price, 2) : "-"}</span>,
    },
    {
      key: "amount",
      label: "Amount（数量）",
      align: "right",
      render: (e) => <span>{e.amount != null ? fmt(e.amount, 6) : "-"}</span>,
    },
    {
      key: "pnl",
      label: "P/L（損益）",
      align: "right",
      render: (e) =>
        e.pnl == null ? (
          <span className="text-slate-500">-</span>
        ) : (
          <span className={cn("font-medium", fmtPLColor(e.pnl))}>
            {e.pnl >= 0 ? "+" : ""}
            {fmt(e.pnl, 2)} USDT
          </span>
        ),
    },
    {
      key: "reason",
      label: "Reason（理由）",
      render: (e) => (
        <span className="text-slate-400">{REASON_LABELS[e.reason] ?? e.reason}</span>
      ),
    },
    {
      key: "order_id",
      label: "Order ID（注文ID）",
      render: (e) => (
        <span className="font-mono text-xs text-slate-500">{e.order_id || "-"}</span>
      ),
    },
    {
      key: "status",
      label: "Status（状態）",
      render: (e) =>
        e.status === "filled" ? (
          <Badge tone="emerald">filled</Badge>
        ) : e.status === "rejected" ? (
          <Badge tone="red">rejected</Badge>
        ) : (
          <Badge tone="slate">{e.status}</Badge>
        ),
    },
  ];

  const lossTone = dailyLoss <= -maxLoss ? "red" : dailyLoss < 0 ? "amber" : "emerald";

  const fundColumns: Column<AffordabilitySymbol>[] = [
    {
      key: "symbol",
      label: "Pair（ペア）",
      render: (s) => <span className="font-mono text-xs text-slate-100">{s.symbol}</span>,
    },
    {
      key: "min_qty",
      label: "最小数量",
      align: "right",
      render: (s) => <span>{fmt(s.min_qty, s.min_qty < 1 ? 4 : 0)}</span>,
    },
    {
      key: "min_jpy",
      label: "必要額（約）",
      align: "right",
      render: (s) => <span>{s.price > 0 ? fmtYen(s.min_jpy) : "-"}</span>,
    },
    {
      key: "affordable",
      label: "取引可否",
      render: (s) =>
        s.price <= 0 ? (
          <Badge tone="slate">価格取得失敗</Badge>
        ) : s.affordable ? (
          <Badge tone="emerald">取引可</Badge>
        ) : (
          <Badge tone="red">資金不足</Badge>
        ),
    },
  ];

  const fundRows = [...(funds?.symbols ?? [])].sort((a, b) =>
    a.symbol.localeCompare(b.symbol)
  );

  return (
    <>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatusCard
          label="Auto Trade（自動売買）"
          value={
            trader?.status === "running"
              ? "稼働中"
              : trader?.status === "stale"
                ? "応答なし"
                : "停止中"
          }
          icon={Power}
          tone={trader?.status === "running" ? "emerald" : "red"}
          sub={
            trader?.status === "running"
              ? `常時自動・${trader.ticks}tick/${trader.orders}注文`
              : stopped
                ? "リスク停止中"
                : "trader 未起動（--profile trading）"
          }
        />
        <StatusCard
          label="Risk Stop（リスク停止）"
          value={stopped ? "STOPPED" : "ACTIVE"}
          icon={ShieldAlert}
          tone={stopped ? "red" : "emerald"}
          sub={`STOP_ON_LOSS: ${data?.risk.stop_on_loss ? "true" : "false"}`}
        />
        <StatusCard
          label="Today P/L（本日損益）"
          value={`${dailyLoss >= 0 ? "+" : ""}${fmt(dailyLoss, 2)} USDT`}
          icon={AlertTriangle}
          tone={lossTone}
          sub={`リミット: ${fmt(maxLoss, 2)} USDT`}
        />
        <StatusCard
          label="Position（保有）"
          value={String(data?.positions.length ?? 0)}
          icon={History}
          tone="sky"
          sub={`Trade Size: ${fmt(data?.risk.trade_size_usdt ?? 0, 0)} USDT`}
        />
      </div>

      <div className="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Card title="Auto Trade Status（自動売買状態・操作不要）" icon={Power}>
          <div className="flex flex-wrap items-center justify-between gap-4">
            <div>
              <p className="text-sm text-slate-300">
                現在の状態:
                <Badge
                  tone={trader?.status === "running" ? "emerald" : "red"}
                  className="ml-2"
                >
                  {trader?.status === "running"
                    ? "稼働中（完全自動）"
                    : trader?.status === "stale"
                      ? "応答なし（heartbeat 古い）"
                      : "停止中（trader 未起動）"}
                </Badge>
                {stopped && (
                  <Badge tone="red" className="ml-2">
                    日次損失リミットで停止
                  </Badge>
                )}
              </p>
              <p className="mt-2 text-xs text-slate-500">
                シグナル検出時に取引所へ自動発注します（操作不要）。
                残高不足の場合は発注せず、資金・取引可否に理由が表示されます。
                リスク上限（最大ポジション / 日次損失）は core 側で自動判定されます。
              </p>
            </div>
          </div>

          <div className="mt-4 grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
            {[
              {
                label: "最大ポジション",
                value: fmt(data?.risk.max_position_size_usdt ?? 0, 0),
                unit: "USDT",
              },
              {
                label: "注文サイズ",
                value: fmt(data?.risk.trade_size_usdt ?? 0, 0),
                unit: "USDT",
              },
              {
                label: "日次損失リミット",
                value: fmt(data?.risk.max_daily_loss_usdt ?? 0, 0),
                unit: "USDT",
              },
              {
                label: "本日損益",
                value: `${dailyLoss >= 0 ? "+" : ""}${fmt(dailyLoss, 2)}`,
                unit: "USDT",
              },
            ].map((item) => (
              <div
                key={item.label}
                className="rounded-lg border border-white/5 bg-navy-850/60 px-3 py-2"
              >
                <div className="text-xs text-slate-500">{item.label}</div>
                <div className="mt-1 font-mono text-sm text-slate-200">
                  {item.value} <span className="text-xs text-slate-500">{item.unit}</span>
                </div>
              </div>
            ))}
          </div>

          {error && <p className="mt-4 text-sm text-red-400">{error}</p>}
        </Card>

        <Card title="Positions（現在ポジション）" icon={History}>
          <Table
            columns={positionColumns}
            rows={data?.positions ?? []}
            rowKey={(p) => p.symbol}
            emptyMessage="保有ポジションはありません"
          />
        </Card>
      </div>

      <div className="mt-4">
        <Card
          title="Funds（資金・取引可否）"
          icon={Wallet}
          action={
            <span className="text-xs text-slate-500">
              {funds?.status === "ok"
                ? `JPY余力: ${fmtYen(funds.jpy_available)}`
                : "GMO未設定または取得失敗"}
            </span>
          }
        >
          {funds?.status !== "ok" ? (
            <p className="text-sm text-slate-400">
              {funds?.message ??
                "口座余力を取得できません。GMO_API_KEY の設定を確認してください。"}
            </p>
          ) : (
            <>
              <p className="mb-3 text-sm text-slate-300">
                JPY余力{" "}
                <span className="font-mono font-semibold text-slate-100">
                  {fmtYen(funds.jpy_available)}
                </span>{" "}
                で取引可能な銘柄は
                {(funds.affordable_symbols ?? []).length > 0 ? (
                  <span className="font-medium text-emerald-400">
                    {(funds.affordable_symbols ?? []).join("、")}
                  </span>
                ) : (
                  <span className="font-medium text-red-400">
                    ありません（入金が必要です）
                  </span>
                )}
                です。
              </p>
              <Table
                columns={fundColumns}
                rows={fundRows}
                rowKey={(s) => s.symbol}
                emptyMessage="銘柄情報を取得できません"
              />
            </>
          )}
        </Card>
      </div>

      <div className="mt-4">
        <Card
          title="Auto Trade Log（自動売買ログ）"
          icon={History}
          action={
            <span className="text-xs text-slate-500">
              直近 {data?.trade_log.length ?? 0} 件
            </span>
          }
        >
          <Table
            columns={logColumns}
            rows={data?.trade_log ?? []}
            rowKey={(e, i) => `${e.timestamp}-${i}`}
            emptyMessage="まだ自動売買ログはありません"
          />
        </Card>
      </div>
    </>
  );
}

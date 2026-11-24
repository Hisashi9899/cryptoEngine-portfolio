"use client";

import { useEffect, useRef, useState } from "react";
import { ApiError, fetchWithAuth, httpGet, httpPost } from "@/lib/http";

export { API_BASE, ApiError } from "@/lib/http";

export type BalanceHistoryPoint = { timestamp: string; balance: number };

export type BalanceData = {
  currency: string;
  total_assets: number;
  available: number;
  invested: number;
  profit_loss: number;
  profit_loss_rate: number;
  initial_capital: number;
  history: BalanceHistoryPoint[];
};

export type Trade = {
  id: string;
  timestamp: string;
  symbol: string;
  side: "buy" | "sell";
  price: number;
  volume: number;
  pnl: number | null;
};

export type TradePair = {
  id: string;
  symbol: string;
  entry_side: string;
  exit_side: string;
  entry_price: number;
  exit_price: number;
  entry_time: string;
  exit_time: string;
  entry_volume: number;
  exit_volume: number;
  pnl: number;
  duration_seconds: number;
};

export type Position = {
  symbol: string;
  size: number;
  avg_price: number;
  last_price: number;
  market_value: number;
  unrealized_pnl: number;
  unrealized_pnl_rate: number;
};

export type Signal = {
  id: string;
  timestamp: string;
  symbol: string;
  type: "buy" | "sell" | "hold";
  confidence: number;
  reason: string;
  mid_price: number;
  spread: number;
};

export type OrderLevel = { price: number; volume: number };

export type PairedTrade = {
  symbol: string;
  entry_time: number;
  exit_time: number;
  entry_price: number;
  exit_price: number;
  volume: number;
  pnl: number;
  duration_seconds: number;
  close_reason?: string;
};

export type ConfidenceStats = {
  average: number;
  median: number;
  min: number;
  max: number;
  count: number;
};

export type BacktestResult = {
  total_signals: number;
  buy_signals: number;
  sell_signals: number;
  hold_signals: number;
  paired_trades: PairedTrade[];
  win_rate: number;
  total_pnl: number;
  max_drawdown: number;
  max_win: number;
  max_loss: number;
  average_holding_seconds: number;
  close_reasons?: Record<string, number>;
  confidence_stats: ConfidenceStats;
};

export type OrderbookData = {
  symbol: string;
  mid_price: number;
  spread: number;
  timestamp: number;
  bids: OrderLevel[];
  asks: OrderLevel[];
};

export type ContainerInfo = {
  name: string;
  image: string;
  status: string;
  uptime: string;
};

export type ErrorLogEntry = {
  timestamp: string;
  level: string;
  message: string;
};

export type SystemData = {
  core: { status: string; version: string; uptime_sec: number; last_heartbeat: string };
  api: { status: string; latency_ms: number; requests_1h: number };
  docker: { containers: ContainerInfo[] };
  errors: ErrorLogEntry[];
  trader?: { status: string; ticks: number; orders: number; last_signal: string; age_sec: number };
};

export type SweepParams = {
  imbalance_threshold: number;
  trend_window: number;
  min_hold_seconds: number;
  max_hold_seconds: number;
  take_profit_usdt: number;
  stop_loss_usdt: number;
  entry_mode: string;
  label: string;
};

export type SweepRun = {
  params: SweepParams;
  result: BacktestResult;
};

export type SweepResults = {
  csv: string;
  generated_at: number;
  rows: number;
  runs: SweepRun[];
};

export type CaptureStatus = {
  status: "idle" | "capturing" | "done" | "error";
  minutes: number | null;
  elapsed_seconds: number | null;
  target_seconds: number | null;
  rows: number | null;
  error: string | null;
  out_file: string;
};

export type AutoTradeLogEntry = {
  timestamp: string;
  action: string;
  symbol: string;
  price: number | null;
  amount: number | null;
  pnl: number | null;
  reason: string;
  order_id: string;
  status: string;
};

export type AutoTradeRisk = {
  max_position_size_usdt: number;
  max_daily_loss_usdt: number;
  trade_size_usdt: number;
  stop_on_loss: boolean;
  daily_loss_usdt: number;
};

export type AutoTradeData = {
  enabled: boolean;
  risk_stopped: boolean;
  risk: AutoTradeRisk;
  positions: Position[];
  trade_log: AutoTradeLogEntry[];
};

export type AutoTradeToggleResult = {
  enabled: boolean;
  risk_stopped: boolean;
};

export type AffordabilitySymbol = {
  symbol: string;
  min_qty: number;
  price: number;
  min_jpy: number;
  affordable: boolean;
};

export type AffordabilityData = {
  status: string;
  jpy_available?: number;
  fee_bps?: number;
  symbols?: AffordabilitySymbol[];
  affordable_symbols?: string[];
  message?: string;
};

export type GmoAsset = {
  symbol: string;
  available: string;
  balance?: string;
  locked?: string;
};

export type GmoBalanceData = {
  status: string;
  assets?: GmoAsset[];
  jpy_available?: number;
  message?: string;
  detail?: string;
};

export type EquityHolding = {
  symbol: string;
  balance: number;
  price_jpy: number;
  value_jpy: number;
};

export type EquityLiveData = {
  status: string;
  source?: string;
  stale?: boolean;
  total_jpy?: number;
  jpy_available?: number | null;
  jpy_balance?: number | null;
  holdings?: EquityHolding[];
  updated_at?: string;
  pnl_24h?: number | null;
  history?: BalanceHistoryPoint[];
  message?: string;
};

export type FundingRate = {
  exchange: string;
  symbol: string;
  funding_rate: number;
  funding_time: string;
  next_funding_time: string;
  mark_price: number | null;
  apr_pct: number;
};

export type VtSignal = {
  ts: number;
  symbol: string;
  type: string;
  price: number;
  reason: string;
  pattern: string;
  regime: string;
  confidence: number;
  rsi: number;
  percent_b: number;
  macd_hist: number;
  qty: number;
  mid_price: number;
  raw_score: number;
  entry_time?: number;
  entry_pattern?: string;
  bars_sec: number;
};

export type VtScoreStat = {
  pairs: number;
  wins: number;
  pnl: number;
  avg_hold_sec: number;
};

export type VtScore = {
  total: VtScoreStat;
  by_symbol: Record<string, VtScoreStat>;
  by_pattern: Record<string, VtScoreStat>;
  by_regime: Record<string, VtScoreStat>;
  by_exit_reason: Record<string, VtScoreStat>;
  by_bars: Record<string, VtScoreStat>;
  files: number;
  rows: number;
  unpaired_entries: number;
  days: number;
  src: string;
};

export type VtStatus = {
  files: number;
  total_bytes: number;
  latest_age_sec: number;
  status: string;
};

export const api = {
  balance: () => httpGet<BalanceData>("/api/v1/balance"),
  trades: (symbol?: string, side?: string) => {
    const q = new URLSearchParams();
    if (symbol) q.set("symbol", symbol);
    if (side) q.set("side", side);
    const qs = q.toString();
    return httpGet<{ trades: Trade[] }>(`/api/v1/trades${qs ? `?${qs}` : ""}`);
  },
  tradePairs: (limit = 100) =>
    httpGet<{ trade_pairs: TradePair[] }>(
      `/api/v1/trade-pairs?limit=${limit}`
    ),
  positions: () =>
    httpGet<{ positions: Position[]; total_unrealized_pnl: number }>(
      "/api/v1/positions"
    ),
  signals: (symbol?: string) => {
    const qs = symbol ? `?symbol=${symbol}` : "";
    return httpGet<{ signals: Signal[] }>(`/api/v1/signals${qs}`);
  },
  orderbook: async (symbol = "BTCUSDT") => {
    const res = await httpGet<{ order_book: OrderbookData; latency_ms: number }>(
      `/api/v1/orderbook?symbol=${symbol}`
    );
    return res.order_book;
  },
  backtest: async (file: File): Promise<BacktestResult> => {
    const form = new FormData();
    form.append("file", file);
    const res = await fetchWithAuth("/api/v1/backtest", {
      method: "POST",
      body: form,
    });
    if (!res.ok) {
      const detail = await res.text();
      throw new ApiError(res.status, `Backtest error (${res.status}): ${detail}`);
    }
    return res.json() as Promise<BacktestResult>;
  },
  startCapture: (minutes: number) =>
    httpPost<{ status: string; minutes: number; seconds: number; out_file: string }>(
      "/api/v1/local-verify/capture",
      { minutes }
    ),
  captureStatus: () =>
    httpGet<CaptureStatus>("/api/v1/local-verify/capture"),
  runSweep: () => httpPost<SweepResults>("/api/v1/local-verify/sweep"),
  sweepResults: () => httpGet<SweepResults>("/api/v1/local-verify/sweep"),
  runReport: () =>
    httpPost<{ status: string; out_file: string }>("/api/v1/local-verify/report"),
  report: () => httpGet<{ content: string }>("/api/v1/local-verify/report"),
  system: () => httpGet<SystemData>("/api/v1/system"),
  autoTrade: () => httpGet<AutoTradeData>("/api/v1/auto-trade"),
  autoTradeToggle: (enabled: boolean) =>
    httpPost<AutoTradeToggleResult>("/api/v1/auto-trade/toggle", { enabled }),
  affordability: () =>
    httpGet<AffordabilityData>("/api/v1/auto-trade/affordability"),
  gmoBalance: () => httpGet<GmoBalanceData>("/api/v1/gmo/balance"),
  equityLive: () => httpGet<EquityLiveData>("/api/v1/equity/live"),
  fundingRates: (symbols?: string, exchange = "binance") => {
    const q = new URLSearchParams();
    if (symbols) q.set("symbols", symbols);
    if (exchange) q.set("exchange", exchange);
    const qs = q.toString();
    return httpGet<{ rates: FundingRate[]; errors: { symbol: string; message: string }[]; exchange: string }>(
      `/api/v1/funding/rates${qs ? `?${qs}` : ""}`
    );
  },
  gridEvaluate: (params: {
    lower: number;
    upper: number;
    grids: number;
    current_price?: number;
    amount_per_grid?: number;
    fee_bps?: number;
    mode?: string;
  }) =>
    httpPost<{
      ok: boolean;
      levels?: number[];
      net_per_round?: number;
      gross_per_round?: number;
      fee_per_round?: number;
      buy_levels?: number;
      sell_levels?: number;
      warnings?: string[];
      message?: string;
    }>("/api/v1/grid/evaluate", params),
  vtSignals: (opts?: { symbol?: string; bars?: number; days?: number; limit?: number; src?: string }) => {
    const q = new URLSearchParams();
    if (opts?.symbol) q.set("symbol", opts.symbol);
    if (opts?.bars) q.set("bars", String(opts.bars));
    if (opts?.days !== undefined) q.set("days", String(opts.days));
    if (opts?.limit) q.set("limit", String(opts.limit));
    if (opts?.src) q.set("src", opts.src);
    const qs = q.toString();
    return httpGet<{ signals: VtSignal[]; count: number }>(
      `/api/v1/vt/signals${qs ? `?${qs}` : ""}`
    );
  },
  vtScore: (opts?: { days?: number; src?: string; bars?: number }) => {
    const q = new URLSearchParams();
    if (opts?.days !== undefined) q.set("days", String(opts.days));
    if (opts?.src) q.set("src", opts.src);
    if (opts?.bars) q.set("bars", String(opts.bars));
    const qs = q.toString();
    return httpGet<VtScore>(`/api/v1/vt/score${qs ? `?${qs}` : ""}`);
  },
  vtStatus: () => httpGet<VtStatus>("/api/v1/vt/status"),
};

export type WeatherSymbol = {
  regime: string;
  regime_conf: number;
  raw: number;
  cal: number;
  dir: number;
  flat: string;
  mid: number;
  approaching: boolean;
};

export type MarketWeatherData = {
  status: string;
  age_sec: number;
  count: number;
  approaching: string[];
  symbols: Record<string, WeatherSymbol>;
};

export const marketWeatherApi = {
  weather: () => httpGet<MarketWeatherData>("/api/v1/market-weather"),
};

export function usePoll<T>(fetcher: () => Promise<T>, intervalMs: number) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const fetcherRef = useRef(fetcher);

  useEffect(() => {
    fetcherRef.current = fetcher;
  }, [fetcher]);

  useEffect(() => {
    let active = true;
    const run = async () => {
      try {
        const result = await fetcherRef.current();
        if (active) {
          setData(result);
          setError(null);
        }
      } catch (e) {
        if (active) setError(e instanceof Error ? e.message : String(e));
      } finally {
        if (active) setLoading(false);
      }
    };
    run();
    const timer = setInterval(run, intervalMs);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [intervalMs]);

  return { data, error, loading };
}

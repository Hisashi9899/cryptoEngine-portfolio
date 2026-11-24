"use client";

/**
 * Deterministic demo data for the static GitHub Pages preview.
 *
 * Enabled only when NEXT_PUBLIC_DEMO_MODE=1 at build time. The normal app
 * always talks to the FastAPI backend. No real account, exchange key, or
 * network request is involved in demo mode.
 */

export const DEMO_MODE = process.env.NEXT_PUBLIC_DEMO_MODE === "1";

const NOW = Date.now();
const SYMBOLS = [
  "BTC", "ETH", "BCH", "LTC", "XRP", "XLM", "DOT", "ATOM", "ADA",
  "LINK", "DOGE", "SOL", "ASTR", "NAC", "WILD", "SUI", "FCR",
];
const PRICE: Record<string, number> = {
  BTC: 10_500_000, ETH: 350_000, BCH: 36_000, LTC: 680, XRP: 85,
  XLM: 15, DOT: 1_200, ATOM: 800, ADA: 180, LINK: 1_400,
  DOGE: 9.3, SOL: 28_000, ASTR: 12, NAC: 300, WILD: 40, SUI: 500, FCR: 200,
};

function rng(seed: number) {
  let s = seed >>> 0;
  return () => {
    s = (s * 1664525 + 1013904223) >>> 0;
    return s / 4294967296;
  };
}
const rnd = rng(20261124);

function iso(ms: number) {
  return new Date(ms).toISOString();
}
function round(value: number, digits = 2) {
  const f = 10 ** digits;
  return Math.round(value * f) / f;
}
function pick<T>(items: T[]): T {
  return items[Math.floor(rnd() * items.length)];
}

const BUY_PATTERNS = [
  "buy pressure", "momentum breakout", "pullback to MA", "double bottom",
  "RCI turn up", "Bollinger breakout",
];
const SELL_PATTERNS = [
  "sell pressure", "lower high break", "RCI turn down", "bearish engulfing",
  "three crows", "Bollinger breakdown",
];
const HOLD_PATTERNS = ["no clear signal", "range compression", "waiting for confirmation"];

function makeSignals(count = 40) {
  return Array.from({ length: count }, (_, i) => {
    const symbol = pick(SYMBOLS);
    const type = i % 7 === 0 ? "hold" : i % 2 === 0 ? "buy" : "sell";
    const reason =
      type === "buy" ? pick(BUY_PATTERNS)
      : type === "sell" ? pick(SELL_PATTERNS)
      : pick(HOLD_PATTERNS);
    return {
      id: `S${String(i + 1).padStart(4, "0")}`,
      timestamp: iso(NOW - i * 37 * 60 * 1000 - Math.floor(rnd() * 20 * 60 * 1000)),
      symbol,
      type,
      confidence: round(0.35 + rnd() * 0.6, 2),
      reason,
      mid_price: round(PRICE[symbol] * (1 + (rnd() - 0.5) * 0.004), 2),
      spread: round(0.00008 + rnd() * 0.00016, 6),
    };
  });
}

function makeTrades(count = 40) {
  return Array.from({ length: count }, (_, i) => {
    const symbol = pick(SYMBOLS);
    const side = i % 2 === 0 ? "buy" : "sell";
    return {
      id: `T${String(i + 1).padStart(5, "0")}`,
      timestamp: iso(NOW - i * 41 * 60 * 1000 - Math.floor(rnd() * 15 * 60 * 1000)),
      symbol,
      side,
      price: round(PRICE[symbol] * (1 + (rnd() - 0.5) * 0.006), 2),
      volume: round(0.01 + rnd() * 1.5, 4),
      pnl: side === "sell" ? round((rnd() - 0.42) * 9000, 2) : null,
    };
  });
}

function makeTradePairs(count = 20) {
  return Array.from({ length: count }, (_, i) => {
    const symbol = pick(SYMBOLS);
    const entry = PRICE[symbol] * (1 + (rnd() - 0.5) * 0.004);
    const pnl = round((rnd() - 0.38) * 12000, 2);
    const exit = entry + pnl / 1.2;
    const entryMs = NOW - (i + 1) * 2.4 * 3600 * 1000;
    const duration = 900 + Math.floor(rnd() * 9000);
    return {
      id: `P${String(i + 1).padStart(4, "0")}`,
      symbol,
      entry_side: "buy",
      exit_side: "sell",
      entry_price: round(entry, 2),
      exit_price: round(exit, 2),
      entry_time: iso(entryMs),
      exit_time: iso(entryMs + duration * 1000),
      entry_volume: 1.2,
      exit_volume: 1.2,
      pnl,
      duration_seconds: duration,
    };
  });
}

function makePositions() {
  const specs = [
    { symbol: "BTC", size: 0.01, avg: 10_200_000 },
    { symbol: "ETH", size: 0.3, avg: 340_000 },
    { symbol: "SOL", size: 1.2, avg: 27_500 },
  ];
  return specs.map((p) => {
    const last = PRICE[p.symbol];
    const market = last * p.size;
    const pnl = (last - p.avg) * p.size;
    return {
      symbol: p.symbol,
      size: p.size,
      volume: p.size,
      avg_price: p.avg,
      last_price: last,
      market_value: round(market, 2),
      unrealized_pnl: round(pnl, 2),
      unrealized_pnl_rate: round((pnl / (p.avg * p.size)) * 100, 2),
    };
  });
}

function makeBalance() {
  const initial = 1_000_000;
  const total = 1_258_430.75;
  const points = 48;
  const values: number[] = [initial];
  for (let i = 1; i < points; i += 1) {
    values.push(values[i - 1] * (1 + (rnd() - 0.35) * 0.012));
  }
  const scale = total / values[values.length - 1];
  const history = values.map((v, i) => ({
    timestamp: iso(NOW - 48 * 3600 * 1000 + (i * 48 * 3600 * 1000) / (points - 1)),
    balance: round(v * scale, 2),
  }));
  return {
    currency: "JPY",
    total_assets: total,
    available: round(total * 0.62, 2),
    invested: round(total * 0.38, 2),
    profit_loss: round(total - initial, 2),
    profit_loss_rate: round(((total - initial) / initial) * 100, 2),
    initial_capital: initial,
    history,
  };
}

function makeSystem() {
  return {
    core: {
      status: "running",
      version: "v1.0.0-demo",
      uptime_sec: 1_284_300,
      last_heartbeat: iso(NOW - 4000),
    },
    api: { status: "ok", latency_ms: 9, requests_1h: 1842 },
    docker: {
      containers: [
        { name: "api", image: "cryptoengine-api", status: "running", uptime: "5 days" },
        { name: "core", image: "cryptoengine-core", status: "running", uptime: "5 days" },
        { name: "db", image: "postgres:16-alpine", status: "healthy", uptime: "5 days" },
        { name: "frontend", image: "cryptoengine-frontend", status: "running", uptime: "5 days" },
        { name: "vtlive", image: "cryptoengine-core", status: "dry-run", uptime: "20 hours" },
      ],
    },
    errors: [],
    trader: {
      status: "running",
      ticks: 42,
      orders: 8,
      last_signal: "LINK buy",
      age_sec: 7,
    },
  };
}

function makeWeather() {
  const regimes = ["trend_up", "trend_down", "range", "high_vol", "reversal", "low_vol", "uncertain"];
  const symbols: Record<string, unknown> = {};
  SYMBOLS.forEach((symbol, i) => {
    const regime = regimes[i % regimes.length];
    const conf = round(0.4 + rnd() * 0.55, 2);
    symbols[symbol] = {
      regime,
      regime_conf: conf,
      raw: round(conf - 0.1 + rnd() * 0.2, 3),
      cal: conf,
      dir: regime === "trend_up" ? 1 : regime === "trend_down" ? -1 : 0,
      flat: regime === "range" || regime === "low_vol" ? "yes" : "no",
      mid: PRICE[symbol],
      approaching: symbol === "LINK" || symbol === "SOL",
    };
  });
  return {
    status: "running",
    age_sec: 3,
    count: SYMBOLS.length,
    approaching: ["LINK", "SOL"],
    symbols,
  };
}

function makeVtSignals() {
  const patterns = ["rci_turn", "pullback_to_ma", "bb_breakout_up", "lower_high_break", "three_crows", "double_bottom"];
  const regimes = ["trend_up", "trend_down", "range"];
  return Array.from({ length: 24 }, (_, i) => {
    const symbol = pick(["LINK", "LTC", "BCH", "BTC", "ETH"]);
    const type = i % 3 === 0 ? "sell" : "buy";
    return {
      ts: NOW - i * 53 * 60 * 1000,
      symbol,
      type,
      price: round(PRICE[symbol] * (1 + (rnd() - 0.5) * 0.006), 2),
      reason: type === "buy" ? "momentum confirmed" : "lower high confirmed",
      pattern: patterns[i % patterns.length],
      regime: regimes[i % regimes.length],
      confidence: round(0.5 + rnd() * 0.45, 2),
      rsi: round(30 + rnd() * 40, 1),
      percent_b: round(rnd(), 3),
      macd_hist: round((rnd() - 0.5) * 2, 4),
      qty: round(0.1 + rnd() * 1.5, 2),
      mid_price: PRICE[symbol],
      raw_score: round(0.5 + rnd() * 0.4, 2),
      bars_sec: 300,
    };
  });
}

function makeVtScore() {
  const stat = { pairs: 24, wins: 14, pnl: 128400, avg_hold_sec: 2400 };
  return {
    total: stat,
    by_symbol: {
      LINK: { pairs: 10, wins: 6, pnl: 64200, avg_hold_sec: 2200 },
      LTC: { pairs: 8, wins: 5, pnl: 48700, avg_hold_sec: 2600 },
      BCH: { pairs: 6, wins: 3, pnl: 15500, avg_hold_sec: 2500 },
    },
    by_pattern: {
      rci_turn: { pairs: 9, wins: 6, pnl: 52100, avg_hold_sec: 2000 },
      pullback_to_ma: { pairs: 7, wins: 4, pnl: 31800, avg_hold_sec: 2800 },
      bb_breakout_up: { pairs: 8, wins: 4, pnl: 44500, avg_hold_sec: 2300 },
    },
    by_regime: {
      trend_up: { pairs: 12, wins: 8, pnl: 81200, avg_hold_sec: 2100 },
      range: { pairs: 12, wins: 6, pnl: 47200, avg_hold_sec: 2700 },
    },
    by_exit_reason: {
      rsi_weak: { pairs: 9, wins: 5, pnl: 41200, avg_hold_sec: 1800 },
      trailing_stop: { pairs: 8, wins: 6, pnl: 63400, avg_hold_sec: 3100 },
      sl: { pairs: 7, wins: 3, pnl: 23800, avg_hold_sec: 2200 },
    },
    by_bars: {
      "300": { pairs: 24, wins: 14, pnl: 128400, avg_hold_sec: 2400 },
    },
    files: 3,
    rows: 24,
    unpaired_entries: 2,
    days: 7,
    src: "vt_signals",
  };
}

function makeOrderbook(symbol: string) {
  const mid = PRICE[symbol] ?? 10_500_000;
  const levels = Array.from({ length: 10 }, (_, i) => ({
    price: round(mid * (1 - (i + 1) * 0.00008), 2),
    volume: round(0.05 + rnd() * 1.2, 4),
  }));
  const asks = Array.from({ length: 10 }, (_, i) => ({
    price: round(mid * (1 + (i + 1) * 0.00008), 2),
    volume: round(0.05 + rnd() * 1.2, 4),
  }));
  return {
    symbol,
    mid_price: mid,
    spread: round(0.0001, 6),
    timestamp: NOW,
    bids: levels,
    asks,
  };
}

function makeFunding() {
  return {
    exchange: "binance",
    errors: [],
    rates: ["BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT", "LINKUSDT"].map((symbol, i) => ({
      exchange: "binance",
      symbol,
      funding_rate: round((rnd() - 0.5) * 0.0004, 6),
      funding_time: iso(NOW - 3600 * 1000),
      next_funding_time: iso(NOW + 7 * 3600 * 1000),
      mark_price: PRICE[symbol.replace("USDT", "")] ?? 100,
      apr_pct: round((rnd() - 0.3) * 40, 2),
    })),
  };
}

function makeEquityLive() {
  const balance = makeBalance();
  return {
    status: "ok",
    source: "gmo",
    stale: false,
    total_jpy: balance.total_assets,
    jpy_available: balance.available,
    jpy_balance: balance.available,
    holdings: [
      { symbol: "BTC", balance: 0.01, price_jpy: PRICE.BTC, value_jpy: 0.01 * PRICE.BTC },
      { symbol: "ETH", balance: 0.3, price_jpy: PRICE.ETH, value_jpy: 0.3 * PRICE.ETH },
    ],
    updated_at: iso(NOW),
    pnl_24h: 12800,
    history: balance.history,
  };
}

function makeBacktest() {
  return {
    total_signals: 40,
    buy_signals: 18,
    sell_signals: 14,
    hold_signals: 8,
    paired_trades: [],
    win_rate: 0.58,
    total_pnl: 128400,
    max_drawdown: 18200,
    max_win: 14200,
    max_loss: -6100,
    average_holding_seconds: 2400,
    close_reasons: { rsi_weak: 9, trailing_stop: 8, sl: 7 },
    confidence_stats: { average: 0.68, median: 0.7, min: 0.35, max: 0.96, count: 40 },
  };
}

export function demoResponse(path: string, method = "GET", body?: unknown): unknown {
  const [rawPath, query = ""] = path.split("?");
  const p = rawPath.replace(/\/+$/, "");
  const params = new URLSearchParams(query);
  const symbol = params.get("symbol") ?? "BTC";

  if (p === "/api/v1/auth/login" || p === "/api/v1/auth/refresh") {
    return { access_token: "demo-access-token", refresh_token: "demo-refresh-token", token_type: "bearer", expires_in: 3600 };
  }
  if (p === "/api/v1/auth/logout") return {};

  if (p === "/api/v1/balance") return makeBalance();
  if (p === "/api/v1/positions") {
    const positions = makePositions();
    return { positions, total_unrealized_pnl: round(positions.reduce((a, x) => a + x.unrealized_pnl, 0), 2) };
  }
  if (p === "/api/v1/signals") {
    const all = makeSignals();
    const filtered = params.get("symbol") ? all.filter((s) => s.symbol === params.get("symbol")) : all;
    return { signals: filtered };
  }
  if (p === "/api/v1/trades") {
    const all = makeTrades();
    const sym = params.get("symbol");
    const side = params.get("side");
    return { trades: all.filter((t) => (!sym || t.symbol === sym) && (!side || t.side === side)) };
  }
  if (p === "/api/v1/trade-pairs") return { trade_pairs: makeTradePairs() };
  if (p === "/api/v1/orderbook") {
    return { order_book: makeOrderbook(symbol), latency_ms: 4 };
  }
  if (p === "/api/v1/system") return makeSystem();
  if (p === "/api/v1/auto-trade") {
    const positions = makePositions();
    return {
      enabled: true,
      risk_stopped: false,
      risk: {
        max_position_size_usdt: 100,
        max_daily_loss_usdt: 20,
        trade_size_usdt: 50,
        stop_on_loss: true,
        daily_loss_usdt: -3.2,
      },
      positions,
      trade_log: makeTrades(10).map((t) => ({
        timestamp: t.timestamp,
        action: t.side,
        symbol: t.symbol,
        price: t.price,
        amount: t.volume,
        pnl: t.pnl,
        reason: "demo",
        order_id: t.id,
        status: "filled",
      })),
    };
  }
  if (p === "/api/v1/auto-trade/toggle") {
    return { enabled: !!((body as { enabled?: boolean } | undefined)?.enabled), risk_stopped: false };
  }
  if (p === "/api/v1/auto-trade/affordability") {
    return {
      status: "ok",
      jpy_available: 1_258_430,
      fee_bps: 5,
      symbols: SYMBOLS.map((s) => ({
        symbol: s,
        min_qty: 0.01,
        price: PRICE[s],
        min_jpy: round(PRICE[s] * 0.01, 0),
        affordable: true,
      })),
      affordable_symbols: SYMBOLS,
      message: "demo",
    };
  }
  if (p === "/api/v1/gmo/balance") {
    return {
      status: "ok",
      assets: [{ symbol: "JPY", available: "1258430", balance: "1258430" }],
      jpy_available: 1_258_430,
    };
  }
  if (p === "/api/v1/equity/live") return makeEquityLive();
  if (p === "/api/v1/market-weather") return makeWeather();
  if (p === "/api/v1/funding/rates") return makeFunding();
  if (p === "/api/v1/grid/evaluate") {
    return {
      ok: true,
      levels: [9_900_000, 10_000_000, 10_100_000, 10_200_000, 10_300_000],
      net_per_round: 4200,
      gross_per_round: 6000,
      fee_per_round: 1800,
      buy_levels: 8,
      sell_levels: 8,
      warnings: [],
    };
  }
  if (p === "/api/v1/vt/signals") return { signals: makeVtSignals(), count: 24 };
  if (p === "/api/v1/vt/score") return makeVtScore();
  if (p === "/api/v1/vt/status") {
    return { files: 3, total_bytes: 128000, latest_age_sec: 12, status: "running" };
  }
  if (p === "/api/v1/local-verify/capture") {
    return { status: "idle", minutes: null, elapsed_seconds: null, target_seconds: null, rows: null, error: null, out_file: "" };
  }
  if (p === "/api/v1/local-verify/sweep") {
    return { csv: "demo.csv", generated_at: NOW, rows: 0, runs: [] };
  }
  if (p === "/api/v1/local-verify/report") return { content: "# Demo report\n\nStatic demo data." };
  if (p === "/api/v1/backtest") return makeBacktest();
  return {};
}

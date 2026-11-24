"use client";

import { useEffect, useRef, useState } from "react";
import {
  FileText,
  FlaskConical,
  Loader2,
  Radio,
  Search,
  Upload,
} from "lucide-react";
import PageHeader from "@/components/PageHeader";
import Card from "@/components/Card";
import Table, { type Column } from "@/components/Table";
import {
  api,
  type BacktestResult,
  type CaptureStatus,
  type PairedTrade,
  type SweepRun,
  type SweepResults,
} from "@/lib/api";
import { fmt, fmtDuration, fmtPLColor, fmtUSDT, fmtYen } from "@/lib/format";
import { cn } from "@/lib/cn";

function StatCard({
  label,
  value,
  sub,
  className,
}: {
  label: string;
  value: string;
  sub?: string;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "rounded-card border border-white/5 bg-navy-850/70 p-4 shadow-card",
        className
      )}
    >
      <p className="text-[11px] uppercase tracking-wider text-slate-500">{label}</p>
      <p className="mt-1 text-2xl font-bold text-slate-100">{value}</p>
      {sub && <p className="mt-0.5 text-xs text-slate-500">{sub}</p>}
    </div>
  );
}

function fmtEpoch(ms: number): string {
  const d = new Date(ms);
  if (Number.isNaN(d.getTime())) return "-";
  return d.toLocaleString("ja-JP", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

export default function BacktestPage() {
  const inputRef = useRef<HTMLInputElement>(null);
  const [file, setFile] = useState<File | null>(null);
  const [running, setRunning] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<BacktestResult | null>(null);

  // ローカル検証（短期キャプチャ → グリッド探索 → レポート）
  const [minutes, setMinutes] = useState(30);
  const [capture, setCapture] = useState<CaptureStatus | null>(null);
  const [sweep, setSweep] = useState<SweepResults | null>(null);
  const [sweepRunning, setSweepRunning] = useState(false);
  const [report, setReport] = useState<string | null>(null);
  const [reportRunning, setReportRunning] = useState(false);
  const [lvError, setLvError] = useState<string | null>(null);

  useEffect(() => {
    api.captureStatus().then(setCapture).catch(() => {});
    api.sweepResults().then(setSweep).catch(() => {});
  }, []);

  // キャプチャ中は 2 秒間隔で状態をポーリングする。
  useEffect(() => {
    if (capture?.status !== "capturing") return;
    const timer = setInterval(async () => {
      try {
        setCapture(await api.captureStatus());
      } catch {
        // 一時的な取得失敗は無視（次のポーリングで復帰）
      }
    }, 2000);
    return () => clearInterval(timer);
  }, [capture?.status]);

  const startCapture = async () => {
    setLvError(null);
    try {
      await api.startCapture(minutes);
      setCapture(await api.captureStatus());
    } catch (e) {
      setLvError(e instanceof Error ? e.message : String(e));
    }
  };

  const runSweep = async () => {
    setLvError(null);
    setSweepRunning(true);
    try {
      setSweep(await api.runSweep());
    } catch (e) {
      setLvError(e instanceof Error ? e.message : String(e));
    } finally {
      setSweepRunning(false);
    }
  };

  const runReport = async () => {
    setLvError(null);
    setReportRunning(true);
    try {
      await api.runReport();
      const r = await api.report();
      setReport(r.content);
    } catch (e) {
      setLvError(e instanceof Error ? e.message : String(e));
    } finally {
      setReportRunning(false);
    }
  };

  const captureDone =
    capture?.status === "done" || capture?.status === "error";

  const captureProgress =
    capture?.target_seconds && capture?.elapsed_seconds != null
      ? Math.min(100, (capture.elapsed_seconds / capture.target_seconds) * 100)
      : 0;

  const bestPnl =
    sweep && sweep.runs.length > 0
      ? Math.max(...sweep.runs.map((r) => r.result.total_pnl))
      : undefined;

  const run = async () => {
    if (!file) return;
    setRunning(true);
    setError(null);
    setResult(null);
    try {
      setResult(await api.backtest(file));
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setRunning(false);
    }
  };

  const pairColumns: Column<PairedTrade>[] = [
    {
      key: "entry_time",
      label: "Entry（エントリー）",
      sortValue: (p) => p.entry_time,
      render: (p) => <span className="text-slate-400">{fmtEpoch(p.entry_time)}</span>,
    },
    {
      key: "exit_time",
      label: "Exit（クローズ）",
      sortValue: (p) => p.exit_time,
      render: (p) => <span className="text-slate-400">{fmtEpoch(p.exit_time)}</span>,
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
      render: (p) => (
        <span className={cn("font-medium", fmtPLColor(p.pnl))}>
          {p.pnl > 0 ? "+" : ""}
          {fmtYen(p.pnl, 0)}
        </span>
      ),
    },
    {
      key: "duration_seconds",
      label: "Duration（保有時間）",
      sortValue: (p) => p.duration_seconds,
      align: "right",
      render: (p) => <span>{fmtDuration(p.duration_seconds)}</span>,
    },
    {
      key: "close_reason",
      label: "Close（クローズ要因）",
      sortValue: (p) => p.close_reason ?? "",
      render: (p) => {
        const map: Record<string, { label: string; cls: string }> = {
          signal: { label: "シグナル", cls: "text-sky-300" },
          take_profit: { label: "利確(TP)", cls: "text-emerald-300" },
          stop_loss: { label: "損切(SL)", cls: "text-red-300" },
          max_hold: { label: "時間切れ", cls: "text-amber-300" },
        };
        const m = map[p.close_reason ?? ""];
        return m ? (
          <span className={cn("rounded bg-white/5 px-1.5 py-0.5 text-xs", m.cls)}>
            {m.label}
          </span>
        ) : (
          <span className="text-slate-500">-</span>
        );
      },
    },
  ];

  const sweepColumns: Column<SweepRun>[] = [
    {
      key: "label",
      label: "パラメータ",
      sortValue: (r) => r.params.label,
      render: (r) => (
        <span className="inline-flex items-center gap-2">
          {r.result.total_pnl === bestPnl && (
            <span className="rounded bg-emerald-500/15 px-1.5 py-0.5 text-[10px] font-bold text-emerald-300">
              BEST
            </span>
          )}
          <span>{r.params.label}</span>
        </span>
      ),
    },
    {
      key: "imbalance",
      label: "imb",
      align: "right",
      sortValue: (r) => r.params.imbalance_threshold,
      render: (r) => <span>{r.params.imbalance_threshold.toFixed(1)}</span>,
    },
    {
      key: "trend",
      label: "trend",
      align: "right",
      sortValue: (r) => r.params.trend_window,
      render: (r) => <span>{r.params.trend_window}</span>,
    },
    {
      key: "minhold",
      label: "minhold",
      align: "right",
      sortValue: (r) => r.params.min_hold_seconds,
      render: (r) => <span>{r.params.min_hold_seconds}</span>,
    },
    {
      key: "tpsl",
      label: "TP/SL",
      align: "right",
      sortValue: (r) => r.params.take_profit_usdt,
      render: (r) => <span>{r.params.take_profit_usdt.toFixed(2)}</span>,
    },
    {
      key: "entry",
      label: "entry",
      sortValue: (r) => r.params.entry_mode,
      render: (r) => <span>{r.params.entry_mode}</span>,
    },
    {
      key: "win_rate",
      label: "勝率",
      align: "right",
      sortValue: (r) => r.result.win_rate,
      render: (r) => <span>{fmt(r.result.win_rate * 100, 1)}%</span>,
    },
    {
      key: "total_pnl",
      label: "総損益",
      align: "right",
      sortValue: (r) => r.result.total_pnl,
      render: (r) => (
        <span className={cn("font-medium", fmtPLColor(r.result.total_pnl))}>
          {r.result.total_pnl > 0 ? "+" : ""}
          {fmtYen(r.result.total_pnl, 0)}
        </span>
      ),
    },
    {
      key: "max_drawdown",
      label: "MaxDD",
      align: "right",
      sortValue: (r) => r.result.max_drawdown,
      render: (r) => <span>{fmtYen(r.result.max_drawdown, 0)}</span>,
    },
    {
      key: "avg_hold",
      label: "平均保有",
      align: "right",
      sortValue: (r) => r.result.average_holding_seconds,
      render: (r) => <span>{fmtDuration(r.result.average_holding_seconds)}</span>,
    },
    {
      key: "pairs",
      label: "ペア",
      align: "right",
      sortValue: (r) => r.result.paired_trades.length,
      render: (r) => <span>{r.result.paired_trades.length}</span>,
    },
  ];

  return (
    <>
      <PageHeader
        title="Backtest"
        subtitle="過去の板情報 CSV をアップロードし、現在の判定ロジックで再検証する"
      />

      <Card title="CSV アップロード" icon={Upload}>
        <div className="flex flex-col gap-4">
          <input
            ref={inputRef}
            type="file"
            accept=".csv,text/csv"
            className="hidden"
            onChange={(e) => {
              setFile(e.target.files?.[0] ?? null);
              setResult(null);
              setError(null);
            }}
          />
          <button
            type="button"
            onClick={() => inputRef.current?.click()}
            className="w-full rounded-card border-2 border-dashed border-white/10 bg-navy-900/40 px-4 py-6 text-left transition-colors hover:border-emerald-500/40"
          >
            {file ? (
              <p className="text-sm text-emerald-300">
                {file.name}
                <span className="ml-2 text-xs text-slate-500">
                  （{fmt(file.size / 1024, 1)} KB / クリックで変更）
                </span>
              </p>
            ) : (
              <p className="text-sm text-slate-400">
                クリックして CSV を選択（timestamp, midPrice, spread, bids(json),
                asks(json)）
              </p>
            )}
          </button>
          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={run}
              disabled={!file || running}
              className="inline-flex items-center gap-2 rounded-lg bg-emerald-500 px-4 py-2 text-sm font-semibold text-navy-950 transition-colors hover:bg-emerald-400 disabled:cursor-not-allowed disabled:opacity-40"
            >
              {running && <Loader2 className="h-4 w-4 animate-spin" />}
              {running ? "実行中..." : "バックテスト実行"}
            </button>
            {file && (
              <span className="text-xs text-slate-500">
                判定ロジックはリアルタイムと同一（volume 0.001 BTC / JPY 150 換算）
              </span>
            )}
          </div>
          {error && (
            <p className="rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 text-sm text-red-400">
              {error}
            </p>
          )}
        </div>
      </Card>

      <Card
        title="ローカル検証（短期キャプチャ → グリッド探索 → レポート）"
        icon={Radio}
      >
        <div className="flex flex-col gap-4">
          <div className="flex flex-wrap items-end gap-3">
            <label className="flex flex-col gap-1 text-xs text-slate-400">
              キャプチャ時間（分）
              <input
                type="number"
                min={10}
                max={60}
                step={5}
                value={minutes}
                disabled={capture?.status === "capturing"}
                onChange={(e) => setMinutes(Number(e.target.value))}
                className="w-28 rounded-lg border border-white/10 bg-navy-900/60 px-3 py-2 text-sm text-slate-100"
              />
            </label>
            <button
              type="button"
              onClick={startCapture}
              disabled={capture?.status === "capturing"}
              className="inline-flex items-center gap-2 rounded-lg bg-sky-500 px-4 py-2 text-sm font-semibold text-navy-950 transition-colors hover:bg-sky-400 disabled:cursor-not-allowed disabled:opacity-40"
            >
              {capture?.status === "capturing" && (
                <Loader2 className="h-4 w-4 animate-spin" />
              )}
              {capture?.status === "capturing" ? "キャプチャ中..." : "キャプチャ開始"}
            </button>
            <button
              type="button"
              onClick={runSweep}
              disabled={!captureDone || sweepRunning}
              className="inline-flex items-center gap-2 rounded-lg bg-emerald-500 px-4 py-2 text-sm font-semibold text-navy-950 transition-colors hover:bg-emerald-400 disabled:cursor-not-allowed disabled:opacity-40"
            >
              {sweepRunning ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <Search className="h-4 w-4" />
              )}
              {sweepRunning ? "探索中..." : "パラメータ探索実行"}
            </button>
            <button
              type="button"
              onClick={runReport}
              disabled={!sweep || reportRunning}
              className="inline-flex items-center gap-2 rounded-lg bg-navy-700 px-4 py-2 text-sm font-semibold text-slate-100 transition-colors hover:bg-navy-600 disabled:cursor-not-allowed disabled:opacity-40"
            >
              {reportRunning ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <FileText className="h-4 w-4" />
              )}
              {reportRunning ? "生成中..." : "統計レポート生成"}
            </button>
          </div>

          {capture && (
            <div className="rounded-lg border border-white/5 bg-navy-850/60 p-3 text-sm">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <span className="text-slate-300">
                  状態:{" "}
                  <b
                    className={cn(
                      capture.status === "capturing" && "text-sky-300",
                      capture.status === "done" && "text-emerald-300",
                      capture.status === "error" && "text-red-400"
                    )}
                  >
                    {capture.status}
                  </b>
                </span>
                {capture.status === "capturing" && capture.target_seconds && (
                  <span className="text-xs text-slate-400">
                    経過 {fmt((capture.elapsed_seconds ?? 0) / 60, 1)} 分 /{" "}
                    {fmt(capture.target_seconds / 60, 1)} 分
                  </span>
                )}
                {capture.status === "done" && capture.rows != null && (
                  <span className="text-xs text-emerald-300">
                    {fmt(capture.rows)} 行キャプチャ済み
                  </span>
                )}
              </div>
              {capture.status === "capturing" && (
                <div className="mt-2 h-2 w-full overflow-hidden rounded-full bg-white/5">
                  <div
                    className="h-full bg-sky-500 transition-all"
                    style={{ width: `${captureProgress}%` }}
                  />
                </div>
              )}
              {capture.status === "error" && capture.error && (
                <p className="mt-2 text-xs text-red-400">{capture.error}</p>
              )}
            </div>
          )}

          {lvError && (
            <p className="rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 text-sm text-red-400">
              {lvError}
            </p>
          )}

          {sweep && (
            <>
              <div className="flex flex-wrap items-center gap-2 text-xs text-slate-400">
                <span>
                  {sweep.runs.length} セット / キャプチャ {sweep.rows ?? "-"} 行
                  <span className="mx-1">/</span>
                  CSV: <code className="text-slate-500">{sweep.csv}</code>
                </span>
              </div>
              <Table
                columns={sweepColumns}
                rows={sweep.runs}
                rowKey={(r, i) => `${r.params.label}-${i}`}
                defaultSort={{ key: "total_pnl", dir: "desc" }}
                emptyMessage="探索結果がありません"
              />
            </>
          )}

          {report && (
            <Card title="統計レポート" icon={FileText}>
              <pre className="max-h-[560px] overflow-auto whitespace-pre-wrap rounded-lg bg-navy-900/60 p-4 text-xs leading-relaxed text-slate-300">
                {report}
              </pre>
            </Card>
          )}
        </div>
      </Card>

      {result && (
        <>
          <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
            <StatCard
              label="Total Signals（シグナル）"
              value={fmt(result.total_signals)}
            />
            <StatCard
              label="Buy / Sell / Hold"
              value={`${result.buy_signals} / ${result.sell_signals} / ${result.hold_signals}`}
              sub="買い / 売り / 保留"
            />
            <StatCard
              label="Paired Trades（成立ペア）"
              value={fmt(result.paired_trades.length)}
            />
            <StatCard
              label="Win Rate（勝率）"
              value={`${fmt(result.win_rate * 100, 1)}%`}
              sub={`${result.paired_trades.filter((p) => p.pnl > 0).length} 勝`}
            />
            <StatCard
              label="Total P/L（総損益）"
              value={fmtYen(result.total_pnl, 0)}
              className={cn(
                result.total_pnl !== 0 && "ring-1",
                result.total_pnl > 0 && "ring-emerald-500/30",
                result.total_pnl < 0 && "ring-red-500/30"
              )}
            />
            <StatCard
              label="Max Drawdown（最大下落）"
              value={fmtYen(result.max_drawdown, 0)}
            />
            <StatCard
              label="Max Win / Max Loss"
              value={`${fmtYen(result.max_win ?? 0, 0)} / ${fmtYen(result.max_loss ?? 0, 0)}`}
              sub="最大勝ち / 最大負け"
            />
            <StatCard
              label="Avg Holding（平均保有）"
              value={fmtDuration(result.average_holding_seconds)}
            />
            <StatCard
              label="Confidence（平均）"
              value={fmt(result.confidence_stats.average * 100, 1) + "%"}
              sub={`中央値 ${fmt(
                result.confidence_stats.median * 100,
                1
              )}% / n=${result.confidence_stats.count}`}
            />
          </div>

          {result.close_reasons &&
            Object.keys(result.close_reasons).length > 0 && (
              <Card title="Close Reasons（クローズ要因の内訳）">
                <div className="flex flex-wrap gap-2">
                  {Object.entries(result.close_reasons).map(([k, v]) => (
                    <span
                      key={k}
                      className="inline-flex items-center gap-2 rounded-lg border border-white/5 bg-navy-850/70 px-3 py-1.5 text-sm"
                    >
                      <span className="text-slate-400">{k}</span>
                      <span className="font-bold text-slate-100">{v}</span>
                    </span>
                  ))}
                </div>
              </Card>
            )}

          <Card
            title="Paired Trades（バックテスト内のペア）"
            icon={FlaskConical}
            action={
              <span className="text-xs text-slate-500">
                {result.paired_trades.length} 件
              </span>
            }
          >
            {result.paired_trades.length === 0 ? (
              <p className="py-8 text-center text-sm text-slate-500">
                BUY→SELL のペアは成立しませんでした
              </p>
            ) : (
              <Table
                columns={pairColumns}
                rows={result.paired_trades}
                rowKey={(p) => `${p.entry_time}-${p.exit_time}`}
                defaultSort={{ key: "exit_time", dir: "desc" }}
                emptyMessage="成立したペアはありません"
              />
            )}
          </Card>
        </>
      )}
    </>
  );
}

"use client";

import { useState } from "react";
import { Grid3x3 } from "lucide-react";
import PageHeader from "@/components/PageHeader";
import Card from "@/components/Card";
import { api } from "@/lib/api";

export default function GridPage() {
  const [lower, setLower] = useState("9000000");
  const [upper, setUpper] = useState("10000000");
  const [grids, setGrids] = useState("10");
  const [price, setPrice] = useState("9500000");
  const [result, setResult] = useState<{
    ok: boolean;
    net_per_round?: number;
    gross_per_round?: number;
    fee_per_round?: number;
    buy_levels?: number;
    sell_levels?: number;
    warnings?: string[];
    message?: string;
  } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const run = async () => {
    setLoading(true);
    setError(null);
    try {
      const out = await api.gridEvaluate({
        lower: Number(lower),
        upper: Number(upper),
        grids: Number(grids),
        current_price: Number(price),
        fee_bps: 5,
        mode: "arithmetic",
      });
      setResult(out);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  };

  const num = (id: string, v: string, set: (s: string) => void, label: string) => (
    <label className="flex flex-col gap-1 text-xs text-slate-400">
      {label}
      <input
        id={id}
        value={v}
        onChange={(e) => set(e.target.value)}
        inputMode="decimal"
        className="rounded-card border border-white/10 bg-white/[0.03] px-3 py-2 font-mono text-sm text-slate-100"
      />
    </label>
  );

  return (
    <>
      <PageHeader
        title="Grid"
        subtitle="グリッド評価プラグイン（計算のみ・発注なし・既存ループ分離）"
      />
      <div className="mt-4 grid grid-cols-2 gap-3 sm:grid-cols-4">
        {num("grid-lower", lower, setLower, "Lower（下限）")}
        {num("grid-upper", upper, setUpper, "Upper（上限）")}
        {num("grid-n", grids, setGrids, "Grids（分割数）")}
        {num("grid-price", price, setPrice, "Current（現在値）")}
      </div>
      <button
        type="button"
        onClick={run}
        disabled={loading}
        className="mt-3 rounded-card bg-emerald-500/20 px-4 py-2 text-sm font-semibold text-emerald-300 ring-1 ring-emerald-500/30 disabled:opacity-50"
      >
        {loading ? "計算中..." : "評価する"}
      </button>
      <div className="mt-4">
        <Card title="評価結果" icon={Grid3x3}>
          {error ? (
            <p className="py-4 text-sm text-red-400">{error}</p>
          ) : !result ? (
            <p className="py-4 text-sm text-slate-500">条件を入れて評価してください</p>
          ) : !result.ok ? (
            <p className="py-4 text-sm text-red-400">{result.message}</p>
          ) : (
            <div className="text-sm text-slate-300">
              <p>1往復概算（手数料後）: <span className="font-mono">{result.net_per_round}</span></p>
              <p className="text-slate-500">gross {result.gross_per_round} / fee {result.fee_per_round}</p>
              <p className="text-slate-500">buy {result.buy_levels} / sell {result.sell_levels}</p>
              {(result.warnings ?? []).length > 0 && (
                <ul className="mt-2 list-disc pl-5 text-amber-300">
                  {(result.warnings ?? []).map((w) => (
                    <li key={w}>{w}</li>
                  ))}
                </ul>
              )}
            </div>
          )}
        </Card>
        <p className="mt-2 text-xs text-slate-500">
          概算のみ。レンジ外ブレイク・板薄・手数料変動で実績は変わります。自動発注はしません。
        </p>
      </div>
    </>
  );
}

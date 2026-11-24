"use client";

import { CloudSun } from "lucide-react";
import Card from "@/components/Card";
import Badge from "@/components/Badge";
import { marketWeatherApi, usePoll, type WeatherSymbol } from "@/lib/api";
import { cn } from "@/lib/cn";

const REGIME_META: Record<string, { icon: string; label: string; tone: "emerald" | "red" | "amber" | "slate" | "sky" }> = {
  trend_up: { icon: "☀", label: "上昇", tone: "emerald" },
  trend_down: { icon: "🌧", label: "下降", tone: "sky" },
  range: { icon: "☁", label: "横ばい", tone: "slate" },
  high_vol: { icon: "⚡", label: "荒れ", tone: "amber" },
  reversal: { icon: "🌊", label: "反転", tone: "amber" },
  low_vol: { icon: "🌫", label: "凪", tone: "slate" },
  uncertain: { icon: "❔", label: "不明", tone: "slate" },
};

function meta(regime: string) {
  if (!regime) return REGIME_META["uncertain"];
  return REGIME_META[regime] ?? REGIME_META["uncertain"];
}

const LEGEND: { icon: string; label: string; desc: string }[] = [
  { icon: "☀", label: "上昇", desc: "上げの流れが統計的に有意" },
  { icon: "🌧", label: "下降", desc: "下げの流れが統計的に有意" },
  { icon: "☁", label: "横ばい", desc: "方向なし・条件次第で参加" },
  { icon: "⚡", label: "荒れ", desc: "変動大・短期決戦のみ" },
  { icon: "🌊", label: "反転", desc: "流れの転換点・慎重に参加" },
  { icon: "🌫", label: "凪", desc: "動き不足でお休み" },
  { icon: "❔", label: "不明", desc: "材料不足でお休み" },
];

function WeatherChip({ symbol, w }: { symbol: string; w: WeatherSymbol }) {
  const m = meta(w.regime);
  const score = (w.cal || 0) > 0 ? w.cal : w.regime_conf || 0;
  const pct = Math.round(score * 100);
  return (
    <div
      className={cn(
        "rounded-lg border px-2.5 py-2",
        w.approaching
          ? "border-amber-500/50 bg-amber-500/10"
          : "border-white/5 bg-white/[0.03]"
      )}
    >
      <div className="flex items-center justify-between gap-1">
        <span className="font-mono text-xs font-semibold text-slate-200">{symbol}</span>
        <span title={w.regime} className="text-sm leading-none">
          {m.icon}
        </span>
      </div>
      <div className="mt-1 flex items-center gap-1.5">
        <div className="h-1 flex-1 overflow-hidden rounded-full bg-white/10">
          <div
            className={cn(
              "h-full rounded-full",
              w.approaching ? "bg-amber-400" : "bg-emerald-500/70"
            )}
            style={{ width: `${Math.min(pct, 100)}%` }}
          />
        </div>
        <span className="font-mono text-[10px] text-slate-400">{pct}%</span>
      </div>
      <div className="mt-1 text-[10px] text-slate-500">
        {m.label}
        {w.approaching && <span className="ml-1 font-bold text-amber-300">接近</span>}
      </div>
    </div>
  );
}

/**
 * 相場天気予報（v2評価の内訳を可視化）。
 * 監視が生きている証拠＋引き金接近の予告。操作は不要。
 */
export default function MarketWeather() {
  const { data } = usePoll(marketWeatherApi.weather, 10000);
  const entries = Object.entries(data?.symbols ?? {}).sort(([a], [b]) =>
    a.localeCompare(b)
  );
  const approaching = data?.approaching ?? [];
  const live = data?.status === "running";

  return (
    <Card
      title="相場天気予報（全銘柄・5秒更新）"
      icon={CloudSun}
      action={
        <span className="flex items-center gap-2 text-xs text-slate-500">
          {live ? (
            <Badge tone="emerald">監視中</Badge>
          ) : (
            <Badge tone="slate">更新待ち</Badge>
          )}
        </span>
      }
    >
      {entries.length === 0 ? (
        <p className="py-6 text-center text-sm text-slate-500">
          天気データを取得中です（trader起動後まもなく表示されます）
        </p>
      ) : (
        <>
          {approaching.length > 0 && (
            <p className="mb-3 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm text-amber-200">
              チャンス接近中: <b>{approaching.join("、")}</b>
              <span className="text-amber-200/70"> — 方向は出ています。条件が揃えば発注します</span>
            </p>
          )}
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6">
            {entries.map(([symbol, w]) => (
              <WeatherChip key={symbol} symbol={symbol} w={w} />
            ))}
          </div>
          <div className="mt-3 flex flex-wrap gap-x-4 gap-y-1 border-t border-white/5 pt-2 text-[11px] text-slate-500">
            {LEGEND.map((l) => (
              <span key={l.label} className="inline-flex items-center gap-1">
                <span>{l.icon}</span>
                <b className="font-medium text-slate-400">{l.label}</b>
                <span>{l.desc}</span>
              </span>
            ))}
          </div>
          <p className="mt-2 text-xs text-slate-500">
            バーは現在の確信度(発火に近いほど長い)です。凪・不明は設計通りお休みです。
          </p>
        </>
      )}
    </Card>
  );
}

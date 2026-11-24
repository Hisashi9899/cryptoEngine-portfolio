"use client";

import { CloudSun } from "lucide-react";
import Card from "@/components/Card";
import Badge from "@/components/Badge";
import { marketWeatherApi, usePoll, type WeatherSymbol } from "@/lib/api";
import { cn } from "@/lib/cn";
import { useI18n } from "@/lib/i18n";

const REGIME_META: Record<string, { icon: string; label: string; labelEn: string; tone: "emerald" | "red" | "amber" | "slate" | "sky" }> = {
  trend_up: { icon: "☀", label: "上昇", labelEn: "Uptrend", tone: "emerald" },
  trend_down: { icon: "🌧", label: "下降", labelEn: "Downtrend", tone: "sky" },
  range: { icon: "☁", label: "横ばい", labelEn: "Range", tone: "slate" },
  high_vol: { icon: "⚡", label: "荒れ", labelEn: "High vol", tone: "amber" },
  reversal: { icon: "🌊", label: "反転", labelEn: "Reversal", tone: "amber" },
  low_vol: { icon: "🌫", label: "凪", labelEn: "Low vol", tone: "slate" },
  uncertain: { icon: "❔", label: "不明", labelEn: "Unknown", tone: "slate" },
};

function meta(regime: string) {
  if (!regime) return REGIME_META["uncertain"];
  return REGIME_META[regime] ?? REGIME_META["uncertain"];
}

const LEGEND: { icon: string; label: string; labelEn: string; desc: string; descEn: string }[] = [
  { icon: "☀", label: "上昇", labelEn: "Uptrend", desc: "上げの流れが統計的に有意", descEn: "Statistically significant upward flow" },
  { icon: "🌧", label: "下降", labelEn: "Downtrend", desc: "下げの流れが統計的に有意", descEn: "Statistically significant downward flow" },
  { icon: "☁", label: "横ばい", labelEn: "Range", desc: "方向なし・条件次第で参加", descEn: "No direction; participate only on setup" },
  { icon: "⚡", label: "荒れ", labelEn: "High vol", desc: "変動大・短期決戦のみ", descEn: "Large moves; short-term only" },
  { icon: "🌊", label: "反転", labelEn: "Reversal", desc: "流れの転換点・慎重に参加", descEn: "Potential turning point; be cautious" },
  { icon: "🌫", label: "凪", labelEn: "Low vol", desc: "動き不足でお休み", descEn: "Too quiet; stand aside" },
  { icon: "❔", label: "不明", labelEn: "Unknown", desc: "材料不足でお休み", descEn: "Insufficient data; stand aside" },
];

function WeatherChip({ symbol, w }: { symbol: string; w: WeatherSymbol }) {
  const { tr } = useI18n();
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
        {tr(m.label, m.labelEn)}
        {w.approaching && <span className="ml-1 font-bold text-amber-300">{tr("接近", "Approaching")}</span>}
      </div>
    </div>
  );
}

/**
 * 相場天気予報（v2評価の内訳を可視化）。
 * 監視が生きている証拠＋引き金接近の予告。操作は不要。
 */
export default function MarketWeather() {
  const { tr } = useI18n();
  const { data } = usePoll(marketWeatherApi.weather, 10000);
  const entries = Object.entries(data?.symbols ?? {}).sort(([a], [b]) =>
    a.localeCompare(b)
  );
  const approaching = data?.approaching ?? [];
  const live = data?.status === "running";

  return (
    <Card
      title={tr("相場天気予報（全銘柄・5秒更新）", "Market Weather (all symbols, 5s)")}
      icon={CloudSun}
      action={
        <span className="flex items-center gap-2 text-xs text-slate-500">
          {live ? (
            <Badge tone="emerald">{tr("監視中", "LIVE")}</Badge>
          ) : (
            <Badge tone="slate">{tr("更新待ち", "WAITING")}</Badge>
          )}
        </span>
      }
    >
      {entries.length === 0 ? (
        <p className="py-6 text-center text-sm text-slate-500">
          {tr(
            "天気データを取得中です（trader起動後まもなく表示されます）",
            "Loading market weather (it appears shortly after the trader starts)"
          )}
        </p>
      ) : (
        <>
          {approaching.length > 0 && (
            <p className="mb-3 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm text-amber-200">
              {tr("チャンス接近中", "Approaching setup")}: <b>{approaching.join(", ")}</b>
              <span className="text-amber-200/70">
                {" "}— {tr("方向は出ています。条件が揃えば発注します", "direction is present; orders trigger when conditions align")}
              </span>
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
                <b className="font-medium text-slate-400">{tr(l.label, l.labelEn)}</b>
                <span>{tr(l.desc, l.descEn)}</span>
              </span>
            ))}
          </div>
          <p className="mt-2 text-xs text-slate-500">
            {tr(
              "バーは現在の確信度(発火に近いほど長い)です。凪・不明は設計通りお休みです。",
              "Bars show current confidence (longer means closer to a trigger). Low-vol and unknown states intentionally stand aside."
            )}
          </p>
        </>
      )}
    </Card>
  );
}

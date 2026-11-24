"use client";

import { cn } from "@/lib/cn";

export type EquitySource = "live" | "paper";

/** LIVE（GMO実資産）/ PAPER（paper計算）の切替。 */
export default function EquitySourceToggle({
  source,
  onChange,
  liveOk,
}: {
  source: EquitySource;
  onChange: (s: EquitySource) => void;
  liveOk: boolean;
}) {
  const btn = (active: boolean) =>
    cn(
      "rounded-md px-2.5 py-1 text-xs font-medium transition-colors",
      active
        ? "bg-emerald-500/20 text-emerald-300"
        : "text-slate-500 hover:text-slate-300"
    );
  return (
    <div className="flex items-center gap-1 rounded-lg bg-white/[0.04] p-1">
      <button
        type="button"
        className={btn(source === "live")}
        onClick={() => onChange("live")}
        title={liveOk ? "GMO実資産を表示" : "GMO未接続のため表示できません"}
      >
        LIVE
      </button>
      <button
        type="button"
        className={btn(source === "paper")}
        onClick={() => onChange("paper")}
        title="paper計算（初期資金100万円基準）を表示"
      >
        PAPER
      </button>
      {!liveOk && (
        <span className="px-1 text-[11px] text-slate-600" title="GMO未接続">
          GMO未接続
        </span>
      )}
    </div>
  );
}

"use client";

import { AlertTriangle, Wallet } from "lucide-react";
import Card from "@/components/Card";
import Badge from "@/components/Badge";
import { api, usePoll } from "@/lib/api";
import { fmtYen } from "@/lib/format";
import { useI18n } from "@/lib/i18n";

export default function GmoAffordabilityAlert() {
  const { tr } = useI18n();
  const { data: gmo } = usePoll(api.gmoBalance, 30000);
  const { data: funds } = usePoll(api.affordability, 30000);

  const gmoOk = gmo?.status === "ok";
  const fundsOk = funds?.status === "ok";
  const jpy = gmo?.jpy_available ?? funds?.jpy_available ?? null;
  const affordable = funds?.affordable_symbols ?? [];
  const unaffordable = (funds?.symbols ?? [])
    .filter((s) => s.price > 0 && !s.affordable)
    .sort((a, b) => a.min_jpy - b.min_jpy)
    .slice(0, 5);
  const holdings = (gmo?.assets ?? [])
    .map((a) => ({
      symbol: String(a.symbol ?? ""),
      available: Number(a.available ?? 0) || 0,
    }))
    .filter((a) => a.symbol && a.available > 0)
    .sort((a, b) => b.available - a.available)
    .slice(0, 10);

  if (!gmoOk && !fundsOk) {
    return (
      <Card title={tr("GMO実残高・取引可否", "GMO Balance & Tradability")} icon={Wallet}>
        <p className="text-sm text-slate-400">
          {tr(
            "GMO_API_KEY未設定のため実残高を取得できません。VPS上で bash ops/deploy/setup-gmo.sh を実行して読み取り専用キーを設定してください。設定後はJPY余力と銘柄別必要額がここに表示されます。",
            "GMO_API_KEY is not configured, so the real balance is unavailable. Run bash ops/deploy/setup-gmo.sh on the VPS to configure a read-only key. The JPY balance and per-symbol minimums will then appear here."
          )}
        </p>
        <p className="mt-2 text-xs text-slate-500">
          {tr(
            "現在のBalance表示はpaper計算（初期100万円＋確定損益）であり、GMO口座の残高とは別物です。",
            "The Balance view is a paper calculation (initial ¥1,000,000 plus realized P&L), separate from the GMO account balance."
          )}
        </p>
      </Card>
    );
  }

  return (
    <Card
      title={tr("GMO実残高・取引可否", "GMO Balance & Tradability")}
      icon={Wallet}
      action={
        <span className="text-xs text-slate-500">
          {jpy != null
            ? `${tr("JPY余力", "JPY available")}: ${fmtYen(jpy)}`
            : tr("取得中", "Loading")}
        </span>
      }
    >
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <span className="text-slate-300">
          {tr("GMO実残高", "GMO balance")}:{" "}
          <span className="font-mono font-semibold">{jpy != null ? fmtYen(jpy) : "-"}</span>
        </span>
        {affordable.length > 0 ? (
          <Badge tone="emerald">{tr("取引可", "Tradable")}: {affordable.join(", ")}</Badge>
        ) : (
          <Badge tone="red">{tr("取引可なし（入金が必要）", "No tradable symbols (deposit required)")}</Badge>
        )}
      </div>
      {unaffordable.length > 0 && (
        <p className="mt-2 flex items-start gap-2 text-xs text-amber-300">
          <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
          <span>
            {tr("資金不足例", "Insufficient examples")}:{" "}
            {unaffordable.map((s) => `${s.symbol} (${fmtYen(s.min_jpy)})`).join(", ")}.{" "}
            {tr("JPY余力", "JPY available")} {jpy != null ? fmtYen(jpy) : "-"} —{" "}
            {tr("では発注見送りになります。", "orders will be skipped.")}
          </span>
        </p>
      )}
      {holdings.length > 0 && (
        <div className="mt-3 flex flex-wrap gap-2 text-xs">
          {holdings.map((h) => (
            <span key={h.symbol} className="rounded bg-white/5 px-2 py-1 font-mono text-slate-300">
              {h.symbol}: {h.available}
            </span>
          ))}
        </div>
      )}
      <p className="mt-2 text-xs text-slate-500">
        {tr(
          "paper残高とは別にGMO実残高を表示しています。発注前にもcore側で資金ガードします。",
          "The GMO balance is shown separately from the paper balance. The core also applies a pre-order balance guard."
        )}
      </p>
    </Card>
  );
}

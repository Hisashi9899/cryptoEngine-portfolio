"use client";

import { AlertTriangle, Wallet } from "lucide-react";
import Card from "@/components/Card";
import Badge from "@/components/Badge";
import { api, usePoll } from "@/lib/api";
import { fmtYen } from "@/lib/format";

export default function GmoAffordabilityAlert() {
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
      <Card title="GMO実残高・取引可否" icon={Wallet}>
        <p className="text-sm text-slate-400">
          GMO_API_KEY未設定のため実残高を取得できません。VPS上で
          <span className="font-mono text-slate-200"> bash ops/deploy/setup-gmo.sh </span>
          を実行して読み取り専用キーを設定してください。設定後はJPY余力と銘柄別必要額がここに表示されます。
        </p>
        <p className="mt-2 text-xs text-slate-500">
          現在のBalance表示はpaper計算（初期100万円＋確定損益）であり、GMO口座の4,997円とは別物です。
        </p>
      </Card>
    );
  }

  return (
    <Card
      title="GMO実残高・取引可否"
      icon={Wallet}
      action={
        <span className="text-xs text-slate-500">
          {jpy != null ? `JPY余力: ${fmtYen(jpy)}` : "取得中"}
        </span>
      }
    >
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <span className="text-slate-300">
          GMO実残高: <span className="font-mono font-semibold">{jpy != null ? fmtYen(jpy) : "-"}</span>
        </span>
        {affordable.length > 0 ? (
          <Badge tone="emerald">取引可: {affordable.join("、")}</Badge>
        ) : (
          <Badge tone="red">取引可なし（入金が必要）</Badge>
        )}
      </div>
      {unaffordable.length > 0 && (
        <p className="mt-2 flex items-start gap-2 text-xs text-amber-300">
          <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
          <span>
            資金不足例: {unaffordable.map((s) => `${s.symbol}(約${fmtYen(s.min_jpy)})`).join("、")}
            。JPY余力 {jpy != null ? fmtYen(jpy) : "-"} では発注見送りになります。
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
        paper残高とは別にGMO実残高を表示しています。発注前にもcore側で資金ガードします。
      </p>
    </Card>
  );
}

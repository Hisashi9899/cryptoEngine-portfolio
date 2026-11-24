# 製品データポリシー（VPS 完結・短期窓）

板マイクロストラクチャ系は体制変化が激しく、古いデータは較正を汚染する。
よって生の市場データは VPS 内の短期窓に閉じ込め、ローカルには成果物だけ
持ち帰る。VPS のディスクは有限（2026-09-16 時点で trade_logs 626MB・増加中）
のため、以下は上限付きで運用する。

## 置き場の原則

- VPS が真実源。取得→較正→更新の環は VPS 内で完結する
  （cron capture → 週次 sweep → `calibration_latest.json` → trader 自動再読込）。
- ローカル PC には raw を溜めない。持ち帰るのは監査・成果物のみ。
- 製品別サンドボックス: `backend/core/trade_logs/{spot,funding,grid}/`。
  spot が現行 oversea（GMO 現物 v2）。funding（ファンディング裁定）・grid
  （グリッド Bot）は将来製品用の予約であり、板短期窓では足りない
  （funding 率履歴・長期ボラ等）ためデータ設計は製品ごとに分ける。

## 保持期間（`prune-trade-logs.sh` が強制）

| 対象 | 保持 | 理由 |
|---|---|---|
| `orderbook_*.csv` | 14 日 | 較正窓は 2〜4 週で十分。週次 sweep は直近7日連結を使う |
| `signals_v2_*.jsonl` | 14 日 | paper 記録の再計算用。超過分は捨てる |
| `shadow_edge.jsonl` | 2 万行 | 弱 edge 調査用。超過は末尾保持 |
| `calibration_YYYYMMDD.json` | 60 日 | 4KB/件と軽量。履歴比較用に長め |
| `calibration_latest.json` | 恒久 | trader の live seed。**削除禁止** |
| `trade_log.csv` | 恒久 | 監査証跡。**削除禁止** |
| `trader_heartbeat.json` | 恒久 | 生存管理。**削除禁止**（rsync 除外にも登録） |
| `ops/logs/cron-*.log` | 2000 行/件 | 運用ログの上限 |
| DB 時系列 | 45 日 | `rotate-db.sh`（positions/users 等の状態系は対象外） |

## ローカルの持ち帰り範囲（`sync-master.sh pull`）

- `trade_log.csv`（監査）
- `calibration_latest.json`（成果物）
- `trader_heartbeat.json`（稼働証跡）
- `--db` 指定時のみ DB ダンプ

板 CSV・signals jsonl・cron 生ログの bulk pull は廃止。
月次 cold archive が要る場合は別途 `pull --archive YYYYMM` で検討する。

## VPS 検証手順（ローカル検証の代替）

検証・sweep・レポートはすべて VPS 上で `core` コンテナ経由で行う。
ローカルにデータを置かない。trader 常駐と CPU を食い合わないよう、
バッチ系 cron には `nice -n 10` を付与する
（`ops/scripts/remote/nice-batch-cron.sh` で一括適用・冪等）。

```sh
# VPS 上での定型操作（COMPOSE は prod 合成）
cd /opt/cryptoengine/infra/docker
C="docker compose -f docker-compose.yml -f docker-compose.prod.yml"

# sweep・レポート（例: 前日分 BTC・FAK 手数料想定）
$C exec -T core core sweep-v2 \
  /trade_logs/orderbook_BTC_20260917.csv \
  --fee-bps=0.0015 --min-pairs=20 \
  --out=/trade_logs/sweep_manual.json \
  --cal-out=/trade_logs/calibration_manual.json
$C exec -T core core report-v2 \
  /trade_logs/sweep_manual.json --out=/trade_logs/report_manual.md

# 較正の本番投入（trader が mtime 変化で自動再読込、再起動不要）
cp /trade_logs/calibration_manual.json /trade_logs/calibration_latest.json
```

ギア変更＝較正 JSON の差し替えのみ。trader・DB の再起動は不要。
`analyze-local.sh`（ローカル検証）は bulk pull 廃止に伴い使用しない。

## shadow 監査（veto-state forward-return profiler）

不発火 shadow に固定ホールド後の純損益・上限（oracle）を付け、
veto×レジーム×ホールド別に集計する。方向を仮定しないため
ヒンドサイトにならない。oracle は主張可能な利益ではなく上限。
`core shadow-audit`（DB 不要・読み取り専用）で実行し、
日曜 07:00 cron が `shadow_audit_weekly.json` を上書き更新する
（`run --rm` のため稼働コンテナに影響なし）。

## 学習環（VPS 完結・累積型）

取引ゼロでも相場に追従するための較正フロー。すべて VPS 内で完結する。

- paper 擬似ラベル（日曜 07:30）：`core paper-calib` が paper BUY/SELL 判定に
  事後板で固定ホールド純損益を付けて的中ラベル化し、`calibration_latest.json`
  自体に書き戻して累積する（上書きでなく育てる）。
- live 蓄積：trader は決済確定ごとに RecordTrade し、60 ティック毎＋終了時に
  `calibration_live.json` へ書き出す。再起動時は seed より新しければ復元する。
- 週次 sweep（日曜 06:10）：候補テーブルを `calib-merge` で latest にマージする。
- trader は latest の mtime 変化で自動再読込する（再起動不要）。

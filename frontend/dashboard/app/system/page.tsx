"use client";

import { Activity, AlertTriangle, Cpu, Server } from "lucide-react";
import PageHeader from "@/components/PageHeader";
import Card from "@/components/Card";
import StatusCard from "@/components/StatusCard";
import Table, { type Column } from "@/components/Table";
import Badge from "@/components/Badge";
import {
  api,
  usePoll,
  type ContainerInfo,
  type ErrorLogEntry,
} from "@/lib/api";
import { fmtClock, fmtDuration } from "@/lib/format";
import AutoTradePanel from "./AutoTradePanel";

export default function SystemPage() {
  const { data, error } = usePoll(api.system, 5000);

  const containerColumns: Column<ContainerInfo>[] = [
    {
      key: "name",
      label: "Container（コンテナ）",
      render: (c) => <span className="font-mono text-xs text-slate-200">{c.name}</span>,
    },
    {
      key: "image",
      label: "Image（イメージ）",
      render: (c) => <span className="font-mono text-xs text-slate-500">{c.image}</span>,
    },
    {
      key: "status",
      label: "Status（状態）",
      render: (c) =>
        c.status === "running" ? (
          <Badge tone="emerald">● running</Badge>
        ) : (
          <Badge tone="red">● {c.status}</Badge>
        ),
    },
    {
      key: "uptime",
      label: "Uptime（稼働時間）",
      render: (c) => <span className="text-slate-400">{c.uptime}</span>,
    },
  ];

  const errorColumns: Column<ErrorLogEntry>[] = [
    {
      key: "timestamp",
      label: "Time（時刻）",
      render: (e) => <span className="text-slate-400">{fmtClock(e.timestamp)}</span>,
    },
    {
      key: "level",
      label: "Level（レベル）",
      render: (e) =>
        e.level === "error" ? (
          <Badge tone="red">error</Badge>
        ) : (
          <Badge tone="amber">warn</Badge>
        ),
    },
    {
      key: "message",
      label: "Message（メッセージ）",
      render: (e) => <span className="text-slate-300">{e.message}</span>,
    },
  ];

  const coreOk = data?.core.status === "running";

  return (
    <>
      <PageHeader
        title="System"
        subtitle="core / API / Docker の稼働状況"
      />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatusCard
          label="Core Engine（コアエンジン）"
          value={data?.core.status ?? "…"}
          icon={Cpu}
          tone={coreOk ? "emerald" : "red"}
          sub={
            data
              ? `v${data.core.version} · uptime ${fmtDuration(data.core.uptime_sec)}`
              : ""
          }
        />
        <StatusCard
          label="API Latency（API 応答速度）"
          value={`${data?.api.latency_ms ?? "-"} ms`}
          icon={Activity}
          tone="sky"
          sub={`${data?.api.requests_1h ?? 0} リクエスト/h`}
        />
        <StatusCard
          label="Containers（コンテナ数）"
          value={String(data?.docker.containers.length ?? 0)}
          icon={Server}
          tone="navy"
          sub={data?.docker.containers.map((c) => c.name).join(", ")}
        />
        <StatusCard
          label="Errors (24h)（エラー数）"
          value={String(data?.errors.length ?? 0)}
          icon={AlertTriangle}
          tone={data?.errors.length ? "amber" : "emerald"}
          sub="警告 + エラー"
        />
      </div>

      {error && (
        <p className="mt-4 rounded-card border border-red-500/20 bg-red-500/10 px-4 py-3 text-sm text-red-300">
          {error}
        </p>
      )}

      <div className="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Card title="Docker Containers（Docker コンテナ）" icon={Server}>
          <Table
            columns={containerColumns}
            rows={data?.docker.containers ?? []}
            rowKey={(c) => c.name}
            emptyMessage="コンテナ情報がありません"
          />
        </Card>
        <Card title="Error Log（エラーログ）" icon={AlertTriangle}>
          <Table
            columns={errorColumns}
            rows={data?.errors ?? []}
            rowKey={(e, i) => `${e.timestamp}-${i}`}
            emptyMessage="エラーはありません"
          />
        </Card>
      </div>

      <div className="mt-4 border-t border-white/5 pt-4">
        <AutoTradePanel />
      </div>
    </>
  );
}

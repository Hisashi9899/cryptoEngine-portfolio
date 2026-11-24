import type { ReactNode } from "react";
import { cn } from "@/lib/cn";

type Tone = "emerald" | "red" | "amber" | "slate" | "sky";

const TONES: Record<Tone, string> = {
  emerald: "bg-emerald-500/10 text-emerald-300 ring-emerald-500/25",
  red: "bg-red-500/10 text-red-300 ring-red-500/25",
  amber: "bg-amber-500/10 text-amber-300 ring-amber-500/25",
  slate: "bg-slate-500/10 text-slate-300 ring-slate-500/25",
  sky: "bg-sky-500/10 text-sky-300 ring-sky-500/25",
};

type Props = {
  tone?: Tone;
  children: ReactNode;
  className?: string;
};

export default function Badge({ tone = "slate", children, className }: Props) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 whitespace-nowrap rounded-pill px-2.5 py-0.5 text-xs font-medium ring-1",
        TONES[tone],
        className
      )}
    >
      {children}
    </span>
  );
}

export function SideBadge({ side }: { side: string }) {
  if (side === "buy") return <Badge tone="emerald">▲ BUY</Badge>;
  if (side === "sell") return <Badge tone="red">▼ SELL</Badge>;
  return <Badge tone="slate">—</Badge>;
}

export function SignalBadge({ type }: { type: string }) {
  if (type === "buy") return <Badge tone="emerald">▲ BUY</Badge>;
  if (type === "sell") return <Badge tone="red">▼ SELL</Badge>;
  return <Badge tone="slate">HOLD</Badge>;
}

export function DemoBadge() {
  return <Badge tone="amber">DEMO データ</Badge>;
}

export function LiveBadge() {
  return <Badge tone="emerald">LIVE 実データ</Badge>;
}

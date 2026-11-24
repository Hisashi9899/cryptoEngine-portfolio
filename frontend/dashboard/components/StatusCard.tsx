import type { ComponentType, ReactNode } from "react";
import { cn } from "@/lib/cn";

type Props = {
  label: string;
  value: string;
  sub?: string;
  icon: ComponentType<{ className?: string }>;
  delta?: ReactNode;
  deltaPositive?: boolean;
  tone?: "emerald" | "red" | "amber" | "sky" | "navy";
  title?: string;
};

const TONE_STYLES: Record<NonNullable<Props["tone"]>, string> = {
  emerald: "from-emerald-400/25 to-emerald-600/5 text-emerald-400",
  red: "from-red-400/25 to-red-600/5 text-red-400",
  amber: "from-amber-400/25 to-amber-600/5 text-amber-400",
  sky: "from-sky-400/25 to-sky-600/5 text-sky-400",
  navy: "from-slate-300/25 to-slate-500/5 text-slate-300",
};

export default function StatusCard({
  label,
  value,
  sub,
  icon: Icon,
  delta,
  deltaPositive = true,
  tone = "emerald",
  title,
}: Props) {
  return (
    <div
      className="group rounded-card border border-white/5 bg-navy-850/70 p-5 shadow-card backdrop-blur transition-transform duration-200 hover:-translate-y-0.5"
      title={title}
    >
      <div className="flex items-center justify-between gap-2">
        <p className="truncate text-xs font-medium uppercase tracking-wider text-slate-500">
          {label}
        </p>
        <span
          className={cn(
            "grid h-9 w-9 shrink-0 place-items-center rounded-lg bg-gradient-to-br",
            TONE_STYLES[tone]
          )}
        >
          <Icon className="h-4 w-4" />
        </span>
      </div>
      <p className="mt-2 font-mono text-2xl font-semibold tracking-tight text-white">
        {value}
      </p>
      <div className="mt-1 flex min-w-0 items-center gap-2 text-xs">
        {delta && (
          <span
            className={cn(
              "shrink-0 font-medium",
              deltaPositive ? "text-emerald-400" : "text-red-400"
            )}
          >
            {deltaPositive ? "+" : ""}
            {delta}
          </span>
        )}
        {sub && <span className="truncate text-slate-500">{sub}</span>}
      </div>
    </div>
  );
}

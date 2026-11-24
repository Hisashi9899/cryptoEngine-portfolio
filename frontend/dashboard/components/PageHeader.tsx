import type { ReactNode } from "react";

type Props = {
  title: string;
  subtitle?: string;
  live?: boolean;
  children?: ReactNode;
};

export default function PageHeader({
  title,
  subtitle,
  live = true,
  children,
}: Props) {
  return (
    <header className="mb-6 flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
      <div>
        <div className="flex items-center gap-3">
          <h1 className="text-2xl font-bold tracking-tight text-white">
            {title}
          </h1>
          {live && (
            <span className="flex items-center gap-1.5 rounded-pill border border-emerald-500/20 bg-emerald-500/10 px-2.5 py-1 text-[11px] font-medium text-emerald-300">
              <span className="relative flex h-1.5 w-1.5">
                <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-60" />
                <span className="relative inline-flex h-1.5 w-1.5 rounded-full bg-emerald-400" />
              </span>
              LIVE
            </span>
          )}
        </div>
        {subtitle && <p className="mt-1 text-sm text-slate-400">{subtitle}</p>}
      </div>
      {children}
    </header>
  );
}

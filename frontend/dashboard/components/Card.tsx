import type { ReactNode } from "react";
import type { LucideIcon } from "lucide-react";
import { cn } from "@/lib/cn";

type Props = {
  title?: string;
  icon?: LucideIcon;
  action?: ReactNode;
  className?: string;
  children: ReactNode;
};

export default function Card({
  title,
  icon: Icon,
  action,
  className,
  children,
}: Props) {
  return (
    <section
      className={cn(
        "rounded-card border border-white/5 bg-navy-850/70 p-5 shadow-card backdrop-blur",
        className
      )}
    >
      {(title || Icon || action) && (
        <div className="mb-4 flex items-center justify-between gap-3">
          <div className="flex min-w-0 items-center gap-2">
            {Icon && (
              <span className="grid h-8 w-8 shrink-0 place-items-center rounded-lg bg-emerald-500/10 text-emerald-400">
                <Icon className="h-4 w-4" />
              </span>
            )}
            {title && (
              <h2 className="truncate text-sm font-semibold text-slate-200">
                {title}
              </h2>
            )}
          </div>
          {action}
        </div>
      )}
      {children}
    </section>
  );
}

"use client";

import { cn } from "@/lib/cn";
import { useI18n, type Locale } from "@/lib/i18n";

export default function LanguageToggle({ className }: { className?: string }) {
  const { locale, setLocale } = useI18n();
  const options: Locale[] = ["ja", "en"];

  return (
    <div
      className={cn(
        "inline-flex items-center rounded-md border border-white/10 bg-white/[0.03] p-0.5 text-[10px]",
        className
      )}
      aria-label="Language"
    >
      {options.map((option) => (
        <button
          key={option}
          type="button"
          onClick={() => setLocale(option)}
          aria-pressed={locale === option}
          className={cn(
            "rounded px-2 py-1 font-semibold uppercase tracking-wide transition-colors",
            locale === option
              ? "bg-emerald-500/20 text-emerald-300"
              : "text-slate-500 hover:text-slate-200"
          )}
        >
          {option}
        </button>
      ))}
    </div>
  );
}

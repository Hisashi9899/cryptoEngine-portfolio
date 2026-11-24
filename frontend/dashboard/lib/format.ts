export function fmt(n: number | null | undefined, digits = 0): string {
  if (n === null || n === undefined || Number.isNaN(n)) return "-";
  return new Intl.NumberFormat("ja-JP", {
    maximumFractionDigits: digits,
    minimumFractionDigits: digits,
  }).format(n);
}

export function fmtTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString("ja-JP", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

export function fmtClock(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleTimeString("ja-JP", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

export function fmtDuration(sec: number): string {
  const d = Math.floor(sec / 86400);
  const h = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}

export function fmtYen(n: number | null | undefined, digits = 0): string {
  if (n === null || n === undefined || Number.isNaN(n)) return "-";
  return `${fmt(n, digits)}円`;
}

export function fmtUSDT(n: number | null | undefined, digits = 2): string {
  if (n === null || n === undefined || Number.isNaN(n)) return "-";
  return `${fmt(n, digits)}（USDT）`;
}

export function fmtPLColor(value: number): string {
  if (value > 0) return "text-emerald-400";
  if (value < 0) return "text-red-400";
  return "text-slate-400";
}

export function translateReason(reason: string): string {
  return reason
    .replace(/buy pressure/g, "買い圧")
    .replace(/sell pressure/g, "売り圧")
    .replace(/no clear signal/g, "明確なシグナルなし")
    .replace(/imbalance=/g, "インバランス=")
    .replace(/spread=/g, "スプレッド=");
}

export function translateReasonShort(reason: string): string {
  if (reason.includes("buy pressure")) return "買い圧";
  if (reason.includes("sell pressure")) return "売り圧";
  if (reason.includes("no clear signal")) return "明確なシグナルなし";
  return reason;
}

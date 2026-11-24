"use client";

import { useEffect, useRef } from "react";
import type {
  TickMarkFormatter,
  Time,
  UTCTimestamp,
} from "lightweight-charts";

export type ChartPoint = { timestamp: string; balance: number };

function toTime(iso: string): UTCTimestamp {
  return Math.floor(new Date(iso).getTime() / 1000) as UTCTimestamp;
}

// 時間軸は日本時間（Asia/Tokyo）で統一する。
// lightweight-charts の既定はUTC表示のため、目盛りとクロスヘアの
// 両方を上書きする（timeFormatter はクロスヘア専用のため tickMarkFormatter が本体）。
const jstTime = new Intl.DateTimeFormat("ja-JP", {
  timeZone: "Asia/Tokyo",
  hour: "2-digit",
  minute: "2-digit",
  hour12: false,
});
const jstTimeFormatter = (t: UTCTimestamp): string =>
  jstTime.format(new Date(t * 1000));
const toUnixSec = (t: Time): number => {
  if (typeof t === "number") return t;
  if (typeof t === "string") {
    const ms = new Date(t).getTime();
    return Number.isNaN(ms) ? 0 : Math.floor(ms / 1000);
  }
  return Math.floor(Date.UTC(t.year, t.month - 1, t.day) / 1000);
};

// 目盛りは常に日本時間の時:分で出す（日内スナップショット想定）。
const jstTickFormatter: TickMarkFormatter = (t) =>
  jstTime.format(new Date(toUnixSec(t) * 1000));

type Props = {
  data: ChartPoint[];
  color?: string;
  height?: number;
  decimals?: number;
};

function hexToRgba(hex: string, alpha: number): string {
  const m = hex.replace("#", "");
  const r = parseInt(m.slice(0, 2), 16);
  const g = parseInt(m.slice(2, 4), 16);
  const b = parseInt(m.slice(4, 6), 16);
  return `rgba(${r}, ${g}, ${b}, ${alpha})`;
}

export default function Chart({
  data,
  color = "#10B981",
  height = 260,
  decimals = 0,
}: Props) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const chartRef = useRef<any>(null);
  const seriesRef = useRef<any>(null);
  const dataRef = useRef<ChartPoint[]>(data);

  useEffect(() => {
    dataRef.current = data;
  }, [data]);

  useEffect(() => {
    let disposed = false;
    let ro: ResizeObserver | null = null;

    (async () => {
      const { createChart } = await import("lightweight-charts");
      if (disposed || !containerRef.current) return;

      const chart = createChart(containerRef.current, {
        height,
        layout: {
          background: { color: "transparent" },
          textColor: "#64748B",
          fontFamily: "'Inter', 'Noto Sans JP', system-ui, sans-serif",
          fontSize: 11,
        },
        grid: {
          vertLines: { color: "rgba(148, 163, 184, 0.06)" },
          horzLines: { color: "rgba(148, 163, 184, 0.06)" },
        },
        rightPriceScale: { borderColor: "rgba(148, 163, 184, 0.12)" },
        timeScale: {
          borderColor: "rgba(148, 163, 184, 0.12)",
          timeVisible: true,
          tickMarkFormatter: jstTickFormatter,
        },
        localization: { locale: "ja-JP", timeFormatter: jstTimeFormatter },
        crosshair: {
          vertLine: { color, width: 1, style: 2 },
          horzLine: { color, width: 1, style: 2 },
        },
        handleScroll: false,
        handleScale: false,
      });

      const series = chart.addAreaSeries({
        lineColor: color,
        topColor: hexToRgba(color, 0.3),
        bottomColor: hexToRgba(color, 0.02),
        lineWidth: 2,
        priceFormat: { type: "price", precision: decimals, minMove: 10 ** -decimals },
      });

      chartRef.current = chart;
      seriesRef.current = series;

      // スマホ回転・リサイズに追従させる。
      if (typeof ResizeObserver !== "undefined" && containerRef.current) {
        ro = new ResizeObserver(() => {
          const el = containerRef.current;
          const c = chartRef.current;
          if (!el || !c) return;
          c.applyOptions({ width: el.clientWidth });
        });
        ro.observe(containerRef.current);
      }

      const points = dataRef.current;
      if (points.length > 0) {
        series.setData(
          points.map((p) => ({
            time: toTime(p.timestamp),
            value: p.balance,
          }))
        );
        chart.timeScale().fitContent();
      }
    })();

    return () => {
      disposed = true;
      ro?.disconnect();
      chartRef.current?.remove();
      chartRef.current = null;
      seriesRef.current = null;
    };
  }, [color, height, decimals]);

  useEffect(() => {
    const chart = chartRef.current;
    const series = seriesRef.current;
    if (!chart || !series || data.length === 0) return;
    series.setData(
      data.map((p) => ({
        time: toTime(p.timestamp),
        value: p.balance,
      }))
    );
    chart.timeScale().fitContent();
  }, [data]);

  return <div ref={containerRef} className="w-full" style={{ height }} />;
}

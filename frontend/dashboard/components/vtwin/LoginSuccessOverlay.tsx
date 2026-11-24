"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import dynamic from "next/dynamic";
import { useProgress } from "@react-three/drei";
import type { Stage } from "@/components/vtwin3d/types";

const VTwinScene = dynamic(() => import("@/components/vtwin3d/VTwinScene"), {
  ssr: false,
  loading: () => null,
});

const SEQ: Stage[] = ["rev", "turn", "ride", "flash"];
const DURATION: Record<Stage, number> = { rev: 1100, turn: 1500, ride: 1600, flash: 800 };

const WIND = [
  { top: "16%", delay: "0s", dur: "0.55s" },
  { top: "30%", delay: "0.18s", dur: "0.5s" },
  { top: "52%", delay: "0.1s", dur: "0.6s" },
  { top: "70%", delay: "0.28s", dur: "0.45s" },
  { top: "40%", delay: "0.34s", dur: "0.62s" },
];
const PAST = [
  { x: "10%", y: "26%", delay: "0s", dur: "0.8s" },
  { x: "22%", y: "58%", delay: "0.22s", dur: "0.7s" },
  { x: "38%", y: "18%", delay: "0.4s", dur: "0.9s" },
  { x: "58%", y: "68%", delay: "0.12s", dur: "0.75s" },
  { x: "76%", y: "34%", delay: "0.3s", dur: "0.85s" },
  { x: "88%", y: "56%", delay: "0.5s", dur: "0.7s" },
];

export default function LoginSuccessOverlay({ onDone }: { onDone?: () => void }) {
  const router = useRouter();
  const [stage, setStage] = useState<Stage>("rev");
  const [ready, setReady] = useState(false);
  const [lite, setLite] = useState(false);
  const { active, progress } = useProgress();
  const doneRef = useRef(onDone);
  doneRef.current = onDone;

  // iPhone等（細画面・タッチ端末）ではWebGLシーンを持たない軽量版にする。
  // フル3DはデスクトップGPU向けであり、モバイルでは composite 負荷で
  // カクツキの主因になるため。
  useEffect(() => {
    try {
      const small = window.matchMedia("(max-width: 640px)").matches;
      const coarse = window.matchMedia("(pointer: coarse)").matches;
      if (small || coarse) setLite(true);
    } catch {
      // matchMedia 不可の環境ではフル版のまま
    }
  }, []);

  useEffect(() => {
    if (!active && progress > 0 && progress >= 100) setReady(true);
  }, [active, progress]);

  useEffect(() => {
    if (ready) return;
    const t = window.setTimeout(() => setReady(true), 8000);
    return () => window.clearTimeout(t);
  }, [ready]);

  useEffect(() => {
    if (!ready) return;
    if (typeof window !== "undefined" && window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
      doneRef.current?.();
      router.push("/");
      return;
    }
    // 軽量版: 短いフェードのみでダッシュボードへ（3Dなし・無限アニメなし）。
    if (lite) {
      const t = window.setTimeout(() => {
        doneRef.current?.();
        router.push("/");
      }, 900);
      return () => window.clearTimeout(t);
    }
    const timers: number[] = [];
    let acc = 0;
    for (let i = 1; i < SEQ.length; i++) {
      acc += DURATION[SEQ[i - 1]];
      timers.push(window.setTimeout(() => setStage(SEQ[i]), acc));
    }
    acc += DURATION[SEQ[SEQ.length - 1]];
    timers.push(
      window.setTimeout(() => {
        doneRef.current?.();
        router.push("/");
      }, acc)
    );
    return () => timers.forEach((t) => window.clearTimeout(t));
  }, [router, ready]);

  return (
    <div className="ce-overlay-in fixed inset-0 z-[90] overflow-hidden bg-navy-950">
      <div
        className="pointer-events-none absolute inset-0"
        style={{
          background:
            "linear-gradient(115deg, rgba(226,232,240,0.07) 0%, transparent 22%, rgba(226,232,240,0.05) 40%, transparent 58%, rgba(226,232,240,0.06) 78%, transparent 100%), radial-gradient(900px 480px at 50% -5%, rgba(16,185,129,0.16), transparent 62%), radial-gradient(700px 400px at 100% 100%, rgba(59,130,246,0.08), transparent 55%), linear-gradient(165deg, #0d1526 0%, #070b16 45%, #04060d 100%)",
        }}
      />
      <div className={`relative h-full w-full ${stage === "ride" && !lite ? "ce-shake" : ""}`}>
        {!lite && (
          <div className="pointer-events-none absolute left-1/2 top-[46%] w-[min(94vw,820px)] aspect-[13/9] -translate-x-1/2 -translate-y-1/2">
            <VTwinScene stage={stage} />
          </div>
        )}

        {stage !== "flash" && (
          <div className="absolute inset-x-0 bottom-[10%] px-4 text-center">
            <h1 className="text-4xl font-extrabold tracking-tight text-slate-100 md:text-5xl">
              Crypto
              <span className="bg-gradient-to-r from-emerald-300 to-emerald-500 bg-clip-text text-transparent">
                Engine
              </span>
            </h1>
            <p className="mt-2 text-[11px] uppercase tracking-[0.42em] text-emerald-400/90">
              {ready ? "Powered by Crypto · Ignition" : "Loading…"}
            </p>
          </div>
        )}
      </div>

      {stage === "ride" && !lite && (
        <div className="pointer-events-none absolute inset-0 overflow-hidden">
          {WIND.map((w, i) => (
            <div
              key={i}
              className="ce-wind absolute left-0 h-px w-40 bg-gradient-to-r from-transparent via-emerald-300/80 to-transparent"
              style={{ top: w.top, animationDelay: w.delay, animationDuration: w.dur }}
            />
          ))}
          {PAST.map((p, i) => (
            <div
              key={i}
              className="ce-past absolute h-1.5 w-1.5 rounded-full bg-emerald-300"
              style={{ left: p.x, top: p.y, animationDelay: p.delay, animationDuration: p.dur }}
            />
          ))}
        </div>
      )}

      {stage === "flash" && <div className="ce-flash pointer-events-none absolute inset-0 bg-white" />}
    </div>
  );
}

"use client";

import { Suspense } from "react";
import { Canvas } from "@react-three/fiber";
import { Engine3D } from "./Engine3D";
import { Bike3D } from "./Bike3D";
import { ProceduralBike } from "./ProceduralBike";
import { CameraRig } from "./CameraRig";
import { Lights } from "./Lights";
import { ModelBoundary } from "./ModelBoundary";
import type { Stage } from "./types";

export default function VTwinScene({ stage }: { stage: Stage }) {
  return (
    <Canvas
      camera={{ position: [0, 0.6, 4.5], fov: 45 }}
      dpr={[1, 2]}
      gl={{ alpha: true, antialias: true }}
    >
      <Suspense fallback={null}>
        <Lights />
        <ModelBoundary stage={stage}>
          <Engine3D stage={stage} />
        </ModelBoundary>
        <ModelBoundary stage={stage} fallback={ProceduralBike}>
          <Bike3D stage={stage} />
        </ModelBoundary>
        <CameraRig stage={stage} />
      </Suspense>
    </Canvas>
  );
}

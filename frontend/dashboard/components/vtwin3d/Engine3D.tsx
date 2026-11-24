"use client";

import { useEffect, useRef } from "react";
import { useGLTF } from "@react-three/drei";
import * as THREE from "three";
import { useVTwinAnimation } from "./useVTwinAnimation";
import type { Stage } from "./types";

import { asset } from "@/lib/asset";

const MODEL_PATH = asset("/models/vtwin.glb");

type GLTFData = {
  scene: THREE.Group;
  nodes: Record<string, THREE.Object3D>;
};

export function Engine3D({ stage }: { stage: Stage }) {
  const { scene, nodes } = useGLTF(MODEL_PATH) as unknown as GLTFData;

  const engineRig = useRef<THREE.Group | null>(null);
  const crank = useRef<THREE.Group | null>(null);
  const airCleaner = useRef<THREE.Group | null>(null);
  const pistonL = useRef<THREE.Group | null>(null);
  const pistonR = useRef<THREE.Group | null>(null);
  const coreGlow = useRef<THREE.Mesh | null>(null);
  const coreLight = useRef<THREE.PointLight | null>(null);
  const flameL = useRef<THREE.Mesh | null>(null);
  const flameR = useRef<THREE.Mesh | null>(null);
  const exhaustL = useRef<THREE.Group | null>(null);
  const exhaustR = useRef<THREE.Group | null>(null);

  useEffect(() => {
    engineRig.current = (nodes.engineRig as THREE.Group | undefined) ?? null;
    crank.current = (nodes.crank as THREE.Group | undefined) ?? null;
    airCleaner.current = (nodes.airCleaner as THREE.Group | undefined) ?? null;
    pistonL.current = (nodes.pistonL as THREE.Group | undefined) ?? null;
    pistonR.current = (nodes.pistonR as THREE.Group | undefined) ?? null;
    coreGlow.current = (nodes.coreGlow as THREE.Mesh | undefined) ?? null;
    exhaustL.current = (nodes.exhaustL as THREE.Group | undefined) ?? null;
    exhaustR.current = (nodes.exhaustR as THREE.Group | undefined) ?? null;
  }, [nodes]);

  useVTwinAnimation(
    {
      engineRig,
      crank,
      airCleaner,
      pistonL,
      pistonR,
      coreGlow,
      coreLight,
      flameL,
      flameR,
      exhaustL,
      exhaustR,
    },
    stage
  );

  return (
    <group visible={stage === "rev" || stage === "turn"}>
      <primitive object={scene}>
        <mesh ref={flameL} visible={false}>
          <planeGeometry args={[0.3, 0.55]} />
          <meshBasicMaterial
            color="#34d399"
            transparent
            opacity={0}
            blending={THREE.AdditiveBlending}
            side={THREE.DoubleSide}
            depthWrite={false}
          />
        </mesh>
        <mesh ref={flameR} visible={false}>
          <planeGeometry args={[0.3, 0.55]} />
          <meshBasicMaterial
            color="#34d399"
            transparent
            opacity={0}
            blending={THREE.AdditiveBlending}
            side={THREE.DoubleSide}
            depthWrite={false}
          />
        </mesh>
        <pointLight ref={coreLight} color="#34d399" intensity={0} distance={5} />
      </primitive>
    </group>
  );
}

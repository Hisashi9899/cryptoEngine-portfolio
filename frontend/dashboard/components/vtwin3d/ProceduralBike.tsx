"use client";

import { forwardRef, useRef } from "react";
import { ContactShadows } from "@react-three/drei";
import * as THREE from "three";
import { useVTwinAnimation } from "./useVTwinAnimation";
import type { Stage } from "./types";

const CHROME = { metalness: 0.9, roughness: 0.2 };
const DARK = { metalness: 0.5, roughness: 0.5 };

const Wheel3D = forwardRef<THREE.Group | null, { forward: number }>(({ forward }, ref) => (
  <group ref={ref} position={[0, 0.42, forward * 0.9]}>
    <mesh rotation={[0, Math.PI / 2, 0]}>
      <torusGeometry args={[0.42, 0.07, 12, 32]} />
      <meshStandardMaterial color="#0b1322" {...DARK} />
    </mesh>
    <mesh rotation={[0, Math.PI / 2, 0]}>
      <cylinderGeometry args={[0.05, 0.05, 0.16, 12]} />
      <meshStandardMaterial color="#94a3b8" {...CHROME} />
    </mesh>
  </group>
));
Wheel3D.displayName = "Wheel3D";

export function ProceduralBike({ stage }: { stage: Stage }) {
  const bikeRig = useRef<THREE.Group | null>(null);
  const wheelF = useRef<THREE.Group | null>(null);
  const wheelR = useRef<THREE.Group | null>(null);

  useVTwinAnimation({ bikeRig, wheelF, wheelR }, stage);

  return (
    <group visible={stage !== "rev"}>
      <group ref={bikeRig} rotation={[0, -0.55, 0]}>
        <Wheel3D ref={wheelR} forward={-1} />
        <Wheel3D ref={wheelF} forward={1} />
        <mesh position={[0, 0.55, 0]}>
          <boxGeometry args={[0.34, 0.16, 1.7]} />
          <meshStandardMaterial color="#0b1322" {...DARK} />
        </mesh>
        <mesh position={[0, 0.72, 0]}>
          <cylinderGeometry args={[0.09, 0.13, 0.8, 12]} />
          <meshStandardMaterial color="#0b1322" {...DARK} />
        </mesh>
        <mesh position={[0, 0.72, 0.85]}>
          <sphereGeometry args={[0.11, 16, 16]} />
          <meshBasicMaterial color="#a7f3d0" />
        </mesh>
        <mesh position={[0.22, 0.32, -0.7]}>
          <cylinderGeometry args={[0.045, 0.045, 0.6, 8]} />
          <meshStandardMaterial color="#475569" {...CHROME} />
        </mesh>
        <mesh position={[-0.22, 0.32, -0.7]}>
          <cylinderGeometry args={[0.045, 0.045, 0.6, 8]} />
          <meshStandardMaterial color="#475569" {...CHROME} />
        </mesh>
        <mesh position={[0, 0.85, 0.35]}>
          <boxGeometry args={[0.16, 0.06, 0.4]} />
          <meshStandardMaterial color="#0b1322" {...DARK} />
        </mesh>
      </group>
      <ContactShadows position={[0, 0, 0]} scale={3} blur={2.4} opacity={0.35} />
    </group>
  );
}

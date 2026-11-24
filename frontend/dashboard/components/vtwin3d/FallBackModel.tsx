"use client";

import { useRef } from "react";
import * as THREE from "three";
import { useVTwinAnimation } from "./useVTwinAnimation";
import type { Stage } from "./types";

const CHROME = { metalness: 0.95, roughness: 0.18 };
const DARK = { metalness: 0.7, roughness: 0.35 };

type BarrelProps = {
  rot: number;
  piston: (el: THREE.Group | null) => void;
  exhaust: (el: THREE.Group | null) => void;
};

function Barrel({ rot, piston, exhaust }: BarrelProps) {
  return (
    <group position={[0, 0.42, 0]} rotation={[0, 0, rot]}>
      <mesh position={[0, 0.28, 0]}>
        <cylinderGeometry args={[0.24, 0.24, 0.85, 24]} />
        <meshStandardMaterial color="#131c31" metalness={0.6} roughness={0.42} />
      </mesh>
      {Array.from({ length: 9 }, (_, i) => (
        <mesh key={i} position={[0, 0.02 + i * 0.075, 0]}>
          <cylinderGeometry
            args={[0.3 + (i % 2) * 0.012, 0.3 + ((i + 1) % 2) * 0.012, 0.04, 24]}
          />
          <meshStandardMaterial color="#1a2438" metalness={0.85} roughness={0.28} />
        </mesh>
      ))}
      <mesh position={[0, -0.04, 0]}>
        <cylinderGeometry args={[0.34, 0.36, 0.14, 24]} />
        <meshStandardMaterial color="#0f1830" {...DARK} />
      </mesh>
      <mesh position={[0, 0.68, 0]}>
        <cylinderGeometry args={[0.33, 0.31, 0.18, 24]} />
        <meshStandardMaterial color="#1b2740" metalness={0.8} roughness={0.3} />
      </mesh>
      <group position={[0, 0.84, 0]}>
        <mesh>
          <cylinderGeometry args={[0.35, 0.34, 0.16, 24]} />
          <meshStandardMaterial color="#243b5e" metalness={0.85} roughness={0.25} />
        </mesh>
        <mesh position={[0, 0.12, 0]}>
          <cylinderGeometry args={[0.27, 0.27, 0.04, 24]} />
          <meshStandardMaterial color="#2c4a78" metalness={0.9} roughness={0.2} />
        </mesh>
      </group>
      <mesh position={[0.09, 0.45, 0.2]} rotation={[0.1, 0, 0]}>
        <cylinderGeometry args={[0.035, 0.035, 0.72, 12]} />
        <meshStandardMaterial color="#64748b" metalness={0.9} roughness={0.25} />
      </mesh>
      <mesh position={[-0.09, 0.45, 0.2]} rotation={[0.1, 0, 0]}>
        <cylinderGeometry args={[0.035, 0.035, 0.72, 12]} />
        <meshStandardMaterial color="#64748b" metalness={0.9} roughness={0.25} />
      </mesh>
      <group ref={piston} position={[0, 0.32, 0]}>
        <mesh>
          <cylinderGeometry args={[0.16, 0.16, 0.18, 20]} />
          <meshStandardMaterial color="#e2e8f0" metalness={0.8} roughness={0.3} />
        </mesh>
        <mesh position={[0, 0.08, 0]}>
          <cylinderGeometry args={[0.165, 0.165, 0.018, 20]} />
          <meshStandardMaterial color="#334155" metalness={0.9} roughness={0.2} />
        </mesh>
      </group>
      <group ref={exhaust} position={[0.3, -0.1, 0.12]}>
        <mesh rotation={[0, 0, -0.5]}>
          <cylinderGeometry args={[0.055, 0.075, 0.6, 14]} />
          <meshStandardMaterial color="#cbd5e1" {...CHROME} />
        </mesh>
        <mesh rotation={[0, 0, -0.1]} position={[0, -0.27, 0]}>
          <cylinderGeometry args={[0.07, 0.08, 0.3, 14]} />
          <meshStandardMaterial color="#cbd5e1" {...CHROME} />
        </mesh>
        <mesh position={[0.02, -0.44, 0]}>
          <cylinderGeometry args={[0.085, 0.06, 0.06, 14]} />
          <meshStandardMaterial color="#475569" {...CHROME} />
        </mesh>
      </group>
    </group>
  );
}

export function FallBackModel({ stage }: { stage: Stage }) {
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

  const caseBolts: [number, number, number][] = [
    [-0.68, 0.16, 0.41],
    [0.68, 0.16, 0.41],
    [-0.68, -0.02, 0.41],
    [0.68, -0.02, 0.41],
  ];

  return (
    <group visible={stage === "rev" || stage === "turn"}>
      <group ref={engineRig}>
        <mesh position={[0, 0.08, 0]}>
          <boxGeometry args={[1.5, 0.58, 0.85]} />
          <meshStandardMaterial color="#182441" metalness={0.72} roughness={0.35} />
        </mesh>
        <mesh position={[0, 0.08, 0.43]}>
          <boxGeometry args={[1.5, 0.02, 0.02]} />
          <meshStandardMaterial color="#0a1122" metalness={0.5} roughness={0.6} />
        </mesh>
        <mesh position={[0, 0.38, 0]}>
          <boxGeometry args={[1.32, 0.12, 0.66]} />
          <meshStandardMaterial color="#22335a" metalness={0.75} roughness={0.3} />
        </mesh>
        <mesh position={[0, 0.36, 0]}>
          <cylinderGeometry args={[0.5, 0.56, 0.42, 24]} />
          <meshStandardMaterial color="#1b2740" {...DARK} />
        </mesh>
        <mesh position={[0, -0.3, -0.03]}>
          <boxGeometry args={[1.08, 0.18, 0.6]} />
          <meshStandardMaterial color="#101b33" metalness={0.6} roughness={0.42} />
        </mesh>
        <mesh rotation={[Math.PI / 2, 0, 0]} position={[0.38, 0.1, 0.44]}>
          <cylinderGeometry args={[0.3, 0.3, 0.05, 24]} />
          <meshStandardMaterial color="#334a73" metalness={0.85} roughness={0.25} />
        </mesh>
        {caseBolts.map((p, i) => (
          <mesh key={i} position={p} rotation={[Math.PI / 2, 0, 0]}>
            <cylinderGeometry args={[0.035, 0.035, 0.06, 10]} />
            <meshStandardMaterial color="#94a3b8" metalness={0.9} roughness={0.2} />
          </mesh>
        ))}
        <Barrel
          rot={-0.42}
          piston={(el) => {
            pistonL.current = el;
          }}
          exhaust={(el) => {
            exhaustL.current = el;
          }}
        />
        <Barrel
          rot={0.42}
          piston={(el) => {
            pistonR.current = el;
          }}
          exhaust={(el) => {
            exhaustR.current = el;
          }}
        />
        <group ref={crank} position={[0, 0, 0.36]}>
          <mesh rotation={[Math.PI / 2, 0, 0]}>
            <cylinderGeometry args={[0.42, 0.42, 0.09, 24]} />
            <meshStandardMaterial color="#cbd5e1" {...CHROME} />
          </mesh>
          <mesh rotation={[Math.PI / 2, 0, 0]} position={[0, 0, 0.12]}>
            <cylinderGeometry args={[0.27, 0.27, 0.05, 20]} />
            <meshStandardMaterial color="#475569" {...CHROME} />
          </mesh>
          {Array.from({ length: 6 }, (_, i) => {
            const a = (i / 6) * Math.PI * 2;
            return (
              <mesh
                key={i}
                rotation={[Math.PI / 2, 0, 0]}
                position={[Math.cos(a) * 0.3, Math.sin(a) * 0.3, 0.1]}
              >
                <cylinderGeometry args={[0.02, 0.02, 0.03, 8]} />
                <meshStandardMaterial color="#64748b" metalness={0.9} roughness={0.25} />
              </mesh>
            );
          })}
        </group>
        <group ref={airCleaner} position={[0.92, 0.1, 0.36]}>
          <mesh rotation={[Math.PI / 2, 0, 0]}>
            <cylinderGeometry args={[0.28, 0.28, 0.09, 24]} />
            <meshStandardMaterial color="#475569" {...CHROME} />
          </mesh>
          <mesh rotation={[Math.PI / 2, 0, 0]} position={[0, 0, 0.06]}>
            <cylinderGeometry args={[0.3, 0.3, 0.02, 24]} />
            <meshStandardMaterial color="#94a3b8" {...CHROME} />
          </mesh>
          <mesh position={[0, 0, 0.08]}>
            <sphereGeometry args={[0.06, 12, 12]} />
            <meshStandardMaterial color="#e2e8f0" {...CHROME} />
          </mesh>
        </group>
        <mesh ref={coreGlow} position={[0, 0, 0.5]}>
          <sphereGeometry args={[0.11, 16, 16]} />
          <meshBasicMaterial color="#10b981" />
        </mesh>
      </group>

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
    </group>
  );
}

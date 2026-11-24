"use client";

import { useMemo, useRef } from "react";
import type { RefObject } from "react";
import { useFrame } from "@react-three/fiber";
import * as THREE from "three";
import type { Stage } from "./types";

const COUNT = 220;
const EMITTERS: [number, number, number][] = [
  [0.08, -0.28, -0.48],
  [-0.08, -0.28, -0.48],
];
const COLOR = new THREE.Color("#d4d4d8");

const VERT = /* glsl */ `
  attribute float aSize;
  attribute float aAlpha;
  uniform float uSize;
  varying float vAlpha;
  void main() {
    vAlpha = aAlpha;
    vec4 mv = modelViewMatrix * vec4(position, 1.0);
    gl_PointSize = aSize * uSize / max(-mv.z, 0.1);
    gl_Position = projectionMatrix * mv;
  }
`;

const FRAG = /* glsl */ `
  uniform vec3 uColor;
  varying float vAlpha;
  void main() {
    float d = length(gl_PointCoord - 0.5);
    float a = (1.0 - smoothstep(0.05, 0.5, d)) * vAlpha;
    if (a < 0.01) discard;
    gl_FragColor = vec4(uColor, a);
  }
`;

type Props = {
  stage: Stage;
  rig: RefObject<THREE.Group | null>;
};

export function ExhaustSmoke({ stage, rig }: Props) {
  const pointsRef = useRef<THREE.Points | null>(null);

  const positions = useMemo(() => new Float32Array(COUNT * 3), []);
  const sizes = useMemo(() => new Float32Array(COUNT), []);
  const alphas = useMemo(() => new Float32Array(COUNT), []);
  const particles = useMemo(
    () =>
      Array.from({ length: COUNT }, () => ({
        life: 0,
        vel: new THREE.Vector3(),
      })),
    []
  );

  const material = useMemo(
    () =>
      new THREE.ShaderMaterial({
        uniforms: { uColor: { value: COLOR }, uSize: { value: 1 } },
        vertexShader: VERT,
        fragmentShader: FRAG,
        transparent: true,
        depthWrite: false,
      }),
    []
  );

  // Fractional emission budget accumulates across frames so high-refresh
  // displays (tiny dt) still emit particles.
  const budgetRef = useRef(0);

  useFrame((state, delta) => {
    const active = stage === "ride" || stage === "flash";
    const flash = stage === "flash";
    const rigX = rig.current ? rig.current.position.x : 0;
    const rigZ = rig.current ? rig.current.position.z : 0;
    const dt = Math.min(delta, 0.05);
    // In flash the puffs flood the frame: emit much faster across a wide band
    // in front of the camera so the white-out reads as smoke before the DOM
    // overlay fades in behind it.
    if (active) budgetRef.current += dt * (flash ? 180 : 75);
    else budgetRef.current = 0;
    budgetRef.current = Math.min(budgetRef.current, COUNT);

    for (let i = 0; i < COUNT; i++) {
      const p = particles[i];
      const i3 = i * 3;
      if (p.life <= 0) {
        if (budgetRef.current >= 1 && Math.random() < 0.9) {
          budgetRef.current -= 1;
          if (flash) {
            positions[i3] = (Math.random() - 0.5) * 1.4;
            positions[i3 + 1] = -0.35 + Math.random() * 1.5;
            positions[i3 + 2] = (Math.random() - 0.5) * 3.0;
            p.vel.set((Math.random() - 0.5) * 0.1, 0.15 + Math.random() * 0.35, (Math.random() - 0.5) * 0.2);
            p.life = 0.8 + Math.random() * 0.5;
          } else {
            const e = EMITTERS[i % EMITTERS.length];
            positions[i3] = e[0] + rigX + (Math.random() - 0.5) * 0.05;
            positions[i3 + 1] = e[1] + (Math.random() - 0.5) * 0.05;
            positions[i3 + 2] = e[2] + rigZ + (Math.random() - 0.5) * 0.05;
            p.vel.set((Math.random() - 0.5) * 0.1, 0.25 + Math.random() * 0.35, -0.05 - Math.random() * 0.15);
            p.life = 1.1 + Math.random() * 0.5;
          }
        } else {
          positions[i3] = 0;
          positions[i3 + 1] = -100;
          positions[i3 + 2] = 0;
          sizes[i] = 0;
          alphas[i] = 0;
          continue;
        }
      }
      p.life -= dt * 0.5;
      p.vel.y += 0.22 * dt;
      p.vel.multiplyScalar(0.98);
      positions[i3] += p.vel.x * dt;
      positions[i3 + 1] += p.vel.y * dt;
      positions[i3 + 2] += p.vel.z * dt;
      const life = THREE.MathUtils.clamp(p.life, 0, 1);
      alphas[i] = life * life * (flash ? 0.85 : 0.55);
      sizes[i] = 0.1 + (1 - life) * (flash ? 0.9 : 0.5);
    }

    if (pointsRef.current) {
      const geo = pointsRef.current.geometry;
      (geo.attributes.position as THREE.BufferAttribute).needsUpdate = true;
      (geo.attributes.aSize as THREE.BufferAttribute).needsUpdate = true;
      (geo.attributes.aAlpha as THREE.BufferAttribute).needsUpdate = true;
      const px = state.gl.getPixelRatio();
      const h = state.gl.domElement.clientHeight * px;
      const fov = (state.camera as THREE.PerspectiveCamera).fov;
      material.uniforms.uSize.value = h / (2 * Math.tan(THREE.MathUtils.degToRad(fov) / 2));
    }
  });

  return (
    <points ref={pointsRef} frustumCulled={false}>
      <bufferGeometry>
        <bufferAttribute attach="attributes-position" args={[positions, 3]} />
        <bufferAttribute attach="attributes-aSize" args={[sizes, 1]} />
        <bufferAttribute attach="attributes-aAlpha" args={[alphas, 1]} />
      </bufferGeometry>
      <primitive object={material} attach="material" />
    </points>
  );
}

"use client";

import { useRef } from "react";
import type { RefObject } from "react";
import { useFrame } from "@react-three/fiber";
import * as THREE from "three";
import type { Stage } from "./types";

export type VTwinNodes = {
  engineRig: RefObject<THREE.Group | null>;
  bikeRig: RefObject<THREE.Group | null>;
  crank: RefObject<THREE.Group | null>;
  airCleaner: RefObject<THREE.Group | null>;
  pistonL: RefObject<THREE.Group | null>;
  pistonR: RefObject<THREE.Group | null>;
  coreGlow: RefObject<THREE.Mesh | null>;
  coreLight: RefObject<THREE.PointLight | null>;
  wheelF: RefObject<THREE.Object3D | null>;
  wheelR: RefObject<THREE.Object3D | null>;
  flameL: RefObject<THREE.Mesh | null>;
  flameR: RefObject<THREE.Mesh | null>;
  exhaustL: RefObject<THREE.Group | null>;
  exhaustR: RefObject<THREE.Group | null>;
  headlight: RefObject<THREE.PointLight | null>;
  shadow: RefObject<THREE.Group | null>;
};

const tmpVec = new THREE.Vector3();
// Side camera sits on the +x axis, so screen-left = +z. The bike's front
// points +z, so riding +z exits the frame nose-first toward the left. It
// launches from a standstill with constant acceleration (capped), so the
// exit reads as a real take-off instead of an instant shift.
const RIDE_ACCEL = 2.0;
const RIDE_MAX_SPEED = 1.8;

export function useVTwinAnimation(nodes: Partial<VTwinNodes>, stage: Stage) {
  const fadeRef = useRef(0);
  const stageRef = useRef(stage);
  const rideRef = useRef({ speed: 0, time: 0 });

  useFrame((state, delta) => {
    const t = state.clock.elapsedTime;
    const rev = stage === "rev";
    const bike = stage !== "rev";
    const ride = stage === "ride" || stage === "flash";
    const w = rev ? 21 : 5.5;
    const spin = rev ? 16 : 4;

    if (stageRef.current !== stage) {
      if (stage === "ride") rideRef.current = { speed: 0, time: 0 };
      stageRef.current = stage;
    }

    if (nodes.engineRig?.current) {
      const target = rev ? 1 : 0;
      const s = THREE.MathUtils.damp(nodes.engineRig.current.scale.x, target, 4, delta);
      const heartbeat = rev ? 1 + 0.04 * Math.abs(Math.sin(t * w)) : 1;
      nodes.engineRig.current.scale.setScalar(s * heartbeat);
    }

    if (nodes.bikeRig?.current) {
      const rig = nodes.bikeRig.current;
      rig.scale.setScalar(bike ? 1 : 0);

      if (ride) {
        const r = rideRef.current;
        r.time += delta;
        r.speed = Math.min(r.speed + RIDE_ACCEL * delta, RIDE_MAX_SPEED);
        rig.position.z += r.speed * delta;

        // Opacity holds while the bike is visibly rolling, then eases out as
        // it rides off into the smoke (smoothstep, tied to ride time).
        const fadeP = THREE.MathUtils.clamp((r.time - 0.35) / 1.25, 0, 1);
        fadeRef.current = fadeP * fadeP * (3 - 2 * fadeP);
      } else {
        rig.position.z = THREE.MathUtils.damp(rig.position.z, 0, 3, delta);
        fadeRef.current = THREE.MathUtils.damp(fadeRef.current, 0, 2, delta);
      }

      // Fade the bike out (opacity on the GLB materials) and hide the contact
      // shadow plane as it rides off to the left during ride/flash.
      const fade = fadeRef.current;
      if (fade > 0.001) {
        const opacity = 1 - fade;
        if (nodes.shadow?.current) nodes.shadow.current.visible = opacity > 0.05;
        rig.traverse((o) => {
          const mesh = o as THREE.Mesh;
          if (!mesh.isMesh) return;
          const mats = Array.isArray(mesh.material) ? mesh.material : [mesh.material];
          for (const mat of mats) {
            if (mat.type === "MeshBasicMaterial") {
              mat.opacity = 0.35 * opacity;
            } else {
              if (!mat.transparent) mat.transparent = true;
              mat.opacity = opacity;
            }
          }
        });
      }
    }

    const amp = rev ? 0.16 : 0.11;
    if (nodes.pistonL?.current) nodes.pistonL.current.position.y = Math.sin(t * w) * amp;
    if (nodes.pistonR?.current) nodes.pistonR.current.position.y = Math.sin(t * w + Math.PI) * amp;

    if (nodes.crank?.current) nodes.crank.current.rotation.z += delta * spin;
    if (nodes.airCleaner?.current) nodes.airCleaner.current.rotation.z += delta * spin * 1.4;

    // Wheels track the launch: slow during the build, ramping up with speed.
    const wheelSpin = ride ? 2.5 + rideRef.current.speed * 6 : 3;
    if (nodes.wheelF?.current) nodes.wheelF.current.rotation.x += delta * wheelSpin;
    if (nodes.wheelR?.current) nodes.wheelR.current.rotation.x += delta * wheelSpin;

    const glow = rev ? 0.5 + 0.5 * (0.5 + 0.5 * Math.sin(t * w * 2)) : 0;
    if (nodes.coreLight?.current) nodes.coreLight.current.intensity = glow * 4;
    if (nodes.coreGlow?.current) {
      const mat = nodes.coreGlow.current.material as unknown as THREE.MeshStandardMaterial | null;
      if (mat && mat.emissiveIntensity !== undefined) mat.emissiveIntensity = glow * 1.2;
      nodes.coreGlow.current.scale.setScalar(rev ? 1 + 0.2 * Math.abs(Math.sin(t * w * 2)) : 1);
    }

    for (const [flame, exhaust] of [
      [nodes.flameL?.current ?? null, nodes.exhaustL?.current ?? null],
      [nodes.flameR?.current ?? null, nodes.exhaustR?.current ?? null],
    ] as const) {
      if (!flame || !exhaust) continue;
      exhaust.getWorldPosition(tmpVec);
      flame.position.copy(tmpVec);
      flame.visible = rev;
      const mat = flame.material as THREE.MeshBasicMaterial;
      const flick = 0.7 + 0.3 * Math.sin(t * (rev ? 38 : 14));
      mat.opacity = rev ? 0.9 * flick : 0.6 * flick;
    }

    if (nodes.headlight?.current) {
      nodes.headlight.current.intensity = THREE.MathUtils.damp(
        nodes.headlight.current.intensity,
        bike ? 3 : 0,
        3,
        delta
      );
    }
  });
}

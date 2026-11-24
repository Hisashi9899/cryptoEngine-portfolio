"use client";

import { useFrame } from "@react-three/fiber";
import * as THREE from "three";
import type { Stage } from "./types";

type CamTarget = { pos: [number, number, number]; look: [number, number, number] };

const TARGETS: Record<Stage, CamTarget> = {
  rev: { pos: [0, 0.7, 3.6], look: [0, 0.1, 0] },
  turn: { pos: [1.5, 0.7, 0], look: [0, 0, 0] },
  ride: { pos: [1.5, 0.7, 0], look: [0, 0, 0] },
  flash: { pos: [1.5, 0.7, 0], look: [0, 0, 0] },
};

export function CameraRig({ stage }: { stage: Stage }) {
  useFrame((state, delta) => {
    const target = TARGETS[stage];
    const { camera } = state;
    camera.position.x = THREE.MathUtils.damp(camera.position.x, target.pos[0], 3, delta);
    camera.position.y = THREE.MathUtils.damp(camera.position.y, target.pos[1], 3, delta);
    camera.position.z = THREE.MathUtils.damp(camera.position.z, target.pos[2], 3, delta);
    camera.lookAt(target.look[0], target.look[1], target.look[2]);
  });
  return null;
}

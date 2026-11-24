"use client";

import { useEffect, useRef } from "react";
import { useFrame } from "@react-three/fiber";
import { useGLTF, ContactShadows } from "@react-three/drei";
import * as THREE from "three";
import { useVTwinAnimation } from "./useVTwinAnimation";
import { ExhaustSmoke } from "./ExhaustSmoke";
import type { Stage } from "./types";

import { asset } from "@/lib/asset";

const MODEL_PATH = asset("/models/bike.glb");

type GLTFData = { scene: THREE.Group; nodes: Record<string, THREE.Object3D> };

// The GLB scene root (Sketchfab_model) carries a -90 deg pitch that lays the
// model flat: world width = X, height = Y, length = Z (front = -Z, rear = +Z,
// exhausts at +Z). We yaw it 180 deg so the front points +Z, then apply a
// uniform 0.6 scale + centering offset baked into the fit matrix. Final
// bounds: width 0.548, height 0.876 (ground -0.438), length 1.537.
//
// Sequence: the engine (rev) builds the whole bike outward from the engine
// pivot during "turn" (per-part radial assembly), then the finished bike holds
// the big side view and slides left while fading, with white smoke trailing
// from the rear exhausts (ExhaustSmoke).
const FIT_ROTATION: [number, number, number] = [0, Math.PI, 0];
const FIT_OFFSET: [number, number, number] = [0, -0.1445, 0.0777];
const FIT_SCALE: [number, number, number] = [0.6, 0.6, 0.6];

const HEADLIGHT_POS: [number, number, number] = [0, 0.8, -1.05];
const GROUND_Y = -0.438;

// Build pivot in mesh-local space (bike length along Y, height along Z): the
// engine block area. All parts expand radially from here when "turn" starts.
const BUILD_PIVOT = new THREE.Vector3(0, 0.25, 0.15);
const BUILD_STEP = 0.95;
const BUILD_STAGGER = 0.45;

useGLTF.preload(MODEL_PATH);

function meshCenter(mesh: THREE.Mesh): THREE.Vector3 {
  const c = new THREE.Vector3();
  if (mesh.geometry.boundingBox) mesh.geometry.boundingBox.getCenter(c);
  return c;
}

// The tires/rims are merged into the body geometry, but the front and rear
// brake discs are standalone round, thin meshes sitting at the wheel axles.
// Find them by shape (two near-equal large dims, thin third dim), preferring
// disc-like material names, and pick the two most separated along the bike's
// forward axis (raw model front = -Z, yawed to +Z). Both wheels spin about X
// at the same rate, so front/rear order is purely cosmetic.
function findWheelDiscs(scene: THREE.Object3D): THREE.Mesh[] {
  const candidates: THREE.Mesh[] = [];
  scene.traverse((o) => {
    const mesh = o as THREE.Mesh;
    if (!mesh.isMesh || !mesh.geometry) return;
    mesh.geometry.computeBoundingBox();
    const box = mesh.geometry.boundingBox;
    if (!box) return;
    const size = new THREE.Vector3();
    box.getSize(size);
    const dims = [size.x, size.y, size.z].sort((a, b) => a - b);
    const [a, b, c] = dims;
    if (b > 0.22 && c / b > 0.85 && c / b < 1.08 && a < 0.22) candidates.push(mesh);
  });
  const matName = (m: THREE.Mesh) =>
    ((Array.isArray(m.material) ? m.material[0] : m.material) as THREE.Material | undefined)
      ?.name.toLowerCase() ?? "";
  const named = candidates.filter((m) => /diavel|disc|disk|brake|rotor/.test(matName(m)));
  const pool = named.length >= 2 ? named : candidates;
  if (pool.length < 2) return [];
  const cz = (m: THREE.Mesh) => meshCenter(m).z;
  const sorted = [...pool].sort((a, b) => cz(b) - cz(a));
  return [sorted[0], sorted[sorted.length - 1]];
}

export function Bike3D({ stage }: { stage: Stage }) {
  const { scene } = useGLTF(MODEL_PATH) as unknown as GLTFData;

  const bikeRig = useRef<THREE.Group | null>(null);
  const wheelF = useRef<THREE.Object3D | null>(null);
  const wheelR = useRef<THREE.Object3D | null>(null);
  const headlight = useRef<THREE.PointLight | null>(null);
  const shadow = useRef<THREE.Group | null>(null);
  const buildRef = useRef<{ start: number | null; maxDist: number }>({ start: null, maxDist: 1 });

  useEffect(() => {
    const discs = findWheelDiscs(scene);
    if (discs.length === 2) {
      for (const mesh of discs) {
        const c = meshCenter(mesh);
        mesh.geometry = mesh.geometry.clone();
        mesh.geometry.translate(-c.x, -c.y, -c.z);
        mesh.position.copy(c);
      }
      wheelF.current = discs[0];
      wheelR.current = discs[1];
    }

    // Record each part's rest transform and a stagger delay based on how far
    // it sits from the engine pivot, so the build reads as an outward wave.
    const parts: THREE.Mesh[] = [];
    const centers: THREE.Vector3[] = [];
    scene.traverse((o) => {
      const mesh = o as THREE.Mesh;
      if (!mesh.isMesh || !mesh.geometry) return;
      mesh.userData.buildR = mesh.position.clone();
      mesh.geometry.computeBoundingBox();
      const geomC = new THREE.Vector3();
      if (mesh.geometry.boundingBox) mesh.geometry.boundingBox.getCenter(geomC);
      const center = mesh.position.lengthSq() > 0 ? mesh.position : geomC;
      parts.push(mesh);
      centers.push(center);
    });
    let maxDist = 0;
    for (const c of centers) maxDist = Math.max(maxDist, c.distanceTo(BUILD_PIVOT));
    buildRef.current.maxDist = maxDist || 1;
    for (let i = 0; i < parts.length; i++) {
      parts[i].userData.buildDelay =
        (centers[i].distanceTo(BUILD_PIVOT) / buildRef.current.maxDist) * BUILD_STAGGER;
    }
  }, [scene]);

  // "turn" = the bike assembles around the engine pivot: each part expands
  // from BUILD_PIVOT to its rest position, staggered by distance.
  useFrame((state) => {
    if (stage === "turn") {
      if (buildRef.current.start === null) buildRef.current.start = state.clock.elapsedTime;
      const el = state.clock.elapsedTime - buildRef.current.start;
      scene.traverse((o) => {
        const mesh = o as THREE.Mesh;
        if (!mesh.isMesh || mesh.userData.buildR === undefined) return;
        const delay = (mesh.userData.buildDelay as number) ?? 0;
        const p = THREE.MathUtils.clamp((el - delay) / BUILD_STEP, 0, 1);
        const s = 1 - Math.pow(1 - p, 3);
        mesh.position.lerpVectors(BUILD_PIVOT, mesh.userData.buildR as THREE.Vector3, s);
        mesh.scale.setScalar(Math.max(s, 1e-4));
      });
    } else if (buildRef.current.start !== null) {
      buildRef.current.start = null;
      scene.traverse((o) => {
        const mesh = o as THREE.Mesh;
        if (!mesh.isMesh || mesh.userData.buildR === undefined) return;
        mesh.position.copy(mesh.userData.buildR as THREE.Vector3);
        mesh.scale.setScalar(1);
      });
    }
  });

  useVTwinAnimation({ bikeRig, wheelF, wheelR, headlight, shadow }, stage);

  return (
    <group visible={stage !== "rev"}>
      <group ref={bikeRig}>
        <group position={FIT_OFFSET} rotation={FIT_ROTATION} scale={FIT_SCALE}>
          <primitive object={scene} />
          <pointLight ref={headlight} position={HEADLIGHT_POS} color="#fff2cc" distance={6} intensity={0} />
        </group>
        <group ref={shadow}>
          <ContactShadows position={[0, GROUND_Y, 0]} scale={2.6} blur={2.4} opacity={0.35} far={1.6} />
        </group>
      </group>
      <ExhaustSmoke stage={stage} rig={bikeRig} />
    </group>
  );
}

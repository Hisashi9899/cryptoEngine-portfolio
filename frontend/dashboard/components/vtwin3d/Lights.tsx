"use client";

export function Lights() {
  return (
    <>
      <ambientLight intensity={0.55} />
      <hemisphereLight args={["#334155", "#020617", 0.35]} />
      <directionalLight position={[3, 4, 5]} intensity={1.6} />
      <directionalLight position={[-4, 2, -3]} intensity={0.65} color="#64748b" />
      {/* ショールームの柔光：車体前面を見せるための弱い正面光。夜の雰囲気は維持 */}
      <directionalLight position={[0.5, 2.5, 6]} intensity={0.55} color="#d7f3e5" />
    </>
  );
}

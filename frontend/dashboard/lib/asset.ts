/**
 * Prefix a public asset path with the Next.js basePath.
 * GitHub Pages serves the demo under /cryptoEngine-portfolio, so raw
 * <img>/GLTF paths must include the same prefix as Next's own assets.
 */
export const BASE_PATH = process.env.NEXT_PUBLIC_BASE_PATH || "";

export function asset(path: string): string {
  if (!path.startsWith("/")) return path;
  return `${BASE_PATH}${path}`;
}

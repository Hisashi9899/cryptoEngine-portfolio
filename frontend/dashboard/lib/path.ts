/** Normalize a Next.js pathname for basePath and trailingSlash deployments. */
export function normalizePath(pathname: string): string {
  const base = process.env.NEXT_PUBLIC_BASE_PATH || "";
  let path = pathname || "/";
  if (base && path.startsWith(base)) {
    path = path.slice(base.length) || "/";
  }
  if (path.length > 1 && path.endsWith("/")) {
    path = path.slice(0, -1);
  }
  return path || "/";
}

/** Absolute login URL that respects a configured basePath. */
export function loginHref(): string {
  const base = process.env.NEXT_PUBLIC_BASE_PATH || "";
  return base ? `${base}/login/` : "/login";
}

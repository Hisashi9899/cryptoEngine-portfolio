"use client";

/**
 * HTTP + 認証クライアント。
 *
 * - JWT access / refresh token を localStorage に保持
 * - 全 API 呼び出しに Authorization: Bearer <access> を付与
 * - 401 のとき refresh token で自動再取得（単一 flight）して 1 回リトライ
 * - refresh に失敗したら token を破棄して /login へ遷移
 */

export const API_BASE = process.env.NEXT_PUBLIC_API_URL ?? "";

const ACCESS_KEY = "cryptoengine_access_token";
const REFRESH_KEY = "cryptoengine_refresh_token";

function storage(): Storage | null {
  return typeof window !== "undefined" ? window.localStorage : null;
}

export function getAccessToken(): string | null {
  return storage()?.getItem(ACCESS_KEY) ?? null;
}

export function getRefreshToken(): string | null {
  return storage()?.getItem(REFRESH_KEY) ?? null;
}

export function setTokens(access: string, refresh: string): void {
  const s = storage();
  if (!s) return;
  s.setItem(ACCESS_KEY, access);
  s.setItem(REFRESH_KEY, refresh);
}

export function clearTokens(): void {
  const s = storage();
  if (!s) return;
  s.removeItem(ACCESS_KEY);
  s.removeItem(REFRESH_KEY);
}

function decodeExp(token: string): number | null {
  try {
    const parts = token.split(".");
    if (parts.length < 2) return null;
    const b64 = parts[1].replace(/-/g, "+").replace(/_/g, "/");
    const payload = JSON.parse(atob(b64));
    return typeof payload.exp === "number" ? payload.exp : null;
  } catch {
    return null;
  }
}

export function isLoggedIn(): boolean {
  const token = getAccessToken();
  if (!token) return false;
  const exp = decodeExp(token);
  return exp === null || exp * 1000 > Date.now();
}

function toLogin(): void {
  if (
    typeof window !== "undefined" &&
    window.location.pathname !== "/login"
  ) {
    window.location.href = "/login";
  }
}

let refreshing: Promise<boolean> | null = null;

/** refresh token で access token を再取得する。成功時 true。単一 flight。 */
export function refreshAccessToken(): Promise<boolean> {
  if (refreshing) return refreshing;
  refreshing = (async () => {
    const refresh = getRefreshToken();
    if (!refresh) {
      clearTokens();
      return false;
    }
    try {
      const res = await fetch(`${API_BASE}/api/v1/auth/refresh`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ refresh_token: refresh }),
        cache: "no-store",
      });
      if (!res.ok) {
        clearTokens();
        return false;
      }
      const data = await res.json();
      setTokens(data.access_token, data.refresh_token);
      return true;
    } catch {
      clearTokens();
      return false;
    }
  })().finally(() => {
    refreshing = null;
  });
  return refreshing;
}

/** username + password でログインし、token を保存する。 */
export async function login(username: string, password: string): Promise<void> {
  const res = await fetch(`${API_BASE}/api/v1/auth/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password }),
    cache: "no-store",
  });
  if (!res.ok) {
    const detail = await res.text().catch(() => "");
    throw new ApiError(res.status, detail || "login failed");
  }
  const data = await res.json();
  setTokens(data.access_token, data.refresh_token);
}

/** ログアウト。サーバー通知はベストエフォートで、必ずローカル token を破棄する。 */
export async function logout(): Promise<void> {
  const token = getAccessToken();
  const refresh = getRefreshToken();
  if (token) {
    try {
      await fetch(`${API_BASE}/api/v1/auth/logout`, {
        method: "POST",
        headers: {
          Authorization: `Bearer ${token}`,
          "Content-Type": "application/json",
        },
        body: JSON.stringify({ refresh_token: refresh }),
        cache: "no-store",
      });
    } catch {
      // ignore
    }
  }
  clearTokens();
  toLogin();
}

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

function withAuthHeaders(init: RequestInit): RequestInit {
  const headers = new Headers(init.headers);
  const token = getAccessToken();
  if (token) headers.set("Authorization", `Bearer ${token}`);
  return { ...init, headers, cache: init.cache ?? "no-store" };
}

export async function fetchWithAuth(
  path: string,
  init: RequestInit = {}
): Promise<Response> {
  let res = await fetch(`${API_BASE}${path}`, withAuthHeaders(init));
  if (res.status === 401) {
    const ok = await refreshAccessToken();
    if (ok) {
      res = await fetch(`${API_BASE}${path}`, withAuthHeaders(init));
    } else {
      toLogin();
    }
  }
  return res;
}

export async function httpGet<T>(path: string): Promise<T> {
  const res = await fetchWithAuth(path);
  if (!res.ok) {
    throw new ApiError(res.status, `API error (${res.status}): ${path}`);
  }
  return res.json() as Promise<T>;
}

export async function httpPost<T>(path: string, body?: unknown): Promise<T> {
  const init: RequestInit = { method: "POST" };
  if (body !== undefined) {
    init.headers = { "Content-Type": "application/json" };
    init.body = JSON.stringify(body);
  }
  const res = await fetchWithAuth(path, init);
  if (!res.ok) {
    const detail = await res.text();
    throw new ApiError(res.status, `API error (${res.status}): ${path}: ${detail}`);
  }
  return res.json() as Promise<T>;
}

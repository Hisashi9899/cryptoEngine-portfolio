"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { Eye, EyeOff, Loader2, LogIn } from "lucide-react";
import { ApiError, isLoggedIn, login } from "@/lib/http";
import LoginSuccessOverlay from "@/components/vtwin/LoginSuccessOverlay";

export default function LoginPage() {
  const router = useRouter();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [igniting, setIgniting] = useState(false);
  const [showPassword, setShowPassword] = useState(false);

  useEffect(() => {
    if (isLoggedIn()) router.replace("/");
  }, [router]);

  // NOTE: VTwinScene（three.js）の先読みは行わない。
  // iPhoneでは1MB超のJS解析が入力中のカクツキ原因になるため、
  // 成功オーバーレイ表示時のdynamic importに任せる。

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      await login(username, password);
      setIgniting(true);
    } catch (err) {
      setError(
        err instanceof ApiError && err.status === 401
          ? "ユーザー名またはパスワードが正しくありません。"
          : "ログインに失敗しました。しばらくしてから再試行してください。"
      );
      setBusy(false);
    }
  };

  return (
    <div className="relative grid min-h-screen place-items-center overflow-hidden px-4">
      <div className="pointer-events-none absolute inset-0">
        <div
          className="absolute inset-0"
          style={{
            background:
              "linear-gradient(115deg, rgba(226,232,240,0.07) 0%, transparent 22%, rgba(226,232,240,0.05) 40%, transparent 58%, rgba(226,232,240,0.06) 78%, transparent 100%), radial-gradient(900px 480px at 50% -5%, rgba(16,185,129,0.14), transparent 62%), radial-gradient(700px 400px at 100% 100%, rgba(59,130,246,0.08), transparent 55%), linear-gradient(165deg, #0d1526 0%, #070b16 45%, #04060d 100%)",
          }}
        />
        <div className="absolute -top-40 left-1/2 h-[480px] w-[720px] -translate-x-1/2 rounded-full bg-emerald-500/10 blur-3xl" />
        <div className="absolute bottom-0 right-0 h-[360px] w-[360px] rounded-full bg-sky-500/10 blur-3xl" />
        <div className="absolute bottom-0 left-0 h-[300px] w-[300px] rounded-full bg-slate-500/10 blur-3xl" />
      </div>

      <div className="relative z-10 w-full max-w-md">
        <div className="ce-form-in mb-6 flex justify-center">
          <div className="w-full max-w-sm">
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img
              src="/logo/logo-animated.svg"
              alt="CryptoEngine"
              className="w-full"
              width={520}
              height={360}
            />
          </div>
        </div>

        <form
          onSubmit={onSubmit}
          className="ce-form-in rounded-card border border-white/10 bg-navy-900/70 p-8 shadow-2xl backdrop-blur-xl"
        >
          <h2 className="text-lg font-semibold text-slate-100">ログイン</h2>
          <p className="mt-1 text-xs text-slate-500">
            CryptoEngine の監視ダッシュボードへアクセスするには認証が必要です
          </p>

          <div className="mt-6 space-y-4">
            <label className="block">
              <span className="text-xs font-medium text-slate-400">
                Username（ユーザー名）
              </span>
              <input
                type="text"
                required
                autoComplete="username"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                placeholder="username"
                className="mt-1.5 w-full rounded-lg border border-white/10 bg-navy-950/70 px-3 py-2.5 text-sm text-slate-100 placeholder:text-slate-600 focus:border-emerald-400/50 focus:outline-none focus:ring-1 focus:ring-emerald-400/30"
              />
            </label>

            <label className="block">
              <span className="text-xs font-medium text-slate-400">
                Password（パスワード）
              </span>
              <div className="relative mt-1.5">
                <input
                  type={showPassword ? "text" : "password"}
                  required
                  autoComplete="current-password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder="••••••••"
                  className="w-full rounded-lg border border-white/10 bg-navy-950/70 px-3 py-2.5 pr-10 text-sm text-slate-100 placeholder:text-slate-600 focus:border-emerald-400/50 focus:outline-none focus:ring-1 focus:ring-emerald-400/30"
                />
                <button
                  type="button"
                  onClick={() => setShowPassword((v) => !v)}
                  aria-label={showPassword ? "パスワードを隠す" : "パスワードを表示"}
                  className="absolute inset-y-0 right-0 flex items-center pr-3 text-slate-500 hover:text-slate-300"
                >
                  {showPassword ? (
                    <EyeOff className="h-4 w-4" />
                  ) : (
                    <Eye className="h-4 w-4" />
                  )}
                </button>
              </div>
            </label>
          </div>

          {error && (
            <p
              role="alert"
              className="ce-error-in mt-2 text-sm font-medium text-red-400"
            >
              {error}
            </p>
          )}

          <button
            type="submit"
            disabled={busy}
            className="mt-6 inline-flex w-full items-center justify-center gap-2 rounded-lg bg-emerald-500 px-4 py-2.5 text-sm font-semibold text-navy-950 transition-colors hover:bg-emerald-400 disabled:cursor-not-allowed disabled:opacity-40"
          >
            {busy ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <LogIn className="h-4 w-4" />
            )}
            {busy ? "イグニッション..." : "ログイン"}
          </button>
        </form>

        <p className="ce-form-in mt-6 text-center text-[11px] text-slate-600">
          © {new Date().getFullYear()} CryptoEngine · Auto Trading Engine
        </p>
      </div>

      {igniting && <LoginSuccessOverlay />}
    </div>
  );
}

/** @type {import('next').NextConfig} */
const demo = process.env.NEXT_PUBLIC_DEMO_MODE === "1";
const basePath = process.env.NEXT_PUBLIC_BASE_PATH || "";

const nextConfig = {
  reactStrictMode: true,
  // next/image を使用していないため画像最適化 API（/_next/image）を無効化する。
  // Image Optimizer 経由の DoS アドバイザリ（GHSA-9g9p-9gw9-jx7f 等）への対策。
  images: { unoptimized: true },

  // Static GitHub Pages demo: export a purely client-side build with demo data.
  output: demo ? "export" : undefined,
  trailingSlash: demo ? true : undefined,
  basePath: demo && basePath ? basePath : undefined,
  assetPrefix: demo && basePath ? basePath : undefined,

  async rewrites() {
    if (demo) return [];
    return [
      {
        source: "/api/:path*",
        destination: `${process.env.API_PROXY_TARGET ?? "http://localhost:8000"}/api/:path*`,
      },
    ];
  },
};

export default nextConfig;

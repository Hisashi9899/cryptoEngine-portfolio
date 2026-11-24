/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  // next/image を使用していないため画像最適化 API（/_next/image）を無効化する。
  // Image Optimizer 経由の DoS アドバイザリ（GHSA-9g9p-9gw9-jx7f 等）への対策。
  images: { unoptimized: true },
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: `${process.env.API_PROXY_TARGET ?? "http://localhost:8000"}/api/:path*`,
      },
    ];
  },
};

export default nextConfig;

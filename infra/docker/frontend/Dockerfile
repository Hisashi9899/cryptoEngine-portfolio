# CryptoEngine Dashboard (Next.js) - 本番コンテナ
#
# npm run build で本番ビルドし、next start（ポート 3000）で待受する。
# ランタイムは devDependencies を除外した node_modules のみを持つ軽量イメージ。
# node_modules はビルド時に npm ci でインストールする（ホストからはコピーしない）。

FROM node:20-alpine AS deps
WORKDIR /app
COPY frontend/dashboard/package.json frontend/dashboard/package-lock.json ./
RUN npm ci

FROM node:20-alpine AS builder
WORKDIR /app
# next build は rewrites をビルド時に routes-manifest.json へ焼き込むため、
# ランタイム env（API_PROXY_TARGET）では書き換わらない。ビルド時に注入する。
# 既定は compose の api サービス名（:8000）。
ARG API_PROXY_TARGET=http://api:8000
ENV API_PROXY_TARGET=${API_PROXY_TARGET}
ENV NEXT_TELEMETRY_DISABLED=1
COPY --from=deps /app/node_modules ./node_modules
COPY frontend/dashboard/ ./
RUN npm run build

FROM node:20-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
ENV HOSTNAME=0.0.0.0
COPY frontend/dashboard/package.json frontend/dashboard/package-lock.json ./
RUN npm ci --omit=dev && npm cache clean --force
COPY --from=builder /app/.next ./.next
COPY --from=builder /app/public ./public
COPY --from=builder /app/next.config.mjs ./next.config.mjs
EXPOSE 3000
CMD ["node_modules/.bin/next", "start", "-p", "3000"]

#!/usr/bin/env bash
# 构建前端静态产物后，用 wails dev 直接 serve dist 目录启动。
# 绕开 Vite dev server，因此没有任何前端热更新；后端 Go 重建也已禁用。
#
#   - npm run build:wails  → 生成 frontend/dist
#   - -assetdir frontend/dist → wails 直接 serve 静态产物，不启动 Vite
#   - -noreload            → 关闭 wails 对 asset 变化的重载
#   - -nogorebuild         → 关闭后端 .go 文件变化的自动重建
#
# 改了前端代码需要重新跑本脚本（或单独 npm run build:wails）才会生效。
set -euo pipefail

# 切到 desktop 目录（脚本所在目录的上一级）
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

echo "==> 构建前端 (npm run build:wails)"
(cd frontend && npm run build:wails)

echo "==> 启动 wails dev（无热更新，serve frontend/dist）"
exec wails dev \
  -noreload \
  -nogorebuild \
  -tags webkit2_41 \
  -assetdir frontend/dist

#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

mkdir -p _site/img _site/en/img
cp -R site/. _site/
mkdir -p _site/api
cp docs/api/openapi.json _site/api/openapi.json
python3 scripts/render_api_docs.py _site
cp docs/screenshots/ru/dashboard.png docs/screenshots/ru/key-link-dialog.png _site/img/
cp docs/screenshots/dashboard.png docs/screenshots/key-link-dialog.png _site/en/img/
python3 scripts/check-site.py _site

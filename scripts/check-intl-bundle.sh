#!/usr/bin/env bash
# Проверка intl-бандла (mezmo-song.com) перед выкладкой: в нём не должно быть
# российской аналитики и ссылок на mezmo.ru, а посадочная должна оставаться
# закрытой от индексации. Запуск: scripts/check-intl-bundle.sh frontend/dist-intl
set -euo pipefail

DIR="${1:?usage: $0 <dist-intl dir>}"
fail=0

# Маркеры .ru: ID GTM, счётчики Метрики и Top.Mail.Ru, загрузчики их скриптов, домен.
forbidden=(
  'GTM-N9THMZKK'
  '110968609'
  '3781086'
  'googletagmanager.com/gtm.js'
  'mc.yandex.ru/metrika'
  'top-fwz1.mail.ru'
  'mezmo.ru'
)
for m in "${forbidden[@]}"; do
  if hits=$(grep -rlF -- "$m" "$DIR"); then
    echo "FAIL: в бандле найден «${m}»:" >&2
    echo "$hits" | sed 's/^/  /' >&2
    fail=1
  fi
done

# Обязательные признаки intl-сборки.
html="$DIR/index.html"
grep -qF 'content="noindex, nofollow"' "$html" || { echo "FAIL: в index.html нет noindex" >&2; fail=1; }
grep -qF 'href="https://mezmo-song.com/"' "$html" || { echo "FAIL: canonical не на mezmo-song.com" >&2; fail=1; }
grep -qF 'window.__SETTINGS__=' "$html" || { echo "FAIL: не заинлайнен снимок настроек (SPA упадёт в classic)" >&2; fail=1; }
grep -qxF 'Disallow: /' "$DIR/robots.txt" 2>/dev/null || { echo "FAIL: robots.txt не закрывает сайт" >&2; fail=1; }
[ ! -e "$DIR/sitemap.xml" ] || { echo "FAIL: в бандле есть sitemap.xml" >&2; fail=1; }

[ "$fail" -eq 0 ] && echo "intl-бандл чистый"
exit "$fail"

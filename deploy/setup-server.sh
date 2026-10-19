#!/usr/bin/env bash
# Разовая настройка 109.248.42.189 под Go-бэкенд mezmo-song.com. Запуск от root
# из каталога с файлами deploy/:  bash setup-server.sh "<публичный ключ CI для бэкенда>"
#
# Идемпотентен: повторный запуск ничего не ломает. .env не создаёт — его кладут
# отдельно (секреты в репозитории не живут), без него сервис не стартует.
set -euo pipefail
cd "$(dirname "$0")"

API_PUBKEY=${1:?usage: setup-server.sh "<ssh-ed25519 … ci-api>"}
ROOT=/opt/mezmo-com
SITE=/etc/nginx/sites-available/mezmo-song.com

id mezmo-com >/dev/null 2>&1 || useradd --system --home-dir "$ROOT" --shell /usr/sbin/nologin mezmo-com
install -d -o root -g root -m 0755 "$ROOT" "$ROOT/bin"
install -d -o mezmo-com -g mezmo-com -m 0750 "$ROOT/data"
install -d -o deploy-intl -g deploy-intl -m 0755 "$ROOT/incoming"

install -m 0755 install-release.sh "$ROOT/install-release.sh"
install -m 0644 mezmo-com.service mezmo-com-install.service mezmo-com-install.path /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now mezmo-com-install.path
systemctl enable mezmo-com.service

# Второй ключ CI: тот же пользователь deploy-intl, но rrsync только в incoming/.
AK=/home/deploy-intl/.ssh/authorized_keys
LINE="restrict,command=\"/usr/bin/rrsync $ROOT/incoming\" $API_PUBKEY"
grep -qF "$API_PUBKEY" "$AK" || echo "$LINE" >> "$AK"

# nginx: /api, /admin и /health — на Go. Вставка один раз, перед `location / {`
# server-блока mezmo-song.com на 443 (там, где root /var/www/mezmo-song.com).
install -m 0644 nginx-api.conf /etc/nginx/snippets/mezmo-com-api.conf
if ! grep -q "mezmo-com-api.conf" "$SITE"; then
  cp -p "$SITE" "/root/mezmo-song.com.nginx.bak-$(date +%Y%m%d%H%M%S)"
  awk '
    /root \/var\/www\/mezmo-song.com;/ { inblock=1 }
    inblock && /location \/ \{/ && !done { print "    include /etc/nginx/snippets/mezmo-com-api.conf;\n"; done=1 }
    { print }
  ' "$SITE" > "$SITE.new" && mv "$SITE.new" "$SITE"
fi
nginx -t
systemctl reload nginx

echo "готово. Дальше: $ROOT/.env (chmod 600), затем первый деплой из CI."

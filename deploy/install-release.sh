#!/usr/bin/env bash
# Ставит релиз, который CI залил в /opt/mezmo-com/incoming, и перезапускает API.
# Запускается systemd (mezmo-com-install.path → .service) от root.
#
# Прошлый бинарь остаётся как bin/server.prev — ручной откат:
#   mv /opt/mezmo-com/bin/server.prev /opt/mezmo-com/bin/server && systemctl restart mezmo-com
set -euo pipefail

ROOT=/opt/mezmo-com
IN=$ROOT/incoming

[ -f "$IN/READY" ] || exit 0
[ -s "$IN/server" ] || { echo "install: в incoming нет бинаря" >&2; exit 1; }

install -m 0755 -o root -g root "$IN/server" "$ROOT/bin/server.new"
if [ -f "$ROOT/bin/server" ]; then
  cp -p "$ROOT/bin/server" "$ROOT/bin/server.prev"
fi
mv "$ROOT/bin/server.new" "$ROOT/bin/server"

# Шаблоны промптов читаются с диска (data/prompts), их можно править без пересборки.
if [ -d "$IN/prompts" ]; then
  mkdir -p "$ROOT/data/prompts"
  cp -r "$IN/prompts/." "$ROOT/data/prompts/"
  chown -R mezmo-com:mezmo-com "$ROOT/data/prompts"
fi

rm -f "$IN/READY"
systemctl restart mezmo-com
echo "install: релиз $(cat "$IN/BUILD" 2>/dev/null || echo '?') установлен"

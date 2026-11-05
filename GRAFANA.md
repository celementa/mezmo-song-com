# Grafana: валюта и провайдер

Дашборд читает SQLite приложения напрямую. В .com у заказа есть три поля,
без которых рубли и доллары смешиваются:

| Поле | Значение |
|---|---|
| `orders.currency` | `RUB` или `USD`; пусто у старых заказов — валюта сайта |
| `orders.payment_provider` | `yookassa`, `stripe`, `unitpay` (кто выставил счёт) |
| `orders.payment_branch` | `ru` или `intl` (что выбрал покупатель) |

## Обязательное правило

Любая панель с суммой группируется по `currency`. Нельзя делать
`SUM(base_price)` по всем заказам: 750 ₽ и $19 — разные деньги.

Пример выручки по дням и валютам:

```sql
SELECT substr(created_at, 1, 10) AS day,
       currency,
       payment_provider,
       COUNT(*) AS orders,
       SUM(base_price - COALESCE(discount_amount, 0)) AS revenue
FROM orders
WHERE payment_status = 'paid'
GROUP BY day, currency, payment_provider
ORDER BY day DESC;
```

## Панели, которые нужно поправить

- «Выручка по дням» — добавить `currency` в разбивку и в подпись.
- «Оплаченные заказы по плечам» — считать количество, а не сумму; если сумма,
  то отдельными панелями RUB и USD.
- «Воронка по шагам» — фильтр `payment_branch` (`ru`/`intl`) для доли веток.
- «Провайдеры» — `payment_provider`, с явной подписью валюты.

Дашборд живёт в репозитории ai-songs (`grafana/dashboards/ab-flows.json`) и
читает БД по `GRAFANA_DB_PATH`. Для .com нужно либо направить его на
`/opt/mezmo-com/data/mezmo.db`, либо поднять отдельный инстанс; SQL-панели
выше переносятся без изменений.

## .com: как поднято на сервере

На `109.248.42.189` установлены Grafana OSS 11.2 и плагин
`frser-sqlite-datasource`. Grafana слушает только loopback:

```
127.0.0.1:3000
```

Порт Grafana остаётся на loopback, но снаружи она опубликована через nginx:

```
https://mezmo-song.com/grafana/
```

Вход — форма Grafana: пользователь `admin`, пароль в
`/root/grafana-admin.txt` на сервере (права 600). Сниппет nginx —
`deploy/nginx-grafana.conf`.

Альтернатива без публикации — SSH-туннель:

```bash
ssh -N -L 3300:127.0.0.1:3000 root@109.248.42.189
```

`http://localhost:3300`, логин `admin`, пароль в `/root/grafana-admin.txt`.

Дашборд: **Mezmo .com — заказы и валюты** (`uid=mezmo-com`). Панели:

- оплаченные заказы с группировкой `currency × payment_provider × payment_branch`;
- заказы/оплаты/выручка по дням — отдельные строки для ₽ и $;
- последние заказы с валютой, провайдером, веткой, планом и плечом.

Источник — боевая `/opt/mezmo-com/data/mezmo.db`. Пользователь `grafana`
добавлен в группу `mezmo-com`, БД читается только на чтение.

Файлы в репозитории:

```
grafana/provisioning/datasources/mezmo-com.yml
grafana/provisioning/dashboards/mezmo-com.yml
grafana/dashboards/mezmo-com.json
```

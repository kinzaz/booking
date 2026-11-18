# Неделя 5. Docker Compose, том PostgreSQL и миграции

## Локальный стенд

`WEB_PORT=18081 docker compose -f deploy/compose.yaml up -d --build` поднимает PostgreSQL, API, worker и фронтенд на Nginx. Nginx отдаёт фронтенд и проксирует запросы к API. Готовность проверяли через `GET /ready` на порту 18081.

## Пересоздание PostgreSQL

Создали бронь на `night-jazz` через API. Команда `docker compose -f deploy/compose.yaml up -d --no-deps --force-recreate --wait postgres` пересоздаёт контейнер PostgreSQL и подключает тот же именованный том `tickets-data`.

Отмена вернула бронь `9fa009cb-3346-44b4-8dd0-b9f2ff549b0b` со статусом `cancelled`. Для подтверждения пересоздания сравнивают вывод `docker compose -f deploy/compose.yaml ps -q postgres` до и после. Эти два ID в конспекте не зафиксированы.

## Миграция 002

[Миграция](../../app/internal/booking/migrations/002_add_event_organizer.sql) добавляет nullable-столбец `events.organizer_name` типа `text`. API применяет миграции при старте и отмечает их в таблице `schema_migrations`.

- В отдельном проекте `tickets-week5-fresh` API стартовал с пустым томом. Проверили наличие `001_init.sql` и `002_add_event_organizer.sql` в истории и нового столбца в `information_schema.columns`.
- В проекте `tickets-week5-upgrade` собрали API из временной копии с одной миграцией `001` и создали бронь. Затем пересобрали API из рабочего дерева с `002`, сохранив PostgreSQL в том же проекте. После обновления проверили историю миграций, новый столбец и чтение брони по прежнему ID. Nginx перезапустили после пересоздания API, чтобы он заново разрешил имя сервиса.

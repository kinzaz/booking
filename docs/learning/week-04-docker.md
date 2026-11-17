# Неделя 4. Образ и контейнер API

## Сборка образа

Из корня проекта:

```bash
docker build --target api -t tickets-api:week4 ./app
docker image inspect tickets-api:week4 \
  --format 'user={{.Config.User}} entrypoint={{json .Config.Entrypoint}}'
```

В [Dockerfile](../../app/Dockerfile) стадия `build` компилирует Go-бинарники, а стадия `api` копирует бинарник API в образ на основе Alpine. `USER 65532:65532` задаёт непривилегированные UID и GID процесса. `ENTRYPOINT` в форме массива запускает API напрямую, поэтому процесс может получать сигналы Docker.

## Запуск вручную

- **Образ** содержит файлы и настройки запуска. Из одного образа можно создать несколько контейнеров.
- **Контейнер** получает собственный записываемый слой. При удалении контейнера этот слой удаляется.
- **Том** `tickets-week4-pgdata` хранит данные PostgreSQL независимо от контейнера.
- **Сеть** `tickets-week4` позволяет API найти PostgreSQL по имени `tickets-week4-db`. Проброс `127.0.0.1:18080:8080` открывает порт API на Mac.

При запуске API передали `DATABASE_URL`, `BOOKING_TTL=2m` и `SEED_DEMO=true` через `docker run -e`. Сборку и запуск подтвердили. Готовность проверяли через `curl -i http://127.0.0.1:18080/ready`, UID процесса — через `docker top tickets-week4-api -eo pid,uid,args`.

## Проверка graceful shutdown

В [коде API](../../app/api/main.go) обработчик `SIGTERM` вызывает `server.Shutdown` с лимитом 10 секунд. Сервер прекращает принимать новые соединения и ждёт, пока активные запросы завершатся или истечёт лимит.

1. В учебной базе открыть транзакцию и выполнить `LOCK TABLE events IN ACCESS EXCLUSIVE MODE`.
2. Запустить `curl -i --max-time 20 http://127.0.0.1:18080/api/events` и убедиться, что запрос ждёт.
3. В другом терминале запустить `docker stop --timeout 15 tickets-week4-api`. Пока команда ждёт, выполнить `ROLLBACK` в сессии PostgreSQL.
4. Проверить HTTP-статус и состояние контейнера.

```bash
docker inspect tickets-week4-api \
  --format 'status={{.State.Status}} exit={{.State.ExitCode}}'
```

Критерии прохождения — активный запрос получил HTTP 200, контейнер завершился со статусом `exited` и кодом 0. Фактический вывод эксперимента пока не зафиксирован.

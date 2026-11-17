# Места

Сервис бронирования билетов из [учебного роадмапа](roadmap.md). Возможности, состав и маршруты API описаны в [документации приложения](docs/architecture/application.md).

## Запуск

На машине с Docker и Docker Compose выполните из корня проекта:

```bash
docker compose -f deploy/compose.yaml up --build -d
```

Откройте [http://localhost:8080](http://localhost:8080). Выберите событие в афише, укажите количество билетов и забронируйте. Номер появится в блоке «Моя бронь», где можно проверить статус или отменить бронь.

```bash
bash deploy/smoke.sh
docker compose -f deploy/compose.yaml logs -f api worker
docker compose -f deploy/compose.yaml down
```

Smoke-тест использует `curl`, Python 3 и демонстрационное событие. Команда `down` сохраняет данные в Docker volume. Если порт 8080 занят, задайте `WEB_PORT=8081` перед командой запуска и `BASE_URL=http://localhost:8081` перед smoke-тестом.

Инструкции по разработке без Docker и нагрузочным сценариям находятся в [руководстве по локальному запуску](docs/runbooks/local.md).

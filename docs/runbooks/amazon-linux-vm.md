# Развёртывание учебного стенда на Amazon Linux 2023

Стенд состоит из Nginx, API бронирования и PostgreSQL на одной VM. Nginx принимает HTTP на `127.0.0.1:8081`, API слушает `127.0.0.1:8080`, PostgreSQL доступен приложению на `127.0.0.1:5432`. Запросы проверки выполняются внутри VM. Предполагается, что на VM уже можно войти по SSH под `ec2-user`.

## 1. Пользователь и пакеты

На VM под `ec2-user`:

```bash
sudo useradd -m -s /bin/bash learner
sudo install -d -m 700 -o learner -g learner /home/learner/.ssh
sudo install -m 600 -o learner -g learner \
  ~/.ssh/authorized_keys /home/learner/.ssh/authorized_keys
sudo dnf install golang postgresql16-server nginx python3 curl
```

`useradd -m` создаёт домашний каталог. `install` копирует публичные ключи и выставляет владельца и права. С Mac проверь вход по SSH под `learner`, оставив административный сеанс открытым. В новом сеансе проверь `go version`: проект требует Go 1.24 или новее.

## 2. PostgreSQL

На чистой VM под `ec2-user`:

```bash
sudo postgresql-setup --initdb
sudo systemctl enable --now postgresql.service
sudo -u postgres psql
```

`--initdb` подготавливает новый каталог данных. На стенде с существующей базой этот шаг пропускаем. `enable --now` включает автозапуск и запускает службу сейчас. В клиенте `psql` выполни:

```sql
CREATE ROLE tickets_app LOGIN;
\password tickets_app
CREATE DATABASE tickets OWNER tickets_app;
\q
```

`\password` запрашивает пароль без вывода его в терминал. Открой `sudo nano /var/lib/pgsql/data/pg_hba.conf` и добавь перед первым общим правилом для `127.0.0.1/32`:

```text
host    tickets    tickets_app    127.0.0.1/32    scram-sha-256
```

PostgreSQL использует первое совпавшее правило. Перечитай настройки и проверь вход:

```bash
sudo systemctl reload postgresql.service
psql -h 127.0.0.1 -U tickets_app -d tickets -W -c 'SELECT current_user, current_database();'
```

Ожидаем роль `tickets_app` и базу `tickets`.

## 3. Исходники и сборка

На VM под `learner` создай папку командой `mkdir -p ~/tickets/app`. На Mac из корня проекта перенеси исходники и тест, заменив `АДРЕС_VM`:

```bash
scp -r app/go.mod app/go.sum app/api app/internal learner@АДРЕС_VM:/home/learner/tickets/app/
scp deploy/smoke.sh learner@АДРЕС_VM:/home/learner/tickets/smoke.sh
```

На VM под `learner` собери бинарный файл:

```bash
cd ~/tickets/app
go build -o tickets-api ./api
```

## 4. Служба API

На VM под `ec2-user` создай файл с паролем, если его ещё нет. Команда `install` инициализирует его пустым, поэтому существующий файл открывай сразу в редакторе.

```bash
sudo install -m 600 /dev/null /etc/tickets-api.env
sudo nano /etc/tickets-api.env
```

Для учебного пароля без одинарной кавычки запиши `PGPASSWORD='ТВОЙ_ПАРОЛЬ'`, подставив пароль роли. В отдельной строке добавь `SEED_DEMO=true`, если база новая и нужно добавить записи для smoke-теста. После первого успешного старта эту строку можно убрать. API применяет встроенные миграции при запуске. Демоданные используют даты относительно дня загрузки и не обновляют существующие строки, поэтому тесту нужен будущий `night-jazz` со свободным местом.

Открой `sudo nano /etc/systemd/system/tickets-api.service` и запиши:

```ini
[Unit]
Description=Tickets API
Wants=postgresql.service
After=postgresql.service

[Service]
User=learner
WorkingDirectory=/home/learner/tickets/app
Environment=HTTP_ADDR=127.0.0.1:8080
Environment="DATABASE_URL=postgres://tickets_app@127.0.0.1:5432/tickets?sslmode=disable"
EnvironmentFile=/etc/tickets-api.env
ExecStart=/home/learner/tickets/app/tickets-api

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now tickets-api.service
curl -i http://127.0.0.1:8080/ready
```

`daemon-reload` перечитывает описания служб. `/ready` должен вернуть `200 OK` и `{"status":"ready"}`. Если служба завершилась, причина обычно есть в `sudo journalctl -u tickets-api.service -n 20 --no-pager`.

## 5. Nginx и smoke-тест

В стандартном блоке `server` в `/etc/nginx/nginx.conf` ограничь слушатели порта 80 адресами `127.0.0.1:80` и `[::1]:80`. В файле `/etc/nginx/conf.d/tickets.conf` создай блок:

```nginx
server {
    listen 127.0.0.1:8081;

    location / {
        proxy_pass http://127.0.0.1:8080;
    }
}
```

На чистом стенде проверь конфигурацию и включи Nginx:

```bash
sudo nginx -t && sudo systemctl enable --now nginx.service
```

Если Nginx уже работает и конфигурация менялась, примени её через `sudo nginx -t && sudo systemctl reload nginx`. Учебный HTTPS на 8443 можно добавить по [конспекту второй недели](../learning/week-02-nginx.md#4-https-и-tls). Для smoke-теста достаточно локального HTTP.

Под `learner`:

```bash
BASE_URL=http://127.0.0.1:8081 bash ~/tickets/smoke.sh
```

Ожидаем `Smoke test passed: …`. Скрипт проверяет готовность, создаёт бронь, проверяет её статус и идемпотентность, затем отменяет бронь.

## 6. Учебное восстановление службы на существующей VM

Сначала получи успешный результат smoke-теста. Затем под `ec2-user` отключи и останови API. Перенеси файл службы в резервный путь, чтобы он исчез из каталога systemd:

```bash
sudo systemctl disable --now tickets-api.service
sudo mv -i /etc/systemd/system/tickets-api.service /root/tickets-api.service.week03.bak
sudo systemctl daemon-reload
systemctl status tickets-api.service --no-pager
```

`disable --now` убирает автозапуск и останавливает процесс. `mv -i` запросит подтверждение, если резервный файл уже существует. После переноса ожидаем, что systemd не найдёт службу. Nginx, PostgreSQL, файл с паролем и бинарный файл остаются на месте. До восстановления `curl -i http://127.0.0.1:8081/ready` должен вернуть `502`.

Воссоздай файл службы по разделу 4, начиная с команды `sudo nano /etc/systemd/system/tickets-api.service`. Затем перечитай настройки, включи службу и проверь прямой `/ready`. Повтори smoke-тест через Nginx. Если восстановление по инструкции остановилось на ошибке, резервный файл можно скопировать назад и повторить `daemon-reload` и `enable --now`.

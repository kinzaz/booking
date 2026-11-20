# Неделя 6. Ansible, развёртывание чистой VM и восстановление PostgreSQL

Конспект выполненных упражнений 18–19 ноября 2026 года. Содержит команды, объяснения и снимок текущих файлов проекта. Команды для Mac выполнялись из `/Users/kinzaz/Documents/infra`, если явно не указано другое место. Команды внутри `ssh '…'` выполняются на VM.

Публичный DNS и публичный HTTPS ещё предстоят. Учебный HTTPS на loopback-адресе уже проверен. Архив базы восстановлен на Mac в отдельном экземпляре PostgreSQL, и API прочитал из него прежнюю бронь.

## Содержание

1. [Устройство стенда и Ansible](#1-устройство-стенда-и-ansible)
2. [Установка и восстановление SSH-доступа](#2-установка-и-восстановление-ssh-доступа)
3. [Inventory и первый модуль](#3-inventory-и-первый-модуль)
4. [Первый playbook и идемпотентность](#4-первый-playbook-и-идемпотентность)
5. [Переменные, шаблоны и handlers](#5-переменные-шаблоны-и-handlers)
6. [Роли и переменные группы](#6-роли-и-переменные-группы)
7. [Пользователь Linux и SSH](#7-пользователь-linux-и-ssh)
8. [Исходники, сборка и systemd](#8-исходники-сборка-и-systemd)
9. [PostgreSQL и Vault](#9-postgresql-и-vault)
10. [Nginx и учебный HTTPS](#10-nginx-и-учебный-https)
11. [Проверки результата](#11-проверки-результата)
12. [Эксперименты с отказами](#12-эксперименты-с-отказами)
13. [Создание чистой VM в AWS](#13-создание-чистой-vm-в-aws)
14. [Первое развёртывание и исправления](#14-первое-развёртывание-и-исправления)
15. [Smoke-тест и перезагрузка](#15-smoke-тест-и-перезагрузка)
16. [Копирование и восстановление базы](#16-копирование-и-восстановление-базы)
17. [Результаты и ограничения](#17-результаты-и-ограничения)

## 1. Устройство стенда и Ansible

Ansible выполняется на управляющем узле. В упражнении это Mac. Он читает inventory и playbook, подключается по SSH к VM и выполняет задачи. В дальнейшем тот же playbook можно запускать с CI runner либо через AWX / Ansible Automation Platform. Этот переход добавлен в неделю 8 курса.

На VM работают PostgreSQL, Go API и Nginx. В этом упражнении приложение развёрнуто как служба systemd. Docker использовали на Mac для отдельного стенда восстановления.

```text
Mac с Ansible → SSH → Amazon Linux VM
                         ├── PostgreSQL на 127.0.0.1:5432
                         ├── API на 127.0.0.1:8080
                         └── Nginx
                             ├── HTTP-прокси на 127.0.0.1:8081
                             ├── HTTPS-прокси на 127.0.0.1:8443
                             └── учебная страница на 127.0.0.1:18090
```

Inventory описывает машины и параметры подключения. Playbook описывает, к каким хостам применять задачи и роли. Модуль — готовая программа для конкретной операции. Роль объединяет задачи, шаблоны, handlers и переменные в стандартную структуру.

У каждого модуля собственные параметры. У `file` параметр `path` задаёт управляемый путь. У `copy` и `template` параметр `dest` задаёт назначение, а `src` — исходник. Документацию установленной версии можно открыть через `ansible-doc ansible.builtin.file`, `ansible-doc ansible.builtin.copy` и `ansible-doc ansible.builtin.template`. Из просмотрщика выходят клавишей `q`.

## 2. Установка и восстановление SSH-доступа

На Mac установили Ansible через Homebrew.

```bash
brew install ansible
ansible --version
```

В выводе пользователя был `ansible [core 2.21.6]` и `config file = None`. Это означает использование стандартных настроек при отсутствии выбранного конфигурационного файла. Python на Mac исполняет сам Ansible, Python на VM исполняет переданные модули.

Ранее работала команда подключения к первой VM.

```bash
ssh ec2-user@16.192.61.253
```

Позже она стала возвращать `Permission denied (publickey,gssapi-keyex,gssapi-with-mic)`. При диагностике выяснили, что SSH-agent пуст, клиент предлагает `id_ed25519`, а сервер его отклоняет. С ключом `linux-test.pem` вход прошёл.

```bash
ssh-add -l
ssh -v ec2-user@16.192.61.253
ssh -i ~/.ssh/linux-test.pem -o IdentitiesOnly=yes ec2-user@16.192.61.253
ssh-add ~/.ssh/linux-test.pem
```

`-v` выводит диагностику, включая `Offering public key`. `-i` выбирает ключ, `IdentitiesOnly=yes` ограничивает выбор настроенными ключами. `ssh-add` загружает ключ в агент. После загрузки снова заработало подключение без `-i`. Почему агент опустел, не установлено. Возможные причины обсуждали как гипотезы — перезагрузка, выход из сессии или перезапуск агента.

В inventory затем явно указали путь к рабочему ключу, чтобы подключение Ansible не зависело от его наличия в агенте.

## 3. Inventory и первый модуль

Создали `infra/ansible/inventory.ini`. Изначально в группе `lab` был один хост `tickets`. После создания чистой VM добавили `tickets-clean`.

### Текущий inventory

`[lab]` — группа. `tickets` и `tickets-clean` — выбранные нами имена хостов. `ansible_host` задаёт адрес, `ansible_user` — SSH-пользователя. `ansible_python_interpreter` фиксирует Python на VM. `ansible_ssh_private_key_file` задаёт ключ на Mac. Публичные IP отражают состояние на дату упражнения и могут измениться после остановки и запуска EC2.

Файл `infra/ansible/inventory.ini`.

```ini
[lab]
tickets ansible_host=16.192.61.253 ansible_user=ec2-user ansible_python_interpreter=/usr/bin/python3.9 ansible_ssh_private_key_file=~/.ssh/linux-test.pem
tickets-clean ansible_host=13.60.98.7 ansible_user=ec2-user ansible_python_interpreter=/usr/bin/python3.9 ansible_ssh_private_key_file=~/.ssh/linux-test.pem
```

Первое подключение проверили модулем `ping`.

```bash
ansible tickets -i infra/ansible/inventory.ini -m ansible.builtin.ping
```

Получили `SUCCESS`, `changed: false`, `ping: pong`. Ansible автоматически обнаружил `/usr/bin/python3.9` и предупредил, что при изменении состава Python на VM выбор может измениться. После этого путь зафиксировали в inventory.

### Как исполняется ping

1. Ansible на Mac читает inventory и выбирает хост.
2. Находит локальный Python-файл `ansible/modules/ping.py`, установленный вместе с Ansible.
3. Упаковывает модуль и нужные вспомогательные части `module_utils` механизмом Ansiballz.
4. Через SSH подготавливает временный каталог на VM. При обычном режиме передаёт файл вида `AnsiballZ_ping.py`, обычно через SFTP.
5. Запускает его удалённым Python. Код ping возвращает значение параметра `data`, по умолчанию `pong`, через JSON.
6. Ansible получает результат по SSH, показывает статус и очищает временные файлы.

При включённом pipelining передача и исполнение могут идти через стандартный ввод Python. Детали конкретного запуска видны с `-vvv`.

```bash
ansible tickets -i infra/ansible/inventory.ini -m ansible.builtin.ping -vvv
```

В журнале разбирали `Using module file`, `ESTABLISH SSH CONNECTION`, `PUT` и `EXEC`.

### Если Python отсутствует

Для начальной подготовки можно использовать `raw`, который передаёт команду удалённой оболочке через SSH.

```bash
ansible tickets -i infra/ansible/inventory.ini \
  -m ansible.builtin.raw -a 'command -v python3 && python3 --version'

ansible tickets -i infra/ansible/inventory.ini \
  -m ansible.builtin.raw -a 'sudo dnf install -y python3'
```

У play с такой подготовкой нужно `gather_facts: false`, поскольку автоматический сбор facts также требует Python. На наших VM Python уже присутствовал. Это был разбор способа начальной подготовки, фактической установки через raw не потребовалось.

### Как выбрать одну VM

`hosts: lab` выбирает группу, `hosts: tickets` — конкретный хост. Параметр `--limit tickets-clean` ограничивает выбор playbook. Хост должен подходить одновременно под `hosts` и под `--limit`.

```bash
ansible-playbook -i infra/ansible/inventory.ini \
  infra/ansible/site.yml --limit tickets-clean --list-hosts --ask-vault-pass
```

После появления Vault команды, загружающие переменные группы `lab`, требуют доступа к паролю Vault, даже если сам модуль пароль базы не использует.

## 4. Первый playbook и идемпотентность

Создали `first.yml` с двумя задачами. Первая создавала `/home/ec2-user/ansible-lab` через `file` с `state: directory` и правами `0755`. Вторая записывала `hello.txt` через `copy` с параметром `content` и правами `0644`.

```yaml
- name: Первое упражнение с Ansible
  hosts: lab
  gather_facts: false
  tasks:
    - name: Создать каталог для упражнения
      ansible.builtin.file:
        path: /home/ec2-user/ansible-lab
        state: directory
        mode: '0755'

    - name: Создать файл с приветствием
      ansible.builtin.copy:
        dest: /home/ec2-user/ansible-lab/hello.txt
        content: |
          Этот файл создан через Ansible.
        mode: '0644'
```

Проверили синтаксис, применили дважды.

```bash
ansible-playbook -i infra/ansible/inventory.ini infra/ansible/first.yml --syntax-check
ansible-playbook -i infra/ansible/inventory.ini infra/ansible/first.yml
```

При первом создании объектов ожидаются изменения. Повторное применение при совпадении состояния даёт `changed=0`. Это проверка идемпотентности. `ok` означает успешную задачу, `changed` — успешную задачу с изменениями, `failed` — ошибку выполнения, `unreachable` — невозможность подключения, `skipped` — пропуск по условию или особенностям режима.

Права `0755` дают владельцу каталога чтение, запись и вход, остальным чтение и вход. `0644` дают владельцу файла чтение и запись, остальным чтение. Права записывали строками в кавычках.

### Ручное изменение и check mode

Изменили файл на VM через SSH.

```bash
ssh ec2-user@16.192.61.253 \
  'printf "Изменено вручную на сервере\n" > /home/ec2-user/ansible-lab/hello.txt'

ansible-playbook -i infra/ansible/inventory.ini \
  infra/ansible/first.yml --check --diff
```

`--check` оценивал будущие изменения, `--diff` показывал различия. Для задач file и copy этот режим поддерживается. Проверили, что ручное содержимое пока сохраняется, затем выполнили обычный запуск с `--diff` и восстановили описанный текст. Следующий прогон снова дал отсутствие изменений. Модуль command без специальной поддержки обычно пропускается в check mode, поэтому этот режим не подтверждает успешность всех реальных команд.

## 5. Переменные, шаблоны и handlers

Вместо текста внутри copy сделали шаблон Jinja2 `templates/hello.txt.j2`.

```jinja2
Приложение: {{ application_name }}
Окружение: {{ environment_name }}
Хост в inventory: {{ inventory_hostname }}
```

`{{ … }}` подставляет значение. `application_name` и `environment_name` определили в vars, `inventory_hostname` предоставляет Ansible. Template обрабатывается на Mac, готовый текст передаётся на VM. В playbook src ищется в каталоге templates рядом с ним, в роли — в templates самой роли.

```bash
ansible-playbook -i infra/ansible/inventory.ini \
  infra/ansible/first.yml -e environment_name=staging --diff
```

Параметр `-e` задаёт extra vars с высоким приоритетом. На VM получили строки с приложением tickets, окружением staging и хостом tickets. Изменение значения для одного запуска не переписывает vars в YAML. Позднее для упражнений использовали `training`.

Добавили `notify` к задаче template и handler с совпадающим именем. Первым handler был debug с сообщением об изменении файла. При changed он запускался, при повторном прогоне пропускался. Несколько уведомлений одному handler объединяются до точки выполнения. Handler обычно выполняется в конце соответствующей секции play. `meta: flush_handlers` позволяет выполнить накопленные обработчики раньше.

Затем применили этот механизм к Nginx — изменённый шаблон уведомлял handler с `state: reloaded`. Перед применением конфигурации обычная задача `nginx -t` проверяла синтаксис, а `changed_when: false` отмечал проверку как операцию без изменений.

## 6. Роли и переменные группы

Создали роль nginx_lab и перенесли в неё tasks/main.yml, handlers/main.yml, templates и defaults/main.yml. В playbook осталось подключение roles. Значения defaults переопределяются переменными группы, а extra vars могут перекрыть их на конкретном запуске.

Изначально переменные группы находились в group_vars/lab.yml. При добавлении Vault создали каталог group_vars/lab и перенесли обычные переменные в main.yml.

```bash
mkdir -p infra/ansible/group_vars/lab
mv infra/ansible/group_vars/lab.yml infra/ansible/group_vars/lab/main.yml
```

Эта команда переноса уже выполнена. Повторять её для текущего дерева не требуется.

### Переменные группы lab

Эти значения автоматически получают оба хоста группы lab. При запуске с `-e environment_name=training` файл окружения и учебная страница содержат training.

Файл `infra/ansible/group_vars/lab/main.yml`.

```yaml
application_name: tickets
environment_name: staging
```

## 7. Пользователь Linux и SSH

В site.yml включили `become: true`. Задачи получают права через sudo. `become_user` у конкретной задачи позволяет выбрать другого системного пользователя.

Роль users сначала создаёт learner, затем каталог .ssh, читает authorized_keys пользователя ec2-user и добавляет его строки пользователю learner. На чистой EC2 исходный authorized_keys появился благодаря выбранной ключевой паре при создании VM.

### Задачи роли users

`slurp` возвращает содержимое файла в Base64. `register` сохраняет результат. `b64decode` восстанавливает текст, `splitlines()` разбивает его на строки. `loop` подставляет очередную строку в item. Условия when пропускают пустые строки и комментарии. Lineinfile добавляет отсутствующие строки, сохраняя остальные ключи. Если в authorized_keys есть дополнительные опции перед ключом, они копируются вместе со строкой.

Файл `infra/ansible/roles/users/tasks/main.yml`.

```yaml
- name: Создать пользователя приложения
  ansible.builtin.user:
    name: "{{ app_user }}"
    state: present
    create_home: true
    shell: /bin/bash

- name: Создать каталог SSH пользователя приложения
  ansible.builtin.file:
    path: "/home/{{ app_user }}/.ssh"
    state: directory
    owner: "{{ app_user }}"
    group: "{{ app_user }}"
    mode: '0700'

- name: Прочитать разрешённые ключи ec2-user
  ansible.builtin.slurp:
    src: /home/ec2-user/.ssh/authorized_keys
  register: source_authorized_keys

- name: Добавить ключи для доступа к пользователю приложения
  ansible.builtin.lineinfile:
    path: "/home/{{ app_user }}/.ssh/authorized_keys"
    line: "{{ item }}"
    create: true
    owner: "{{ app_user }}"
    group: "{{ app_user }}"
    mode: '0600'
  loop: "{{ (source_authorized_keys.content | b64decode).splitlines() }}"
  when:
    - item | trim | length > 0
    - not item.lstrip().startswith('#')
```

### Значение пользователя по умолчанию

app_user обозначает пользователя Linux.

Файл `infra/ansible/roles/users/defaults/main.yml`.

```yaml
app_user: learner
```

Проверили учётную запись и SSH.

```bash
ssh ec2-user@16.192.61.253 'getent passwd learner; id learner'
ssh -i ~/.ssh/linux-test.pem -o IdentitiesOnly=yes learner@16.192.61.253 whoami
```

На первой VM получили `learner:x:1001:1001::/home/learner:/bin/bash` и `uid=1001(learner) gid=1001(learner) groups=1001(learner)`. SSH вернул learner. UID на другой VM определяется её состоянием, поэтому в роли используем имя пользователя.

## 8. Исходники, сборка и systemd

В самостоятельном упражнении создали роль app. Она подготовила каталог приложения и environment.txt. При проверке исправили путь с /home/ec2-user на /home/{{ app_user }} и имя готового файла с environment.txt.j2 на environment.txt. Если прежняя задача уже была применена, старый файл мог остаться в каталоге ec2-user — изменение dest само по себе его не удаляет.

Через slurp прочитали environment.txt и через assert проверили наличие ожидаемых строк. Оператор `~` соединяет строки, `in` проверяет вхождение. Условия assert и when пишутся без внешних `{{ }}`.

Включили gather_facts. Получили Amazon, 2023, x86_64, dnf. Facts содержатся в словаре ansible_facts. Версия Go на первой VM была `go1.26.10-X:nodwarf5 linux/amd64`. Проект требует Go 1.24 или новее. Добавили установку golang и проверку версии.

Для исходников используем copy с loop по go.mod, go.sum, api и internal. Playbook_dir обозначает каталог site.yml, ../../app ведёт к исходникам проекта на Mac. Отсутствие завершающего слеша у каталогов src означает копирование самих каталогов. Mode preserve сохраняет исходные права файлов.

### Сборка и повторные попытки

Первое условие сборки учитывало изменение исходников или отсутствие бинарника. Обнаружили недостаток — после неудачной сборки исходники уже скопированы, а старый бинарник может существовать. Добавили маркер .build-success.

При изменении исходников или app_force_build=true маркер удаляется. Блок сборки выполняется при отсутствии маркера или бинарника. Go создаёт tickets-api.next. После успешной компиляции mv заменяет tickets-api в том же каталоге. Затем записывается маркер и ставится уведомление перезапуска API. При ошибке компиляции последующие задачи блока не выполняются и маркер отсутствует, поэтому следующий прогон повторит сборку.

Замена бинарника через rename в одном каталоге атомарна. Уже работающий процесс продолжает исполнять старый файл до перезапуска. Маркер не хранит хеш исходников или версию Go. Изменение исходников вручную на VM может остаться незамеченным, если последующее copy не сообщает changed. Прерванное частичное копирование исходников и сбой между заменой бинарника и выполнением handler также требуют отдельной проверки или принудительной сборки. Эти сценарии пока не автоматизированы полностью.

### Значения app по умолчанию

Отдельный default app_user позволяет роли app использовать имя пользователя при самостоятельном подключении. Для чистой VM пользователь должен быть создан до задач app.

Файл `infra/ansible/roles/app/defaults/main.yml`.

```yaml
app_user: learner
app_force_build: false
```

### Полные текущие задачи app

Задачи идут от подготовки каталога и проверки данных к установке Go, копированию исходников, сборке, файлу окружения и службе. Command возвращает stdout, stderr и rc. Changed_when=false применяется к командам проверки.

Файл `infra/ansible/roles/app/tasks/main.yml`.

```yaml
- name: Создать каталог приложения
  ansible.builtin.file:
    path: "/home/{{ app_user }}/{{ application_name }}"
    state: directory
    owner: "{{ app_user }}"
    group: "{{ app_user }}"
    mode: '0755'

- name: Создать файл из шаблона
  ansible.builtin.template:
    src: environment.txt.j2
    dest: "/home/{{ app_user }}/{{ application_name }}/environment.txt"
    owner: "{{ app_user }}"
    group: "{{ app_user }}"
    mode: '0644'

- name: Прочитать файл окружения
  ansible.builtin.slurp:
    src: "/home/{{ app_user }}/{{ application_name }}/environment.txt"
  register: app_environment_file

- name: Проверить содержимое файла окружения
  ansible.builtin.assert:
    that:
      - "'Application: ' ~ application_name in app_environment_text"
      - "'Environment: ' ~ environment_name in app_environment_text"
      - "'User: ' ~ app_user in app_environment_text"
    success_msg: Файл окружения содержит ожидаемые значения.
    fail_msg: Содержимое файла окружения не соответствует переменным.
  vars:
    app_environment_text: "{{ app_environment_file.content | b64decode }}"

- name: Установить Go
  ansible.builtin.package:
    name: golang
    state: present

- name: Узнать версию Go
  ansible.builtin.command:
    cmd: go version
  register: app_go_version
  changed_when: false

- name: Проверить совместимость версии Go
  ansible.builtin.assert:
    that:
      - app_go_version.stdout is search('go1\.(2[4-9]|[3-9][0-9])([.\s-]|$)')
    success_msg: Версия Go подходит для сборки API.
    fail_msg: Для сборки API нужен Go 1.24 или новее в ветке Go 1.x.

- name: Показать версию Go
  ansible.builtin.debug:
    msg: "{{ app_go_version.stdout }}"

- name: Создать каталог исходников API
  ansible.builtin.file:
    path: "/home/{{ app_user }}/{{ application_name }}/app"
    state: directory
    owner: "{{ app_user }}"
    group: "{{ app_user }}"
    mode: '0755'

- name: Скопировать исходники API
  ansible.builtin.copy:
    src: "{{ playbook_dir }}/../../app/{{ item }}"
    dest: "/home/{{ app_user }}/{{ application_name }}/app/"
    owner: "{{ app_user }}"
    group: "{{ app_user }}"
    mode: preserve
  loop:
    - go.mod
    - go.sum
    - api
    - internal
  register: app_sources

- name: Сбросить отметку сборки при обновлении исходников
  ansible.builtin.file:
    path: "/home/{{ app_user }}/{{ application_name }}/app/.build-success"
    state: absent
  when: app_sources.changed or (app_force_build | bool)

- name: Проверить отметку успешной сборки
  ansible.builtin.stat:
    path: "/home/{{ app_user }}/{{ application_name }}/app/.build-success"
  register: app_build_marker

- name: Проверить наличие бинарника API
  ansible.builtin.stat:
    path: "/home/{{ app_user }}/{{ application_name }}/app/tickets-api"
  register: app_binary

- name: Собрать и установить API
  become_user: "{{ app_user }}"
  when: not app_build_marker.stat.exists or not app_binary.stat.exists
  block:
    - name: Собрать новый бинарник API
      ansible.builtin.command:
        cmd: go build -o tickets-api.next ./api
        chdir: "/home/{{ app_user }}/{{ application_name }}/app"

    - name: Заменить бинарник API
      ansible.builtin.command:
        cmd: mv -f tickets-api.next tickets-api
        chdir: "/home/{{ app_user }}/{{ application_name }}/app"
      notify: Перезапустить API

    - name: Записать отметку успешной сборки
      ansible.builtin.copy:
        content: "Build completed successfully.\n"
        dest: "/home/{{ app_user }}/{{ application_name }}/app/.build-success"
        mode: '0644'

- name: Создать файл окружения API
  ansible.builtin.template:
    src: tickets-api.env.j2
    dest: /etc/tickets-api.env
    owner: root
    group: root
    mode: '0600'
  no_log: true
  diff: false
  notify: Перезапустить API

- name: Разместить описание службы API
  ansible.builtin.template:
    src: tickets-api.service.j2
    dest: /etc/systemd/system/tickets-api.service
    owner: root
    group: root
    mode: '0644'
  notify: Перезапустить API

- name: Запустить API и включить автозапуск
  ansible.builtin.systemd_service:
    name: tickets-api.service
    state: started
    enabled: true
    daemon_reload: true
```

### Шаблон environment.txt

Результат находится в /home/learner/tickets/environment.txt.

Файл `infra/ansible/roles/app/templates/environment.txt.j2`.

```jinja2
Application: {{ application_name }}
Environment: {{ environment_name }}
User: {{ app_user }}
```

### Unit-файл API

User выбирает системного пользователя API. WorkingDirectory задаёт рабочий каталог. ExecStart запускает бинарник. HTTP_ADDR ограничивает слушатель loopback. DATABASE_URL выбирает локальную базу и роль, пароль поступает через PGPASSWORD. EnvironmentFile читается systemd при старте. WantedBy связывает автозапуск с multi-user.target. After задаёт порядок запуска относительно PostgreSQL, но само по себе не подтверждает готовность базы.

Файл `infra/ansible/roles/app/templates/tickets-api.service.j2`.

```ini
[Unit]
Description=Tickets API
Wants=postgresql.service
After=postgresql.service

[Service]
User={{ app_user }}
WorkingDirectory=/home/{{ app_user }}/{{ application_name }}/app
Environment=HTTP_ADDR=127.0.0.1:8080
Environment="DATABASE_URL=postgres://tickets_app@127.0.0.1:5432/tickets?sslmode=disable"
EnvironmentFile=/etc/tickets-api.env
ExecStart=/home/{{ app_user }}/{{ application_name }}/app/tickets-api

[Install]
WantedBy=multi-user.target
```

### Handler API

Daemon_reload перечитывает unit-файлы systemd. Restarted перезапускает процесс API. Handler получает уведомления от сборки, шаблона окружения и unit-файла.

Файл `infra/ansible/roles/app/handlers/main.yml`.

```yaml
- name: Перезапустить API
  ansible.builtin.systemd_service:
    name: tickets-api.service
    daemon_reload: true
    state: restarted
```

Принудительную сборку проверяли отдельным запуском.

```bash
ansible-playbook -i infra/ansible/inventory.ini infra/ansible/site.yml \
  -e environment_name=training -e app_force_build=true --ask-vault-pass
```

После добавления второго хоста для экспериментов нужно указывать --limit нужного хоста. Команда без limit применяет play ко всей группе lab.

## 9. PostgreSQL и Vault

Проверили пакет postgresql16-server, службу postgresql.service и каталог данных /var/lib/pgsql/data. На первой VM пакет имел версию postgresql16-server-16.15-1.amzn2023.0.1.x86_64. Установку и инициализацию перенесли в роль.

```bash
ssh ec2-user@16.192.61.253 \
  'rpm -q postgresql16-server; sudo systemctl cat postgresql.service; sudo -u postgres psql -Atc "SHOW data_directory;"'
```

Postgresql-setup --initdb создаёт кластер PostgreSQL. Creates с путём PG_VERSION пропускает команду, если кластер уже инициализирован. На существующем каталоге данных эту операцию повторно не запускали.

Для модулей PostgreSQL на Mac нужна коллекция community.postgresql, на VM — драйвер python3-psycopg2. Обнаружили установленную коллекцию версии 4.2.0 и зафиксировали её.

```bash
ansible-galaxy collection list community.postgresql
ansible-galaxy collection install -r infra/ansible/requirements.yml
```

### Зависимости коллекций

Файл позволяет установить выбранную версию на другой управляющей машине.

Файл `infra/ansible/requirements.yml`.

```yaml
collections:
  - name: community.postgresql
    version: "4.2.0"
```

При административных задачах become_user: postgres выбирает пользователя Linux, login_user: postgres — роль PostgreSQL. Без login_host подключение идёт через Unix-сокет. В текущей конфигурации peer позволяет административное подключение от системного пользователя postgres.

Postgresql_user создаёт tickets_app с LOGIN. Postgresql_db создаёт tickets и назначает владельца. В текущих задачах пароль приходит из Vault.

### Создание Vault

```bash
EDITOR=nano ansible-vault create infra/ansible/group_vars/lab/vault.yml
```

Задали пароль Vault, затем в редакторе записали переменную. Ниже только образец с заменителем.

```yaml
vault_postgres_app_password: 'ПАРОЛЬ_РОЛИ_POSTGRESQL'
```

Пароль Vault открывает зашифрованный файл. Пароль роли PostgreSQL используется приложением при подключении к базе. Для первого применения взяли существующий пароль tickets_app. Настоящие пароли в этот конспект не включены, vault.yml не расшифровывался при подготовке документа.

```bash
head -n 2 infra/ansible/group_vars/lab/vault.yml
EDITOR=nano ansible-vault edit infra/ansible/group_vars/lab/vault.yml
```

У зашифрованного файла заголовок $ANSIBLE_VAULT. Для запуска добавляем --ask-vault-pass. Пароль Vault сохраняют отдельно, в менеджере паролей. Зашифрованный файл без пароля не позволяет подготовить приложение на новой VM.

No_log=true скрывает параметры и результат секретных задач. Diff=false отключает показ различий файла окружения. Сам файл на VM содержит открытый пароль для процесса API и защищён правами 0600 с владельцем root.

### Шаблон файла окружения API

Template создаёт весь /etc/tickets-api.env. SEED_DEMO=true включает загрузку демонстрационных мероприятий на новой базе. API применяет миграции при запуске. Фильтр to_json использовали для заключения учебного пароля в кавычки. Для паролей с управляющими символами и нестандартными escape-последовательностями нужна отдельная проверка совместимости формата с systemd.

Файл `infra/ansible/roles/app/templates/tickets-api.env.j2`.

```jinja2
PGPASSWORD={{ vault_postgres_app_password | to_json }}
SEED_DEMO=true
```

### Defaults PostgreSQL

Префикс postgres_ отделяет роль базы от пользователя Linux app_user.

Файл `infra/ansible/roles/postgres/defaults/main.yml`.

```yaml
postgres_app_user: tickets_app
postgres_app_database: tickets
```

### Текущие задачи PostgreSQL

Задача установки сервера сейчас встречается отдельно и повторяется в списке установки драйвера. Это избыточность, оставшаяся после пошагового обучения. State=present сохраняет повторное выполнение без изменений. Конспект отражает существующие файлы без дополнительного изменения ролей.

Файл `infra/ansible/roles/postgres/tasks/main.yml`.

```yaml
- name: Установить сервер PostgreSQL
  ansible.builtin.package:
    name: postgresql16-server
    state: present

- name: Инициализировать кластер PostgreSQL
  ansible.builtin.command:
    cmd: postgresql-setup --initdb
    creates: /var/lib/pgsql/data/PG_VERSION

- name: Запустить PostgreSQL и включить автозапуск
  ansible.builtin.systemd_service:
    name: postgresql.service
    state: started
    enabled: true

- name: Установить PostgreSQL и Python-драйвер
  ansible.builtin.package:
    name:
      - postgresql16-server
      - python3-psycopg2
    state: present

- name: Проверить подключение к PostgreSQL
  become_user: postgres
  community.postgresql.postgresql_ping:
    login_user: postgres
    login_db: postgres
  register: postgres_connection

- name: Подтвердить доступность PostgreSQL
  ansible.builtin.assert:
    that:
      - postgres_connection.is_available
    success_msg: Ansible подключился к PostgreSQL.
    fail_msg: Ansible не смог подключиться к PostgreSQL.

- name: Создать роль приложения в PostgreSQL
  become_user: postgres
  community.postgresql.postgresql_user:
    login_user: postgres
    login_db: postgres
    name: "{{ postgres_app_user }}"
    password: "{{ vault_postgres_app_password }}"
    role_attr_flags: LOGIN
    state: present
  no_log: true

- name: Создать базу приложения
  become_user: postgres
  community.postgresql.postgresql_db:
    login_user: postgres
    name: "{{ postgres_app_database }}"
    owner: "{{ postgres_app_user }}"
    state: present

- name: Разрешить подключение API к PostgreSQL
  ansible.builtin.lineinfile:
    path: /var/lib/pgsql/data/pg_hba.conf
    regexp: '^\s*host\s+{{ postgres_app_database }}\s+{{ postgres_app_user }}\s+127\.0\.0\.1/32\s+'
    line: "host    {{ postgres_app_database }}    {{ postgres_app_user }}    127.0.0.1/32    scram-sha-256"
    insertbefore: '^\s*host\s+all\s+all\s+127\.0\.0\.1/32\s+'
    backup: true
  notify: Перечитать конфигурацию PostgreSQL

- name: Применить настройки PostgreSQL перед настройкой API
  ansible.builtin.meta: flush_handlers
```

### Handler PostgreSQL

Reload применяет настройки к последующим подключениям.

Файл `infra/ansible/roles/postgres/handlers/main.yml`.

```yaml
- name: Перечитать конфигурацию PostgreSQL
  ansible.builtin.systemd_service:
    name: postgresql.service
    state: reloaded
```

### Порядок правил pg_hba.conf

Проверяли правила запросом.

```bash
ssh ec2-user@16.192.61.253 \
  'sudo -u postgres psql -P pager=off -c "SELECT rule_number, type, database, user_name, address, auth_method, error FROM pg_hba_file_rules ORDER BY rule_number;"'
```

На первой VM правило host tickets tickets_app 127.0.0.1/32 scram-sha-256 стояло вторым, перед host all all 127.0.0.1/32 ident. PostgreSQL выбирает первое совпавшее правило. Пустое error подтверждает отсутствие ошибок разбора файла, но само представление не подтверждает применение файла текущим процессом.

Lineinfile ищет специфичную строку через regexp. Если она существует, заменяет её текстом line на текущем месте. Если отсутствует, вставляет перед общей строкой через insertbefore. Если шаблон вставки не найден, lineinfile добавляет строку в конец. Поэтому текущее решение проверено для стандартной структуры Amazon Linux и нашего файла. Произвольное перемещение уже существующего правила выше запрещающего правила этой задачей не обеспечено.

## 10. Nginx и учебный HTTPS

Сначала настроили учебный слушатель 127.0.0.1:18090. При изменении environment_name с lab на staging, затем на training менялся текст ответа и выполнялся handler reload. При повторном запуске handler пропускался.

Затем перенесли реальный прокси tickets.conf, который ранее настроили вручную. Он передаёт запросы на API 127.0.0.1:8080 и слушает HTTP 8081 и HTTPS 8443 на loopback.

### Учебная страница

Строки ответа генерируются из переменных.

Файл `infra/ansible/roles/nginx_lab/templates/ansible-lab.conf.j2`.

```nginx
server {
    listen 127.0.0.1:18090;
    server_name localhost;

    location / {
        default_type text/plain;
        return 200 "Application: {{ application_name }}\nEnvironment: {{ environment_name }}\n";
    }
}
```

### Прокси API

Tickets.invalid — учебное имя. Существование такого server_name в конфигурации не создаёт DNS-запись.

Файл `infra/ansible/roles/nginx_lab/templates/tickets.conf.j2`.

```nginx
server {
    listen 127.0.0.1:8081;
    listen 127.0.0.1:8443 ssl;
    server_name tickets.invalid;

    ssl_certificate     /etc/nginx/tickets-lab.crt;
    ssl_certificate_key /etc/nginx/tickets-lab.key;

    location / {
        proxy_pass http://127.0.0.1:8080;
    }
}
```

### Defaults Nginx

Значения перекрываются group_vars и extra vars.

Файл `infra/ansible/roles/nginx_lab/defaults/main.yml`.

```yaml
application_name: tickets
environment_name: lab
```

### Текущие задачи Nginx

OpenSSL создаёт самоподписанный сертификат на 30 дней для tickets.invalid с Subject Alternative Name. Если отсутствует сертификат или ключ, создаётся новая пара. Истечение срока и соответствие уже существующих файлов автоматически не проверяются. Ключ имеет права 0600, сертификат — 0644. Command argv передаёт аргументы отдельными элементами списка.

Файл `infra/ansible/roles/nginx_lab/tasks/main.yml`.

```yaml
- name: Установить Nginx
  ansible.builtin.package:
    name: nginx
    state: present

- name: Разместить конфигурацию учебной страницы
  ansible.builtin.template:
    src: ansible-lab.conf.j2
    dest: /etc/nginx/conf.d/ansible-lab.conf
    owner: root
    group: root
    mode: '0644'
  notify: Перечитать конфигурацию Nginx

- name: Установить OpenSSL
  ansible.builtin.package:
    name: openssl
    state: present

- name: Собрать сведения об установленных пакетах
  ansible.builtin.package_facts:
    manager: auto

- name: Установить curl при отсутствии обеих версий
  ansible.builtin.package:
    name: curl-minimal
    state: present
  when:
    - "'curl' not in ansible_facts.packages"
    - "'curl-minimal' not in ansible_facts.packages"

- name: Проверить наличие сертификата
  ansible.builtin.stat:
    path: /etc/nginx/tickets-lab.crt
  register: nginx_lab_certificate

- name: Проверить наличие ключа сертификата
  ansible.builtin.stat:
    path: /etc/nginx/tickets-lab.key
  register: nginx_lab_private_key

- name: Создать учебный сертификат и ключ
  ansible.builtin.command:
    argv:
      - openssl
      - req
      - -x509
      - -nodes
      - -newkey
      - rsa:2048
      - -days
      - "30"
      - -keyout
      - /etc/nginx/tickets-lab.key
      - -out
      - /etc/nginx/tickets-lab.crt
      - -subj
      - /CN=tickets.invalid
      - -addext
      - subjectAltName=DNS:tickets.invalid
  when: >
    not nginx_lab_certificate.stat.exists or
    not nginx_lab_private_key.stat.exists
  notify: Перечитать конфигурацию Nginx

- name: Установить права на ключ сертификата
  ansible.builtin.file:
    path: /etc/nginx/tickets-lab.key
    owner: root
    group: root
    mode: '0600'

- name: Установить права на сертификат
  ansible.builtin.file:
    path: /etc/nginx/tickets-lab.crt
    owner: root
    group: root
    mode: '0644'

- name: Разместить конфигурацию прокси API
  ansible.builtin.template:
    src: tickets.conf.j2
    dest: /etc/nginx/conf.d/tickets.conf
    owner: root
    group: root
    mode: '0644'
  notify: Перечитать конфигурацию Nginx

- name: Проверить конфигурацию Nginx
  ansible.builtin.command:
    cmd: nginx -t
  changed_when: false

- name: Запустить Nginx и включить автозапуск
  ansible.builtin.service:
    name: nginx
    state: started
    enabled: true
```

### Handler Nginx

Handler перечитывает конфигурацию после успешной проверки nginx -t.

Файл `infra/ansible/roles/nginx_lab/handlers/main.yml`.

```yaml
- name: Перечитать конфигурацию Nginx
  ansible.builtin.service:
    name: nginx
    state: reloaded
```

### Проверки HTTPS вручную

Сначала проверили ответ с отключённой проверкой сертификата.

```bash
ssh ec2-user@16.192.61.253 \
  'curl --max-time 5 -kfsS https://127.0.0.1:8443/ready'
```

Затем проверили доверие и имя.

```bash
ssh ec2-user@16.192.61.253 \
  'curl --max-time 5 -fsS \
    --cacert /etc/nginx/tickets-lab.crt \
    --resolve tickets.invalid:8443:127.0.0.1 \
    https://tickets.invalid:8443/ready'
```

Cacert задаёт доверенный сертификат для одного запроса. Resolve направляет tickets.invalid на loopback, сохраняя имя в URL для TLS. На обе проверки получили JSON со status=ready. Вторую команду перенесли в автоматические post_tasks.

## 11. Проверки результата

В site.yml после ролей выполняются проверки базы, готовности API, прокси и HTTPS. Uri выполняет HTTP-запрос с VM, поэтому localhost относится к ней. Status_code задаёт ожидаемый код. Register сохраняет ответ, return_content позволяет читать тело. Для JSON-ответов uri предоставляет поле json.

Until повторяет задачу до выполнения условия, retries ограничивает повторы, delay задаёт паузу. Проверка готовности API ждёт HTTP 200. Assert останавливает playbook, если условие ложно. Для ответа curl фильтр from_json преобразует stdout в объект.

### Полный site.yml

Порядок ролей users → postgres → app → nginx_lab обеспечивает подготовку пользователей и базы до приложения. Post_tasks проверяют фактический результат. Внутри роли postgres есть ранний flush_handlers, а перед финальными проверками выполняется общий flush.

Файл `infra/ansible/site.yml`.

```yaml
- name: Настроить учебную VM
  hosts: lab
  become: true
  gather_facts: true

  roles:
    - users
    - postgres
    - app
    - nginx_lab

  post_tasks:
    - name: Показать сведения о VM
      ansible.builtin.debug:
        msg:
          - "ОС: {{ ansible_facts['distribution'] }}"
          - "Версия ОС: {{ ansible_facts['distribution_version'] }}"
          - "Архитектура: {{ ansible_facts['architecture'] }}"
          - "Менеджер пакетов: {{ ansible_facts['pkg_mgr'] }}"

    - name: Применить отложенные обработчики
      ansible.builtin.meta: flush_handlers

    - name: Проверить подключение к базе с учётными данными API
      community.postgresql.postgresql_ping:
        login_host: 127.0.0.1
        login_user: "{{ postgres_app_user }}"
        login_password: "{{ vault_postgres_app_password }}"
        login_db: "{{ postgres_app_database }}"
      register: app_database_connection
      no_log: true

    - name: Подтвердить доступ приложения к базе
      ansible.builtin.assert:
        that:
          - app_database_connection.is_available
        success_msg: Подключение к базе с учётными данными API работает.
        fail_msg: Не удалось подключиться к базе с учётными данными API.

    - name: Дождаться готовности API
      ansible.builtin.uri:
        url: http://127.0.0.1:8080/ready
        status_code: 200
      register: app_readiness
      until: app_readiness.status == 200
      retries: 10
      delay: 2

    - name: Проверить готовность API через Nginx
      ansible.builtin.uri:
        url: http://127.0.0.1:8081/ready
        status_code: 200
        return_content: true
      register: proxy_readiness

    - name: Проверить ответ API через Nginx
      ansible.builtin.assert:
        that:
          - proxy_readiness.json.status == 'ready'
        success_msg: Nginx передаёт запросы готовности в API.

    - name: Запросить учебную страницу Nginx
      ansible.builtin.uri:
        url: http://127.0.0.1:18090/
        status_code: 200
        return_content: true
      register: nginx_response

    - name: Проверить ответ Nginx
      ansible.builtin.assert:
        that:
          - "('Application: ' ~ application_name) in nginx_response.content"
          - "('Environment: ' ~ environment_name) in nginx_response.content"
        success_msg: Nginx отвечает с ожидаемой конфигурацией.
        fail_msg: Ответ Nginx отличается от ожидаемого.

    - name: Проверить API через HTTPS с проверкой сертификата
      ansible.builtin.command:
        argv:
          - curl
          - --max-time
          - "5"
          - --fail
          - --silent
          - --show-error
          - --cacert
          - /etc/nginx/tickets-lab.crt
          - --resolve
          - tickets.invalid:8443:127.0.0.1
          - https://tickets.invalid:8443/ready
      register: app_https_response
      changed_when: false

    - name: Проверить содержимое HTTPS-ответа
      ansible.builtin.assert:
        that:
          - (app_https_response.stdout | from_json).status == 'ready'
        success_msg: API доступен через HTTPS с проверкой сертификата.
```

## 12. Эксперименты с отказами

### Остановка Nginx

На первой VM остановили Nginx и проверили ошибку curl.

```bash
ssh ec2-user@16.192.61.253 'sudo systemctl stop nginx'
ssh ec2-user@16.192.61.253 'curl --max-time 3 -fsS http://127.0.0.1:18090/'
```

После запуска site.yml задача state=started запустила службу, HTTP-проверка прошла. Следующий прогон дал changed=0. Этот случай показывает, почему запуск службы должен быть обычной задачей роли — обработчик изменения файла может не получить уведомления, когда конфигурация уже совпадает.

### Остановка PostgreSQL

Остановили базу и проверили /ready. Ожидался HTTP 503. Playbook с одной проверкой готовности выполнял повторы и завершался failed. После ручного systemctl start PostgreSQL проверка прошла.

Затем создали роль postgres с state=started и enabled=true. Повторили остановку. Теперь playbook сам запускал базу, после чего /ready проходил. Пользователь подтвердил успешное восстановление.

```bash
ssh ec2-user@16.192.61.253 'sudo systemctl stop postgresql.service'
# Далее запускали site.yml для tickets и проверяли результат.
```

Эти команды останавливают зависимость приложения. Их запуск относится к учебной VM и выполняется с последующим восстановлением.

## 13. Создание чистой VM в AWS

В браузере открыли EC2 Console в регионе eu-north-1. Существующая VM linux-test имела адрес 16.192.61.253. Для отдельного эксперимента подготовили новую VM.

1. Нажали Launch instances.
2. Указали имя tickets-ansible-clean.
3. Выбрали официальный Amazon Linux 2023 x86_64, AMI ami-09853ee25db7186f2, образ 2023.12.20261119.0 с kernel 6.18.
4. Выбрали t3.micro, 2 vCPU, 1 GiB RAM и один диск 8 GiB gp3. Диск в форме был без шифрования. Режим CPU credits остался стандартным Unlimited.
5. Выбрали существующую ключевую пару linux-test, соответствующую локальному linux-test.pem.
6. Оставили default VPC с автоматическим публичным IPv4, без дополнительной IAM-роли.
7. Вместо существующей группы с SSH от 0.0.0.0/0 подготовили отдельную security group. Через SSH_CONNECTION первой VM определили исходящий IPv4 Mac — 89.125.22.80. Разрешили TCP 22 от 89.125.22.80/32. Внешние HTTP и HTTPS в форме не включали.
8. Проверили AWS Budgets. Бюджетов не было.
9. Создали Learning-monthly-5USD на $5 в месяц для всего аккаунта с уведомлениями на указанный пользователем email. Шаблон уведомляет о фактических 85%, 100% и прогнозе 100%. На момент создания консоль показывала использованные $1.86. Это значение аккаунта на дату упражнения.
10. После подтверждения пользователя запустили экземпляр.

Консоль показывала базовую стоимость t3.micro $0.0108/час. Это цена вычислений, диск и публичный IPv4 учитываются отдельно. Тариф и текущие льготы нужно проверять перед будущими запусками. Бюджет $5 выбран как порог уведомлений аккаунта. Он не является автоматическим пределом оплаты или механизмом остановки.

Получили экземпляр i-00e7c5b9d14aef508, публичный IP 13.60.98.7, приватный IP 172.31.36.53, состояние Running. SSH-проверка вернула ec2-user, Python 3.9.25 и Amazon Linux 2023.

```bash
ssh -i ~/.ssh/linux-test.pem ec2-user@13.60.98.7
```

При первом диагностическом подключении использовали StrictHostKeyChecking=accept-new, поэтому новый host key был сохранён в known_hosts. Публичный адрес может измениться после stop/start. Разрешённый исходящий адрес Mac также может измениться при смене сети или VPN.

## 14. Первое развёртывание и исправления

Добавили tickets-clean в группу lab. Проверили выбор через --list-hosts, затем применили роли только к новой VM.

```bash
ansible-playbook -i infra/ansible/inventory.ini infra/ansible/site.yml \
  --limit tickets-clean --list-hosts --ask-vault-pass

ansible-playbook -i infra/ansible/inventory.ini infra/ansible/site.yml \
  --limit tickets-clean -e environment_name=training --ask-vault-pass
```

### Конфликт curl и curl-minimal

Первый прогон дошёл до роли nginx_lab и завершился Depsolve Error. В чистом образе уже был curl-minimal, а задача требовала конфликтующий пакет curl.

Исправили роль — отдельно устанавливаем openssl, собираем package_facts и устанавливаем curl-minimal только при отсутствии обоих пакетов. Через curl --version на новой VM подтвердили поддержку HTTP и HTTPS. Фактическая версия была curl 8.21.0 с OpenSSL 3.5.8.

### Правило записано, но reload не выполнен

После исправления curl прогон дошёл до проверки базы и получил Ident authentication failed for user tickets_app. Проверили pg_hba.conf — правило scram-sha-256 уже стояло перед ident. Проверили SHOW hba_file и pg_conf_load_time — PostgreSQL продолжал использовать конфигурацию со времени запуска. API был в failed.

Причина — первый прогон записал файл и поставил reload в очередь, затем остановился на другой задаче до выполнения handlers. При повторном запуске файл уже совпал с line, задача вернула ok и уведомление не возникло. Очередь handlers между запусками не сохраняется.

Добавили flush_handlers в конец роли postgres. Для текущей VM уже потерянное уведомление восстановили ручным reload и перезапуском API.

```bash
ssh -i ~/.ssh/linux-test.pem ec2-user@13.60.98.7 \
  'sudo systemctl reload postgresql.service && sudo systemctl restart tickets-api.service && sleep 2 && curl -fsS http://127.0.0.1:8080/ready'
```

API вернул status=ready. Ранний flush предотвращает задержку reload до более поздних ролей в нормальном первом прогоне. Он не восстанавливает уведомления, потерянные при любом произвольном аварийном завершении между изменением файла и flush. Такой общий сценарий требует дополнительного механизма согласования.

Повторный полный прогон подтвердил результат.

```text
tickets-clean : ok=50 changed=0 unreachable=0 failed=0 skipped=6 rescued=0 ignored=0
```

Это фактический итог в тот момент. После изменения числа задач счётчики ok и skipped могут отличаться. Важные признаки — отсутствие failed и unreachable и changed=0 при повторном применении.

## 15. Smoke-тест и перезагрузка

Скопировали существующий тест проекта и запустили через Nginx на новой VM.

```bash
scp -i ~/.ssh/linux-test.pem deploy/smoke.sh \
  ec2-user@13.60.98.7:/tmp/tickets-smoke.sh

ssh -i ~/.ssh/linux-test.pem ec2-user@13.60.98.7 \
  'BASE_URL=http://127.0.0.1:8081 bash /tmp/tickets-smoke.sh'
```

Тест проверяет /ready, создаёт бронь на night-jazz с новым Idempotency-Key, повторяет запрос и сравнивает ID, читает active, отменяет бронь и проверяет cancelled. Повтор исходного запроса после отмены должен вернуть прежний ID. При ошибке trap пытается отменить созданную бронь.

Получили подтверждённый результат.

```text
Smoke test passed: cfa985fd-24cd-4299-a2a6-5ea8cd78e375
```

Далее перезагрузили VM.

```bash
ssh -i ~/.ssh/linux-test.pem ec2-user@13.60.98.7 'sudo systemctl reboot'
```

После восстановления SSH проверили службы и прокси без предварительного запуска Ansible.

```bash
ssh -i ~/.ssh/linux-test.pem ec2-user@13.60.98.7 \
  'systemctl is-active postgresql tickets-api nginx; curl --max-time 5 -fsS http://127.0.0.1:8081/ready'
```

Пользователь подтвердил успех — службы поднялись и /ready прошёл. Проверка выполнена один раз, она не доказывает отсутствие всех возможных гонок готовности PostgreSQL при старте.

## 16. Копирование и восстановление базы

### Создание логической копии на Mac

На новой VM есть база tickets с мероприятиями, миграциями и бронью smoke-теста. Выполнили pg_dump и направили его вывод в локальный файл.

```bash
backup_dir=$(mktemp -d "$HOME/tickets-backup.XXXXXX")

ssh -i ~/.ssh/linux-test.pem ec2-user@13.60.98.7 \
  'sudo -u postgres pg_dump --format=custom tickets' \
  > "$backup_dir/tickets.dump"
```

Pg_dump выполняется на VM, а перенаправление > расположено снаружи SSH-кавычек и выполняется оболочкой Mac. Полученный архив содержит логическое описание объектов и данные на момент выгрузки. Он хранится отдельно от VM. Каталог mktemp создаётся с доступом для владельца. Сам архив содержит данные приложения, включая ключи идемпотентности бронирований.

Фактический путь в этом упражнении — /Users/kinzaz/tickets-backup.4tmP62/tickets.dump.

### Проверка архива

```bash
ls -lh /Users/kinzaz/tickets-backup.4tmP62/tickets.dump

ssh -i ~/.ssh/linux-test.pem ec2-user@13.60.98.7 \
  'pg_restore --list' \
  < /Users/kinzaz/tickets-backup.4tmP62/tickets.dump
```

Знак < передаёт архив с Mac в стандартный ввод pg_restore на VM. List читает оглавление без восстановления данных. Пользователь показал строки INDEX reservations_expiry и FK CONSTRAINT reservations_event_id_fkey. Они подтвердили читаемость оглавления. Полноту пригодности копии затем проверили реальным восстановлением.

В SQL-схеме брони находятся в reservations. HTTP-маршрут называется /api/bookings. Ранее в объяснении была ошибочно названа таблица bookings, это исправили после сверки миграции 001.

### Отдельный PostgreSQL на Mac

```bash
docker volume create tickets-restore-data

docker run -d --name tickets-restore-db \
  --mount source=tickets-restore-data,target=/var/lib/postgresql/data \
  -e POSTGRES_USER=tickets_app \
  -e POSTGRES_PASSWORD=restore-lab-only \
  -e POSTGRES_DB=tickets_restore \
  postgres:16-alpine

docker exec tickets-restore-db \
  pg_isready -U tickets_app -d tickets_restore
```

Создали отдельный экземпляр PostgreSQL 16 со своим томом и базой tickets_restore. Порт на Mac не публиковали. Учебный пароль restore-lab-only относится к этому экземпляру. В официальном Docker-образе POSTGRES_USER при первой инициализации создаёт административного пользователя, поэтому локальный tickets_app здесь обладает более широкими правами, чем роль приложения на EC2. Эта проверка подтверждает восстановление данных, точное воспроизведение прав отдельно не проверяли.

Переменные POSTGRES_* применяются при инициализации пустого тома. Повторный запуск с уже заполненным томом не создаёт новую чистую базу автоматически. Все команды ниже относятся к первому выполненному упражнению.

### Восстановление

```bash
docker exec -i tickets-restore-db \
  pg_restore -U tickets_app -d tickets_restore \
    --exit-on-error --single-transaction \
  < /Users/kinzaz/tickets-backup.4tmP62/tickets.dump
```

Docker exec -i сохраняет стандартный ввод. Pg_restore читает архив, создаёт таблицы, заполняет данные и восстанавливает индексы и ограничения. Exit-on-error останавливает работу при ошибке. Single-transaction выполняет восстановление одной транзакцией, поэтому изменения неудачной попытки откатываются. Повторное применение в уже восстановленную базу может встретить существующие объекты, для нового упражнения нужна отдельная пустая база или том.

### Проверка прежней брони через SQL

```bash
docker exec tickets-restore-db \
  psql -U tickets_app -d tickets_restore \
  -c "SELECT id, event_id, quantity, status
      FROM reservations
      WHERE id = 'cfa985fd-24cd-4299-a2a6-5ea8cd78e375';"
```

Получили одну строку.

```text
id                                   | event_id   | quantity | status
cfa985fd-24cd-4299-a2a6-5ea8cd78e375 | night-jazz | 1        | cancelled
```

Cancelled ожидается, поскольку smoke-тест отменил бронь перед завершением.

### Проверка через отдельный API

```bash
docker network create tickets-restore
docker network connect tickets-restore tickets-restore-db

docker build --target api -t tickets-api:restore ./app

docker run -d --name tickets-restore-api \
  --network tickets-restore \
  -p 127.0.0.1:18083:8080 \
  -e 'DATABASE_URL=postgres://tickets_app:restore-lab-only@tickets-restore-db:5432/tickets_restore?sslmode=disable' \
  tickets-api:restore

curl -fsS http://127.0.0.1:18083/ready
curl -fsS http://127.0.0.1:18083/api/bookings/cfa985fd-24cd-4299-a2a6-5ea8cd78e375
```

Сеть позволяет API найти PostgreSQL по имени контейнера. DATABASE_URL направляет его в восстановленную базу. API при старте проверяет миграции, но SEED_DEMO в этом контейнере не включали. Пользователь подтвердил готовность и чтение прежней брони через HTTP.

```text
PostgreSQL на EC2 → pg_dump → архив на Mac
                               ↓ pg_restore
                         PostgreSQL в Docker
                               ↑
                         отдельный API
                               ↑
                         HTTP-запрос с Mac
```

### Что именно восстановили

Восстановили базу приложения на момент pg_dump. Изменения, сделанные позже, в архив не входят. Файловая система VM, конфигурация Nginx, systemd, SSH и пароль Vault в такой архив не включаются. Для подготовки среды используются исходники и Ansible, для доступа к секретам нужен пароль Vault. Новое окружение базы на Mac использует отдельный учебный пароль.

## 17. Результаты и ограничения

| Проверка | Фактический результат |
| --- | --- |
| SSH и Python на обеих VM | Подключение проверено |
| Роли users, postgres, app, nginx_lab | Применены к новой VM |
| Повторный прогон на новой VM | changed=0, failed=0 |
| Восстановление остановленных служб | Проверено в упражнениях |
| Парольное подключение tickets_app | Проверено отдельным модулем |
| Локальный HTTP и учебный HTTPS | Проверены |
| Smoke-тест на новой VM | Прошёл |
| Автозапуск после перезагрузки | Подтверждён пользователем |
| Копия базы отдельно от VM | Сохранена на Mac |
| Восстановление в другом экземпляре PostgreSQL | Выполнено |
| Чтение прежней брони через SQL и API | Подтверждено |
| Полностью непрерывный первый прогон после всех исправлений на ещё одной чистой VM | Ещё не проверен |
| Публичный DNS, доверенный публичный HTTPS и внешний smoke-тест | Предстоят |

### Обнаруженные ограничения текущей реализации

- Маркер сборки и уведомления handlers не гарантируют восстановление после любого возможного прерывания. Подробности описаны в разделах сборки и первого развёртывания.
- Учебный TLS-сертификат не продлевается автоматически. Генерация проверяет наличие файлов, а не их срок и соответствие.
- Служба API имеет After=postgresql.service, но автоматический повтор запуска после неготовности базы в unit-файле пока не настроен.
- Слушатели, которыми управляет наша конфигурация Nginx, привязаны к loopback. Полная настройка стандартного server-блока из nginx.conf на чистой VM ещё не перенесена в роль. Security group новой VM пропускает входящий SSH с выбранного адреса.
- Конспект фиксирует успешную проверку одной брони. Измерения времени восстановления, допустимой потери данных, автоматическое расписание копирования и проверка всей базы пока не проводились.

### Ресурсы после упражнения

На момент последнего подтверждения новая EC2 оставалась запущенной. Исходная linux-test также использовалась в курсе. На Mac созданы tickets-restore-db, tickets-restore-api, том tickets-restore-data, сеть tickets-restore и образ tickets-api:restore. Их удаление в этом упражнении не выполнялось.

После завершения работы предстоит удалить ненужные EC2 и отдельно проверить диски, снимки, публичные адреса и балансировщики. Бюджетные уведомления не останавливают ресурсы. Перед удалением нужно решить, какие данные и артефакты сохранить.

### Следующий этап курса

Выбрать доменное имя, настроить DNS на новую VM, перенести внешние слушатели Nginx в Ansible, открыть необходимые внешние порты, получить доверенный сертификат и выполнить smoke-тест снаружи. На момент составления конспекта домен ещё не выбран.


<img src="https://repository-images.githubusercontent.com/991835536/72ff229d-e789-4fc8-883d-53439aab3c0d" align="right" width="55%">

# 🇬🇧 `postgresql-backup`  
<a href="#-русский">🇷🇺 Инструкция на Русском</a>

## English

### 📦 Overview
`postgresql-backup` is a single-binary Go utility that performs **hot physical backups** of a local PostgreSQL cluster.  
It uses the native SQL functions `pg_backup_start/stop` (or `pg_start/stop_backup` on Pg ≤ 14), recursively archives the cluster’s `data_directory` into a compressed `tar.gz`, and keeps a rotating set of daily/weekly/monthly/yearly snapshots.  
Backups can be **replicated to any number of FTP servers** for off-site storage.

### ✨ Features
* Hot (online) base-backup — no downtime  
* Compressed `tar.gz` archives with UNIX owners/permissions preserved  
* Retention tiers: **daily, weekly, monthly, yearly**  
* Local rotation by count (`--copies`) or by age (`--days`)  
* **Multi-FTP replication** with independent retention (`--ftp-keep-factor`)  
* Colourized, human-friendly terminal output  
* Cross-platform (Linux, macOS, *BSD, …) — pure Go, no shell commands

### 🚀 Quick start
```bash
sudo postgresql-backup --dsn "user=postgres host=/var/run/postgresql sslmode=disable"
````

### 🔧 Usage

```bash
postgresql-backup [flags]
```

| Flag                | Description                                               | Default                         |
| ------------------- | --------------------------------------------------------- | ------------------------------- |
| `--dsn`             | PostgreSQL DSN (connection string)                        | local UNIX socket as `postgres` |
| `--backup-path`     | Root folder for backups                                   | `/backup`                       |
| `--days`            | Delete daily archives older than *N* days (0 = never)     | `30`                            |
| `--copies`, `-c`    | Keep only the newest *N* daily archives (0 = unlimited)   | `0`                             |
| `--list`            | List existing archives and exit                           | –                               |
| `--help`            | Show help and exit                                        | –                               |
| **FTP replication** |                                                           |                                 |
| `--ftp-conf`        | Credentials file with one or **multiple** FTP blocks      | `/etc/ftp-backup.conf`          |
| `--ftp-host`        | Override FTP host (single-target quick setup)             | –                               |
| `--ftp-user`        | Override FTP username                                     | –                               |
| `--ftp-pass`        | Override FTP password                                     | –                               |
| `--ftp-keep-factor` | Remote retention = `days × factor` (or `copies × factor`) | `4`                             |

> **Tip:** when `--copies 1` is used, the default `--ftp-keep-factor`
> automatically increases to **4**, so you still keep four off-site copies.

### 🗄️ Directory layout

```
/backup/<hostname>/postgresql-backup/
└── cluster/
    ├── daily/
    ├── weekly/
    ├── monthly/
    └── yearly/
```

Archive name format: `YYYY-MM-DD_HH-MM-SS_cluster.tar.gz`

### 🌐 Multi-FTP configuration

`postgresql-backup` reads **one or many** account blocks from *ftp-conf*
(variables inside each block are mandatory):

```conf
FTP_HOST=backup1.example.com
FTP_USER=alice
FTP_PASS=s3cret

FTP_HOST=backup2.example.net
FTP_USER=bob
FTP_PASS=pa55w0rd
```

The same archive is uploaded to **every** listed host; retention is enforced
independently on each server.

### 🔧 Installation

Pre-built binaries are available on the
[Releases page](https://github.com/matveynator/postgresql-backup/releases).
Download the file for your platform, place it in `/usr/local/bin/`
and make it executable (`chmod +x`).

### ♻️ Restore

The archive is a regular `tar.gz` of the cluster’s `data_directory`.
To restore:

1. Stop PostgreSQL.
2. Move the old `data_directory` aside.
3. Extract:

   ```bash
   tar xzf YYYY-MM-DD_HH-MM-SS_cluster.tar.gz -C /var/lib/postgresql
   chown -R postgres:postgres /var/lib/postgresql
   ```
4. Start PostgreSQL and run `pg_wal_replay_resume()` if needed.

---

# 🇷🇺 Русский

### 📦 Обзор

`postgresql-backup` — самостоятельная Go-утилита для **«горячего»** (онлайн) физического
бэкапа кластера PostgreSQL.
Запускает `pg_backup_start/stop`, пакует `data_directory` в `tar.gz`, хранит
ежедневные/еженедельные/ежемесячные/годовые копии и умеет отправлять их
**на несколько FTP-серверов**.

### ✨ Возможности

* Онлайн base-backup без простоя
* Сжатие `tar.gz`, сохранение владельцев/прав
* Схема хранения: день/неделя/месяц/год
* Ротация по количеству (`--copies`) или по возрасту (`--days`)
* Репликация на **несколько** FTP-хостов, отдельная ротация (`--ftp-keep-factor`)
* Цветной вывод, кроссплатформенность, чистый Go

### 🚀 Быстрый старт

```bash
sudo postgresql-backup --dsn "user=postgres host=/var/run/postgresql sslmode=disable"
```

### 🔧 Флаги

| Флаг                   | Описание                                                    | По умолчанию           |
| ---------------------- | ----------------------------------------------------------- | ---------------------- |
| `--dsn`                | Строка подключения к PostgreSQL                             | локальный сокет        |
| `--backup-path`        | Корневая папка для бэкапов                                  | `/backup`              |
| `--days`               | Удалять daily-архивы старше *N* дней (0 = не удалять)       | `30`                   |
| `--copies`, `-c`       | Хранить только *N* последних daily-архивов (0 = без лимита) | `0`                    |
| `--list` / `--help`    | Показать архивы / справку и выйти                           | –                      |
| **FTP**                |                                                             |                        |
| `--ftp-conf`           | Файл с одной или **несколькими** FTP-учётками               | `/etc/ftp-backup.conf` |
| `--ftp-host/user/pass` | Быстрая настройка для одного FTP                            | –                      |
| `--ftp-keep-factor`    | Срок хранения на FTP = `дни × factor` или `copies × factor` | `4`                    |

### 🌐 Пример *ftp-conf* с несколькими хостами

```
FTP_HOST=backup1.example.com
FTP_USER=alice
FTP_PASS=s3cret

FTP_HOST=backup2.example.net
FTP_USER=bob
FTP_PASS=pa55w0rd
```

### 🔧 Установка

Скачайте готовый бинарник с вкладки
[Releases](https://github.com/matveynator/postgresql-backup/releases),
положите в `/usr/local/bin/` и сделайте исполняемым:

```bash
sudo install -m 755 postgresql-backup_* /usr/local/bin/postgresql-backup
```

### 🔄 Восстановление

1. Остановите PostgreSQL.
2. Переместите старый `data_directory` в резерв.
3. Распакуйте архив:

   ```bash
   tar xzf YYYY-MM-DD_HH-MM-SS_cluster.tar.gz -C /var/lib/postgresql
   chown -R postgres:postgres /var/lib/postgresql
   ```
4. Запустите PostgreSQL; при необходимости выполните `pg_wal_replay_resume()`.

---

## 📝 License

This project is licensed under the GNU General Public License (GPL).

```

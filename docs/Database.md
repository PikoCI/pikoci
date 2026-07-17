---
description: Supported database backends for PikoCI. In-memory, SQLite, MySQL, and PostgreSQL with migration and backup options.
---

# Database Backends

PikoCI supports multiple database backends via the `--db-system` flag.

## mem (default)

In-memory storage. All data is lost when the server stops. Useful for development and testing.

```bash
pikoci server --jwt-secret my-secret --db-system mem
```

No additional flags required.

## sqlite

Embedded SQLite database. Data persists in a single file. Good for single-node deployments.

```bash
pikoci server --jwt-secret my-secret --db-system sqlite --db-name pikoci.db
```

| Flag | Description |
|------|-------------|
| `--db-name` | Path to the SQLite database file |

PikoCI automatically enables [WAL mode](https://www.sqlite.org/wal.html) and a 5-second `busy_timeout` on file-backed SQLite connections. WAL allows concurrent reads during writes, and `busy_timeout` prevents immediate `SQLITE_BUSY` errors when multiple workers access the database simultaneously. In-memory databases (`mem`) only use `busy_timeout` since WAL requires a file on disk.

## mysql

MySQL or MariaDB.

```bash
pikoci server \
  --jwt-secret my-secret \
  --db-system mysql \
  --db-host 127.0.0.1 \
  --db-port 3306 \
  --db-user pikoci \
  --db-password secret \
  --db-name pikoci
```

| Flag | Description |
|------|-------------|
| `--db-host` | MySQL host |
| `--db-port` | MySQL port |
| `--db-user` | MySQL user |
| `--db-password` | MySQL password |
| `--db-name` | Database name |

## postgresql

PostgreSQL.

```bash
pikoci server \
  --jwt-secret my-secret \
  --db-system postgresql \
  --db-host 127.0.0.1 \
  --db-port 5432 \
  --db-user pikoci \
  --db-password secret \
  --db-name pikoci
```

| Flag | Description |
|------|-------------|
| `--db-host` | PostgreSQL host |
| `--db-port` | PostgreSQL port |
| `--db-user` | PostgreSQL user |
| `--db-password` | PostgreSQL password |
| `--db-name` | Database name |

## Migrations

Migrations run automatically on startup by default. To disable:

```bash
pikoci server --run-migrations=false ...
```

The initial migration (V8) seeds the default user `admin` / `admin123`.

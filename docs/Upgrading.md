---
description: "Version-by-version upgrade notes for PikoCI, covering the releases that need a manual step and the commands to run."
---

# Upgrading

Notes for the releases that need a manual step. A release not listed here needs none: replace the binary and restart.

Sections are newest first. Anything under **Unreleased** applies to the next release and is not part of a published version yet.

## [Unreleased]

### SQLite `DB_NAME` is now honoured

Affects servers on SQLite that set `DB_NAME` or `--db-name`. That includes every server configured from `deploy/pikoci.env.example`, which shipped with `DB_NAME=/var/lib/pikoci/pikoci.db`.

Earlier versions ignored the setting for SQLite and always stored the database at the [XDG data path](https://specifications.freedesktop.org/basedir-spec/latest/), which on a systemd install is `/var/lib/pikoci/.local/share/pikoci/pikoci.db`. The setting now takes effect, so the server opens the path you name.

Nothing is lost if you do nothing. What happens depends on what is already at the named path:

| At the named path | Result |
|------|--------|
| Nothing | The database already in use is kept, and a warning names both paths |
| An empty file | The empty file is ignored, the database already in use is kept, and a warning names both paths |
| A file the server cannot write | The server refuses to start, with an error naming the file |
| Another database | That database is opened, and the data in the old one will appear to be missing |

So the only required action is to check that `DB_NAME` does not point at a *different* database. Either leave it unset to keep the XDG path, or move your database to the path you named.

#### If the server fails to start

A named file that the server cannot write produces this, and under `Restart=always` it repeats indefinitely while the reverse proxy serves 502:

```
Error: failed to run migrations: error while migrating: attempt to write a readonly database (8)
```

This means the file at `DB_NAME` exists but belongs to another user, typically a leftover created by `root`. Check it:

```bash
ls -l /var/lib/pikoci/pikoci.db
```

!!! danger "Do not fix this by changing the file's owner"
    Giving the server ownership of an empty file makes it start cleanly on an empty database, and your pipelines and build history will look like they are gone. The data is still at the XDG path. Delete the empty file instead, or move your database onto it as shown below.

If the file is empty, deleting it is enough. The server then falls back to the database it was already using:

```bash
rm /var/lib/pikoci/pikoci.db
systemctl restart pikoci
```

#### Moving the database to the path you named

Stop the server first, and move the `-wal` and `-shm` files along with the database. The `-wal` file can hold the most recent writes, so the server refuses to start if the database is moved without it:

```bash
systemctl stop pikoci
rm -f /var/lib/pikoci/pikoci.db          # remove any empty leftover at the target
mv /var/lib/pikoci/.local/share/pikoci/pikoci.db* /var/lib/pikoci/
chmod 700 /var/lib/pikoci
systemctl start pikoci
```

The `chmod` keeps other local users out. The database files are readable by everyone, and the old XDG directory was the only thing protecting them.

Confirm which file the server opened:

```bash
journalctl -u pikoci -n 50 | grep "Using SQLite database"
```

See [Database Backends](Database.md#sqlite) for the `--db-name` reference.

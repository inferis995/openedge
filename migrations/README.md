# Two migration paths, and which one you want

This directory is **not** the migration system. It is mounted at
`/docker-entrypoint-initdb.d`, which means Postgres executes these files
**exactly once — when it initialises an empty data directory** — and never
again. A file added here today will run on installs created tomorrow and on no
existing one, ever.

The migration system is `runAutoMigrations()` in `internal/db/db.go`. It runs on
every start of every service, and it is what reaches deployments that already
exist.

## Which to use

**Adding or changing anything: use `runAutoMigrations()`.** No exceptions worth
the risk. Adding a `.sql` file here instead produces a change that works
perfectly on your laptop — where you keep recreating the volume — and never
appears on a single customer machine.

This directory stays because the ten tables at the root of the schema
(`organizations`, `sites`, `areas`, `gateways`, `tags`, `users`,
`global_settings`, `access_logs`, `user_sites`, `user_areas`) are declared only
in `20250308_schema.sql`. Removing the mount would leave a fresh install with
no schema at all. Treat these files as the seed of a new database, not as a
changelog.

## The trap this cost us once

`20250308_schema.sql` used to end with:

```sql
SELECT add_retention_policy('tag_history', INTERVAL '90 days', if_not_exists => TRUE);
```

The application seeds `historian_retention_days = 365`, displays it in the UI,
and runs a worker against it. But initdb happens before core-api has ever
started, so the 90-day policy was installed first — and `if_not_exists` meant
every later attempt to correct it was a no-op.

The database dropped chunks at 90 days while every operator-facing surface said
a year. What made it invisible rather than merely wrong: the worker's `DELETE`
at 365 days then found nothing to delete, and an empty result is exactly what a
correctly-aged table returns too.

The lesson generalises to anything configurable. If a value can be changed by an
operator, it cannot be frozen into a file that runs once at database creation.
Put it where the code that reads the setting can re-apply it — for retention,
that is `db.EnsureRetentionPolicies`, called on every core-api start.

## Before adding a file here

Ask what happens on a machine that is already running. If the answer is
"nothing", the change belongs in `runAutoMigrations()`.

# Database model review

This review describes the SQLite model shipped in War Room 0.0.1 and the persistence rules enforced by the application.

## Relationships and storage

`jobs` is the aggregate root. Each job owns ordered `stages`; each stage owns ordered `stage_questions`. Attachments belong to a job and may optionally belong to a stage. Foreign keys cascade job and stage deletion to their dependent rows, while deleting a stage clears an attachment's optional stage reference. This matches the application's lifecycle and avoids orphan records.

Interviewers are stored as compact JSON arrays on jobs and stages. They are small, edited with their owning record, and are not queried independently, so keeping them embedded avoids joins and extra write coordination. The Go models expose the typed interviewer arrays and hide the legacy JSON storage columns. If the application later needs to search, share, or independently audit interviewers, migrate them to relational child tables.

Salary amounts and timestamps use SQLite integers. Salary ranges reject a minimum greater than a maximum. Work arrangement and employment type are stored as validated enums; old records migrate to `unknown` until the user specifies them. Optional fields use nullable SQL columns and pointer fields in Go; boolean API fields use Go `bool` values mapped to SQLite `0`/`1`. Current-stage labels, counts, calendar rows, and other joined values are read projections, not duplicated persisted state.

Attachments use a generated, unique stored filename. The absolute-path column remains for compatibility with existing databases, but new rows put only the stored filename there, Go no longer reads it, and the API never returns it. File reads and deletion derive paths from the configured attachment directory plus a validated basename. Attachment bytes live in the Docker data volume; JSON snapshots restore metadata only when the corresponding regular file is still in that volume.

## Constraints, migrations, and indexes

New databases constrain job, stage, meeting, salary, recruiter, employment, and boolean values, salary ordering, and non-negative attachment sizes. Versioned migrations add legacy columns, reject unsupported future schema versions, create validation triggers for older tables that cannot acquire new `CHECK` clauses without rebuilding, enforce attachment stage ownership, and replace redundant single-column indexes with indexes that match the list, stage-order, question-order, and attachment-order queries. Migration 3 normalizes invalid or missing legacy meeting types to `video`, then restricts stored meeting types to `video`, `phone`, or `onsite` on both inserts and updates. Migration 6 adds employment type without employer-specific defaults.

The SQLite driver receives foreign-key enforcement, WAL mode, normal synchronous mode, and a five-second busy timeout in its DSN. Those settings are therefore applied to every pooled connection. A failed connection, schema operation, or migration stops startup instead of silently running with weakened integrity.

## Operational limits and follow-up

The database is an embedded, single-instance SQLite store. WAL supports concurrent readers and serializes writers; the app's normal access pattern is a small number of reads and short writes. Search uses leading-and-trailing wildcard matching, which is appropriate for the current small board but cannot use ordinary B-tree indexes. Add SQLite FTS only if measured job counts or search latency justify the extra synchronization and migration cost.

`backup.json` is a recovery snapshot of job and interview-process data. It is size-limited, decoded before insertion, and restored in one transaction; any invalid row rolls the whole restore back. It does not contain attachment bytes. The persistent Docker volume remains the authoritative store for the database and uploaded files.

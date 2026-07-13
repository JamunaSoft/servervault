package job

// migration is one forward-only schema change, applied in order and
// recorded in schema_migrations so Open never re-applies it. There are no
// down migrations here: this is purely local, disposable operational
// state (job history), not data an operator would ever need to roll back
// -- losing it just means losing history, never live backup data.
type migration struct {
	version int
	sql     string
}

// migrations is the ordered, append-only list of schema changes. Add new
// entries at the end with the next version number; never edit or remove
// an existing entry once it has shipped.
var migrations = []migration{
	{
		version: 1,
		sql: `
CREATE TABLE jobs (
	id             TEXT PRIMARY KEY,
	type           TEXT NOT NULL,
	state          TEXT NOT NULL,
	created_at     TEXT NOT NULL,
	updated_at     TEXT NOT NULL,
	started_at     TEXT NOT NULL DEFAULT '',
	finished_at    TEXT NOT NULL DEFAULT '',
	error_category TEXT NOT NULL DEFAULT '',
	error_summary  TEXT NOT NULL DEFAULT '',
	snapshot_id    TEXT NOT NULL DEFAULT '',
	database_name  TEXT NOT NULL DEFAULT '',
	policy_name    TEXT NOT NULL DEFAULT '',
	target_path    TEXT NOT NULL DEFAULT '',
	host_tag       TEXT NOT NULL DEFAULT '',
	bytes_total    INTEGER NOT NULL DEFAULT 0,
	files_new      INTEGER NOT NULL DEFAULT 0,
	files_changed  INTEGER NOT NULL DEFAULT 0,
	row_version    INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_jobs_state ON jobs(state);
CREATE INDEX idx_jobs_type ON jobs(type);
`,
	},
}

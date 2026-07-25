package rundb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/compozy/compozy/internal/store"
)

type migration struct {
	version    int
	name       string
	statements []string
}

var migrations = []migration{
	{
		version: 1,
		name:    "run_store_initial",
		statements: []string{
			`CREATE TABLE IF NOT EXISTS events (
				sequence     INTEGER PRIMARY KEY,
				event_kind   TEXT NOT NULL,
				payload_json TEXT NOT NULL,
				timestamp    TEXT NOT NULL,
				job_id       TEXT NOT NULL DEFAULT '',
				step_key     TEXT NOT NULL DEFAULT ''
			);`,
			`CREATE INDEX IF NOT EXISTS idx_events_kind ON events(event_kind);`,
			`CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp);`,
			`CREATE INDEX IF NOT EXISTS idx_events_job_id ON events(job_id);`,
			`CREATE TABLE IF NOT EXISTS job_state (
				job_id       TEXT PRIMARY KEY,
				task_id      TEXT NOT NULL DEFAULT '',
				status       TEXT NOT NULL,
				agent_name   TEXT NOT NULL DEFAULT '',
				summary_json TEXT NOT NULL,
				updated_at   TEXT NOT NULL
			);`,
			`CREATE INDEX IF NOT EXISTS idx_job_state_status ON job_state(status);`,
			`CREATE TABLE IF NOT EXISTS transcript_messages (
				sequence      INTEGER PRIMARY KEY,
				stream        TEXT NOT NULL,
				role          TEXT NOT NULL,
				content       TEXT NOT NULL,
				metadata_json TEXT NOT NULL,
				timestamp     TEXT NOT NULL
			);`,
			`CREATE INDEX IF NOT EXISTS idx_transcript_messages_timestamp
				ON transcript_messages(timestamp);`,
			`CREATE TABLE IF NOT EXISTS hook_runs (
				id          TEXT PRIMARY KEY,
				hook_name   TEXT NOT NULL,
				source      TEXT NOT NULL,
				outcome     TEXT NOT NULL,
				duration_ns INTEGER NOT NULL,
				payload_json TEXT NOT NULL,
				recorded_at TEXT NOT NULL
			);`,
			`CREATE INDEX IF NOT EXISTS idx_hook_runs_recorded_at ON hook_runs(recorded_at);`,
			`CREATE TABLE IF NOT EXISTS token_usage (
				turn_id        TEXT PRIMARY KEY,
				input_tokens   INTEGER NOT NULL DEFAULT 0,
				output_tokens  INTEGER NOT NULL DEFAULT 0,
				total_tokens   INTEGER NOT NULL DEFAULT 0,
				cost_amount    REAL,
				timestamp      TEXT NOT NULL
			);`,
			`CREATE INDEX IF NOT EXISTS idx_token_usage_timestamp ON token_usage(timestamp);`,
			`CREATE TABLE IF NOT EXISTS artifact_sync_log (
				sequence      INTEGER PRIMARY KEY,
				relative_path TEXT NOT NULL,
				change_kind   TEXT NOT NULL,
				checksum      TEXT NOT NULL DEFAULT '',
				synced_at     TEXT NOT NULL
			);`,
			`CREATE INDEX IF NOT EXISTS idx_artifact_sync_log_path ON artifact_sync_log(relative_path);`,
		},
	},
	{
		version: 2,
		name:    "drop_dead_secondary_indexes",
		statements: []string{
			`DROP INDEX IF EXISTS idx_events_kind;`,
			`DROP INDEX IF EXISTS idx_events_timestamp;`,
			`DROP INDEX IF EXISTS idx_events_job_id;`,
			`DROP INDEX IF EXISTS idx_job_state_status;`,
			`DROP INDEX IF EXISTS idx_transcript_messages_timestamp;`,
			`DROP INDEX IF EXISTS idx_artifact_sync_log_path;`,
		},
	},
	{
		version: 3,
		name:    "add_run_integrity_state",
		statements: []string{
			`CREATE TABLE IF NOT EXISTS run_integrity (
				singleton_id              INTEGER PRIMARY KEY CHECK (singleton_id = 1),
				incomplete                INTEGER NOT NULL DEFAULT 0,
				reasons_json              TEXT NOT NULL DEFAULT '[]',
				journal_terminal_drops    INTEGER NOT NULL DEFAULT 0,
				journal_non_terminal_drops INTEGER NOT NULL DEFAULT 0,
				first_detected_at         TEXT NOT NULL,
				updated_at                TEXT NOT NULL
			);`,
		},
	},
	{
		version: 4,
		name:    "add_convergence_projections",
		// Convergence projection tables. Canonical state stays the convergence
		// journal + events log; these rows are rebuildable projections committed in
		// the same transaction as their canonical event row. All are additive: a
		// prior-version run.db upgrades in place without touching existing rows.
		statements: []string{
			`CREATE TABLE IF NOT EXISTS convergence_runs (
				convergence_id   TEXT PRIMARY KEY,
				run_id           TEXT NOT NULL DEFAULT '',
				request_id       TEXT NOT NULL DEFAULT '',
				workspace_id     TEXT NOT NULL DEFAULT '',
				execution_scope  TEXT NOT NULL DEFAULT '',
				task_group_id    TEXT NOT NULL DEFAULT '',
				branch           TEXT NOT NULL DEFAULT '',
				worktree         TEXT NOT NULL DEFAULT '',
				target_snapshot  TEXT NOT NULL DEFAULT '',
				config_fingerprint TEXT NOT NULL DEFAULT '',
				config_json      TEXT NOT NULL DEFAULT '{}',
				updated_at       TEXT NOT NULL
			);`,
			`CREATE TABLE IF NOT EXISTS convergence_segments (
				run_id           TEXT PRIMARY KEY,
				convergence_id   TEXT NOT NULL,
				ordinal          INTEGER NOT NULL DEFAULT 0,
				previous_run_id  TEXT NOT NULL DEFAULT '',
				source_run_id    TEXT NOT NULL DEFAULT '',
				state            TEXT NOT NULL DEFAULT 'prepared',
				resume_cursor    TEXT NOT NULL DEFAULT '',
				resume_claimed   INTEGER NOT NULL DEFAULT 0 CHECK (resume_claimed IN (0, 1)),
				terminal_kind    TEXT NOT NULL DEFAULT '',
				terminal_reason  TEXT NOT NULL DEFAULT '',
				terminal_seq     INTEGER NOT NULL DEFAULT 0,
				updated_at       TEXT NOT NULL
			);`,
			`CREATE INDEX IF NOT EXISTS idx_convergence_segments_convergence
				ON convergence_segments(convergence_id, ordinal);`,
			`CREATE UNIQUE INDEX IF NOT EXISTS uq_convergence_segments_ordinal
				ON convergence_segments(convergence_id, ordinal);`,
			`CREATE UNIQUE INDEX IF NOT EXISTS uq_convergence_segments_previous
				ON convergence_segments(previous_run_id) WHERE previous_run_id <> '';`,
			`CREATE TABLE IF NOT EXISTS convergence_phases (
				phase_id       TEXT PRIMARY KEY,
				run_id         TEXT NOT NULL,
				phase          TEXT NOT NULL,
				round          INTEGER NOT NULL DEFAULT 0,
				batch_id       TEXT NOT NULL DEFAULT '',
				attempt        INTEGER NOT NULL DEFAULT 0,
				snapshot       TEXT NOT NULL DEFAULT '',
				state          TEXT NOT NULL DEFAULT 'active',
				sequence       INTEGER NOT NULL DEFAULT 0,
				updated_at     TEXT NOT NULL
			);`,
			`CREATE INDEX IF NOT EXISTS idx_convergence_phases_run ON convergence_phases(run_id, sequence);`,
			`CREATE TABLE IF NOT EXISTS convergence_routes (
				phase_id             TEXT NOT NULL,
				role                 TEXT NOT NULL,
				primary_route        TEXT NOT NULL DEFAULT '',
				selected_route       TEXT NOT NULL DEFAULT '',
				configuration_source TEXT NOT NULL DEFAULT '',
				fallback_reason      TEXT NOT NULL DEFAULT '',
				updated_at           TEXT NOT NULL,
				PRIMARY KEY (phase_id, role)
			);`,
			`CREATE TABLE IF NOT EXISTS convergence_rounds (
				round_id              TEXT PRIMARY KEY,
				run_id                TEXT NOT NULL,
				number                INTEGER NOT NULL DEFAULT 0,
				admitted_at           TEXT NOT NULL DEFAULT '',
				resolved              INTEGER NOT NULL DEFAULT 0,
				severity_decreased    INTEGER NOT NULL DEFAULT 0,
				verification_improved INTEGER NOT NULL DEFAULT 0,
				no_progress_count     INTEGER NOT NULL DEFAULT 0,
				oscillation_count     INTEGER NOT NULL DEFAULT 0,
				terminal_reason       TEXT NOT NULL DEFAULT '',
				sequence              INTEGER NOT NULL DEFAULT 0,
				updated_at            TEXT NOT NULL
			);`,
			`CREATE INDEX IF NOT EXISTS idx_convergence_rounds_run ON convergence_rounds(run_id, number);`,
			`CREATE TABLE IF NOT EXISTS convergence_batches (
				batch_id       TEXT PRIMARY KEY,
				run_id         TEXT NOT NULL,
				phase_id       TEXT NOT NULL DEFAULT '',
				finding_fingerprints_json TEXT NOT NULL DEFAULT '[]',
				before_snapshot TEXT NOT NULL DEFAULT '',
				after_snapshot  TEXT NOT NULL DEFAULT '',
				status          TEXT NOT NULL DEFAULT '',
				affected_ref    TEXT NOT NULL DEFAULT '',
				sequence        INTEGER NOT NULL DEFAULT 0,
				updated_at      TEXT NOT NULL
			);`,
			`CREATE TABLE IF NOT EXISTS convergence_findings (
				fingerprint    TEXT PRIMARY KEY,
				run_id         TEXT NOT NULL,
				state          TEXT NOT NULL DEFAULT 'actionable',
				severity       TEXT NOT NULL DEFAULT '',
				snapshot_seq   INTEGER NOT NULL DEFAULT 0,
				attempts       INTEGER NOT NULL DEFAULT 0,
				first_seq      INTEGER NOT NULL DEFAULT 0,
				evidence_ref   TEXT NOT NULL DEFAULT '',
				updated_at     TEXT NOT NULL
			);`,
			`CREATE INDEX IF NOT EXISTS idx_convergence_findings_run ON convergence_findings(run_id, first_seq);`,
			`CREATE TABLE IF NOT EXISTS convergence_observations (
				observation_id TEXT PRIMARY KEY,
				run_id         TEXT NOT NULL,
				fingerprint    TEXT NOT NULL,
				snapshot       TEXT NOT NULL DEFAULT '',
				snapshot_seq   INTEGER NOT NULL DEFAULT 0,
				severity       TEXT NOT NULL DEFAULT '',
				outcome        TEXT NOT NULL DEFAULT '',
				review_id      TEXT NOT NULL DEFAULT '',
				sequence       INTEGER NOT NULL DEFAULT 0,
				recorded_at    TEXT NOT NULL
			);`,
			`CREATE INDEX IF NOT EXISTS idx_convergence_observations_fingerprint
				ON convergence_observations(fingerprint, sequence);`,
			`CREATE TABLE IF NOT EXISTS convergence_dispositions (
				decision_id         TEXT PRIMARY KEY,
				run_id              TEXT NOT NULL,
				fingerprint         TEXT NOT NULL,
				disposition         TEXT NOT NULL DEFAULT '',
				actor_kind          TEXT NOT NULL DEFAULT '',
				reason              TEXT NOT NULL DEFAULT '',
				snapshot            TEXT NOT NULL DEFAULT '',
				snapshot_seq        INTEGER NOT NULL DEFAULT 0,
				related_fingerprint TEXT NOT NULL DEFAULT '',
				sequence            INTEGER NOT NULL DEFAULT 0,
				recorded_at         TEXT NOT NULL
			);`,
			`CREATE TABLE IF NOT EXISTS convergence_verifications (
				verification_id     TEXT PRIMARY KEY,
				run_id              TEXT NOT NULL,
				phase_id            TEXT NOT NULL DEFAULT '',
				command_fingerprint TEXT NOT NULL DEFAULT '',
				snapshot            TEXT NOT NULL DEFAULT '',
				exit_code           INTEGER,
				passed              INTEGER NOT NULL DEFAULT 0,
				attempt             INTEGER NOT NULL DEFAULT 0,
				evidence_path       TEXT NOT NULL DEFAULT '',
				sequence            INTEGER NOT NULL DEFAULT 0,
				updated_at          TEXT NOT NULL
			);`,
			`CREATE INDEX IF NOT EXISTS idx_convergence_verifications_run
				ON convergence_verifications(run_id, sequence);`,
			`CREATE TABLE IF NOT EXISTS convergence_verification_failures (
				failure_fingerprint TEXT PRIMARY KEY,
				run_id              TEXT NOT NULL,
				attempts            INTEGER NOT NULL DEFAULT 0,
				signature           TEXT NOT NULL DEFAULT '',
				updated_at          TEXT NOT NULL
			);`,
			`CREATE TABLE IF NOT EXISTS convergence_approvals (
				proposal_id  TEXT PRIMARY KEY,
				run_id       TEXT NOT NULL,
				fingerprint  TEXT NOT NULL DEFAULT '',
				action       TEXT NOT NULL DEFAULT '',
				snapshot     TEXT NOT NULL DEFAULT '',
				decision     TEXT NOT NULL DEFAULT '',
				reason       TEXT NOT NULL DEFAULT '',
				evidence_ref TEXT NOT NULL DEFAULT '',
				sequence     INTEGER NOT NULL DEFAULT 0,
				updated_at   TEXT NOT NULL
			);`,
		},
	},
}

var migrationTableStatements = []string{
	`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT NOT NULL,
		applied_at TEXT NOT NULL
	);`,
}

// ErrSchemaTooNew reports that a database carries a migration newer than this binary understands.
var ErrSchemaTooNew = errors.New("rundb: schema too new")

// SchemaTooNewError carries the current database and binary migration versions.
type SchemaTooNewError struct {
	CurrentVersion int
	KnownVersion   int
}

func (e SchemaTooNewError) Error() string {
	return fmt.Sprintf("rundb: schema too new (db=%d binary=%d)", e.CurrentVersion, e.KnownVersion)
}

func (e SchemaTooNewError) Is(target error) bool {
	return target == ErrSchemaTooNew
}

func applyMigrations(ctx context.Context, db *sql.DB, now func() time.Time) error {
	if ctx == nil {
		return errors.New("rundb: migrate context is required")
	}
	if db == nil {
		return errors.New("rundb: migrate database is required")
	}
	if now == nil {
		now = func() time.Time {
			return time.Now().UTC()
		}
	}

	if err := store.EnsureSchema(ctx, db, migrationTableStatements); err != nil {
		return fmt.Errorf("rundb: ensure schema migrations table: %w", err)
	}

	applied, err := loadAppliedMigrations(ctx, db)
	if err != nil {
		return err
	}

	latestKnown := migrations[len(migrations)-1].version
	if applied.highestVersion > latestKnown {
		return SchemaTooNewError{
			CurrentVersion: applied.highestVersion,
			KnownVersion:   latestKnown,
		}
	}

	for _, item := range migrations {
		if applied.versions[item.version] {
			continue
		}
		if err := applyMigration(ctx, db, item, now); err != nil {
			return err
		}
	}

	return nil
}

type appliedMigrationState struct {
	versions       map[int]bool
	highestVersion int
}

func loadAppliedMigrations(ctx context.Context, db *sql.DB) (appliedMigrationState, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY version ASC`)
	if err != nil {
		return appliedMigrationState{}, fmt.Errorf("rundb: query schema migrations: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	state := appliedMigrationState{versions: make(map[int]bool)}
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return appliedMigrationState{}, fmt.Errorf("rundb: scan schema migration: %w", err)
		}
		state.versions[version] = true
		if version > state.highestVersion {
			state.highestVersion = version
		}
	}
	if err := rows.Err(); err != nil {
		return appliedMigrationState{}, fmt.Errorf("rundb: iterate schema migrations: %w", err)
	}

	return state, nil
}

func applyMigration(ctx context.Context, db *sql.DB, item migration, now func() time.Time) (retErr error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("rundb: begin migration %d: %w", item.version, err)
	}

	committed := false
	defer func() {
		if !committed {
			if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
				retErr = errors.Join(
					retErr,
					fmt.Errorf("rundb: rollback migration %d: %w", item.version, rollbackErr),
				)
			}
		}
	}()

	for _, stmt := range item.statements {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf(
				"rundb: apply migration %d (%s): %w",
				item.version,
				strings.TrimSpace(item.name),
				err,
			)
		}
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
		item.version,
		strings.TrimSpace(item.name),
		store.FormatTimestamp(now()),
	); err != nil {
		return fmt.Errorf("rundb: record migration %d: %w", item.version, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("rundb: commit migration %d: %w", item.version, err)
	}
	committed = true
	return nil
}

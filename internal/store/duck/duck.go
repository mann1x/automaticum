// Package duck implements store.State on DuckDB.
//
// One process opens the database for writing. Extension autoloading and
// autoinstalling are switched off: the store needs no downloadable extension,
// and a daemon must not fetch code at run time.
package duck

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	duckdb "github.com/duckdb/duckdb-go/v2"

	"github.com/mann1x/automaticum/internal/store"
	"github.com/mann1x/automaticum/internal/store/blob"
)

// SchemaVersion is the version of the tables this package creates.
const SchemaVersion = 2

const schema = `
CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL);

CREATE TABLE IF NOT EXISTS runs (
	id               VARCHAR PRIMARY KEY,
	batch            VARCHAR, cell VARCHAR, cohort VARCHAR, campaign VARCHAR,
	suite            VARCHAR, task_id VARCHAR, task_version INTEGER,
	arm_id           VARCHAR, lane VARCHAR, host VARCHAR,
	verdict          VARCHAR NOT NULL,
	original_verdict VARCHAR, verdict_reason VARCHAR, prior_verdict VARCHAR,
	agent_end        VARCHAR, claimed_success BOOLEAN,
	started_at       TIMESTAMPTZ, ended_at TIMESTAMPTZ,
	wall_ms          BIGINT, paused BOOLEAN,
	provider         VARCHAR, model VARCHAR, endpoint VARCHAR,
	thinking         VARCHAR, harness VARCHAR,
	arm              VARCHAR, provenance VARCHAR,
	imported         BOOLEAN, importer_version VARCHAR, missing VARCHAR
);

CREATE TABLE IF NOT EXISTS blob_refs (
	run_id VARCHAR NOT NULL, kind VARCHAR NOT NULL, name VARCHAR NOT NULL,
	hash   VARCHAR NOT NULL, size BIGINT NOT NULL,
	PRIMARY KEY (run_id, kind, name)
);

-- Derived tables carry no primary key: they are replaced per run, and a key
-- would make every replace a delete-and-reinsert of index entries.
CREATE TABLE IF NOT EXISTS tool_calls (
	run_id VARCHAR NOT NULL, turn INTEGER, seq INTEGER, tool VARCHAR,
	ok BOOLEAN, error_class VARCHAR, parse_path VARCHAR, duration_ms BIGINT
);

CREATE TABLE IF NOT EXISTS findings (
	run_id VARCHAR NOT NULL, detector VARCHAR, detector_version VARCHAR,
	severity VARCHAR, turn INTEGER, count INTEGER, evidence VARCHAR
);

-- Version 2. seq orders items enqueued in the same instant.
CREATE SEQUENCE IF NOT EXISTS queue_seq;
CREATE TABLE IF NOT EXISTS queue (
	run_id VARCHAR PRIMARY KEY, seq BIGINT NOT NULL DEFAULT nextval('queue_seq'),
	batch VARCHAR, arm VARCHAR NOT NULL, task VARCHAR NOT NULL, lane VARCHAR NOT NULL,
	enqueued_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS lane_state (
	lane VARCHAR PRIMARY KEY, state VARCHAR NOT NULL, run_id VARCHAR,
	updated_at TIMESTAMPTZ NOT NULL, detail VARCHAR
);
`

// State is a store.State backed by one DuckDB file.
type State struct {
	db *sql.DB
}

var _ store.State = (*State)(nil)

// Open opens or creates the database at path.
func Open(ctx context.Context, path string) (*State, error) {
	connector, err := duckdb.NewConnector(path, func(execer driver.ExecerContext) error {
		for _, q := range []string{
			`SET autoinstall_known_extensions = false`,
			`SET autoload_known_extensions = false`,
		} {
			if _, err := execer.ExecContext(ctx, q, nil); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("duck: open %s: %w", path, err)
	}
	db := sql.OpenDB(connector)
	s := &State{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *State) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("duck: create schema: %w", err)
	}
	var v sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT max(version) FROM schema_version`).Scan(&v); err != nil {
		return fmt.Errorf("duck: read schema version: %w", err)
	}
	switch {
	case !v.Valid:
		_, err := s.db.ExecContext(ctx, `INSERT INTO schema_version VALUES (?)`, SchemaVersion)
		return err
	case v.Int64 > SchemaVersion:
		return fmt.Errorf("duck: database has schema version %d, this build knows %d", v.Int64, SchemaVersion)
	case v.Int64 < SchemaVersion:
		// Every change so far only adds tables, which the schema above
		// creates when they are missing.
		_, err := s.db.ExecContext(ctx, `INSERT INTO schema_version VALUES (?)`, SchemaVersion)
		return err
	}
	return nil
}

// Version returns the DuckDB library version.
func (s *State) Version(ctx context.Context) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT version()`).Scan(&v)
	return v, err
}

const runColumns = `id, batch, cell, cohort, campaign, suite, task_id, task_version, arm_id, lane, host,
	verdict, original_verdict, verdict_reason, prior_verdict, agent_end, claimed_success,
	started_at, ended_at, wall_ms, paused, provider, model, endpoint, thinking, harness,
	arm, provenance, imported, importer_version, missing`

// UpsertRun inserts the run or replaces the one with the same id.
func (s *State) UpsertRun(ctx context.Context, r *store.Run) error {
	if r.ID == "" {
		return errors.New("duck: run has no id")
	}
	if !r.Verdict.Valid() {
		return fmt.Errorf("duck: run %s: %q is not a verdict", r.ID, r.Verdict)
	}
	if r.PriorVerdict != "" && !r.PriorVerdict.Valid() {
		return fmt.Errorf("duck: run %s: %q is not a verdict", r.ID, r.PriorVerdict)
	}
	missing, err := json.Marshal(r.Missing)
	if err != nil {
		return err
	}
	var claimed sql.NullBool
	if r.ClaimedSuccess != nil {
		claimed = sql.NullBool{Bool: *r.ClaimedSuccess, Valid: true}
	}
	_, err = s.db.ExecContext(ctx, `INSERT OR REPLACE INTO runs (`+runColumns+`) VALUES (`+placeholders(31)+`)`,
		r.ID, r.Batch, r.Cell, r.Cohort, r.Campaign, r.Suite, r.TaskID, r.TaskVersion, r.ArmID, r.Lane, r.Host,
		string(r.Verdict), r.OriginalVerdict, r.VerdictReason, string(r.PriorVerdict), r.AgentEnd, claimed,
		nullTime(r.StartedAt), nullTime(r.EndedAt), r.WallMS, r.Paused, r.Provider, r.Model, r.Endpoint, r.Thinking, r.Harness,
		string(r.Arm), string(r.Provenance), r.Imported, r.ImporterVersion, string(missing))
	if err != nil {
		return fmt.Errorf("duck: upsert run %s: %w", r.ID, err)
	}
	return nil
}

func scanRun(sc interface{ Scan(...any) error }) (*store.Run, error) {
	var (
		r                           store.Run
		verdict, prior              string
		claimed                     sql.NullBool
		started, ended              sql.NullTime
		arm, provenance, missing    sql.NullString
		batch, cell, cohort, camp   sql.NullString
		suite, task, armID, lane    sql.NullString
		host, orig, reason, end     sql.NullString
		provider, model, endpoint   sql.NullString
		thinking, harness, importer sql.NullString
		taskVersion, wall           sql.NullInt64
		paused, imported            sql.NullBool
	)
	err := sc.Scan(&r.ID, &batch, &cell, &cohort, &camp, &suite, &task, &taskVersion, &armID, &lane, &host,
		&verdict, &orig, &reason, &prior, &end, &claimed,
		&started, &ended, &wall, &paused, &provider, &model, &endpoint, &thinking, &harness,
		&arm, &provenance, &imported, &importer, &missing)
	if err != nil {
		return nil, err
	}
	r.Batch, r.Cell, r.Cohort, r.Campaign = batch.String, cell.String, cohort.String, camp.String
	r.Suite, r.TaskID, r.TaskVersion, r.ArmID = suite.String, task.String, int(taskVersion.Int64), armID.String
	r.Lane, r.Host = lane.String, host.String
	r.Verdict, r.OriginalVerdict, r.VerdictReason = store.Verdict(verdict), orig.String, reason.String
	r.PriorVerdict, r.AgentEnd = store.Verdict(prior), end.String
	if claimed.Valid {
		b := claimed.Bool
		r.ClaimedSuccess = &b
	}
	r.StartedAt, r.EndedAt = started.Time.UTC(), ended.Time.UTC()
	if !started.Valid {
		r.StartedAt = time.Time{}
	}
	if !ended.Valid {
		r.EndedAt = time.Time{}
	}
	r.WallMS, r.Paused = wall.Int64, paused.Bool
	r.Provider, r.Model, r.Endpoint, r.Thinking, r.Harness = provider.String, model.String, endpoint.String, thinking.String, harness.String
	if arm.String != "" {
		r.Arm = json.RawMessage(arm.String)
	}
	if provenance.String != "" {
		r.Provenance = json.RawMessage(provenance.String)
	}
	r.Imported, r.ImporterVersion = imported.Bool, importer.String
	if missing.String != "" && missing.String != "null" {
		if err := json.Unmarshal([]byte(missing.String), &r.Missing); err != nil {
			return nil, fmt.Errorf("duck: run %s: missing list: %w", r.ID, err)
		}
	}
	return &r, nil
}

// GetRun returns the run, or store.ErrNotFound.
func (s *State) GetRun(ctx context.Context, id string) (*store.Run, error) {
	r, err := scanRun(s.db.QueryRowContext(ctx, `SELECT `+runColumns+` FROM runs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("run %s: %w", id, store.ErrNotFound)
	}
	return r, err
}

func where(f store.RunFilter) (string, []any) {
	var conds []string
	var args []any
	add := func(cond string, arg any) {
		conds = append(conds, cond)
		args = append(args, arg)
	}
	if f.Suite != "" {
		add("suite = ?", f.Suite)
	}
	if f.TaskID != "" {
		add("task_id = ?", f.TaskID)
	}
	if f.ArmID != "" {
		add("arm_id = ?", f.ArmID)
	}
	if f.Model != "" {
		add("model = ?", f.Model)
	}
	if f.Verdict != "" {
		add("verdict = ?", string(f.Verdict))
	}
	if f.Batch != "" {
		add("batch = ?", f.Batch)
	}
	if f.Imported != nil {
		add("imported = ?", *f.Imported)
	}
	if !f.Since.IsZero() {
		add("started_at >= ?", f.Since)
	}
	if !f.Until.IsZero() {
		add("started_at < ?", f.Until)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// ListRuns returns the runs matching f, ordered by id.
func (s *State) ListRuns(ctx context.Context, f store.RunFilter) ([]store.Run, error) {
	w, args := where(f)
	q := `SELECT ` + runColumns + ` FROM runs` + w + ` ORDER BY id`
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d OFFSET %d", f.Limit, f.Offset)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// CountRuns counts the runs matching f.
func (s *State) CountRuns(ctx context.Context, f store.RunFilter) (int64, error) {
	w, args := where(f)
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM runs`+w, args...).Scan(&n)
	return n, err
}

// AddBlobRefs records blobs attached to runs. Repeating a call is harmless.
func (s *State) AddBlobRefs(ctx context.Context, refs []store.BlobRef) error {
	if len(refs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	st, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO blob_refs (run_id, kind, name, hash, size) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer st.Close()
	for _, r := range refs {
		if _, err := st.ExecContext(ctx, r.RunID, r.Kind, r.Name, string(r.Hash), r.Size); err != nil {
			return fmt.Errorf("duck: blob ref %s/%s/%s: %w", r.RunID, r.Kind, r.Name, err)
		}
	}
	return tx.Commit()
}

// BlobRefs lists the blobs attached to a run.
func (s *State) BlobRefs(ctx context.Context, runID string) ([]store.BlobRef, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT run_id, kind, name, hash, size FROM blob_refs WHERE run_id = ? ORDER BY kind, name`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.BlobRef
	for rows.Next() {
		var r store.BlobRef
		var h string
		if err := rows.Scan(&r.RunID, &r.Kind, &r.Name, &h, &r.Size); err != nil {
			return nil, err
		}
		r.Hash = blob.Hash(h)
		out = append(out, r)
	}
	return out, rows.Err()
}

// replace deletes a run's rows from a derived table and appends new ones, in
// one transaction, through DuckDB's appender.
func (s *State) replace(ctx context.Context, table, runID string, n int, row func(i int) []driver.Value) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN`); err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			conn.ExecContext(context.WithoutCancel(ctx), `ROLLBACK`)
		}
	}()
	if _, err := conn.ExecContext(ctx, `DELETE FROM `+table+` WHERE run_id = ?`, runID); err != nil {
		return err
	}
	if n > 0 {
		err = conn.Raw(func(dc any) error {
			app, err := duckdb.NewAppenderFromConn(dc.(driver.Conn), "", table)
			if err != nil {
				return err
			}
			for i := 0; i < n; i++ {
				if err := app.AppendRow(row(i)...); err != nil {
					app.Close()
					return err
				}
			}
			return app.Close()
		})
		if err != nil {
			return fmt.Errorf("duck: append to %s for run %s: %w", table, runID, err)
		}
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return err
	}
	done = true
	return nil
}

// ReplaceToolCalls replaces the tool calls derived for a run.
func (s *State) ReplaceToolCalls(ctx context.Context, runID string, calls []store.ToolCall) error {
	return s.replace(ctx, "tool_calls", runID, len(calls), func(i int) []driver.Value {
		c := calls[i]
		return []driver.Value{runID, int32(c.Turn), int32(c.Seq), c.Tool, c.OK, c.ErrorClass, c.ParsePath, c.DurationMS}
	})
}

// ReplaceFindings replaces the findings derived for a run.
func (s *State) ReplaceFindings(ctx context.Context, runID string, findings []store.Finding) error {
	return s.replace(ctx, "findings", runID, len(findings), func(i int) []driver.Value {
		f := findings[i]
		return []driver.Value{runID, f.Detector, f.DetectorVersion, f.Severity, int32(f.Turn), int32(f.Count), string(f.Evidence)}
	})
}

// SetLaneState records the current state of a lane.
func (s *State) SetLaneState(ctx context.Context, l store.LaneState) error {
	if l.UpdatedAt.IsZero() {
		l.UpdatedAt = time.Now()
	}
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO lane_state (lane, state, run_id, updated_at, detail) VALUES (?, ?, ?, ?, ?)`,
		l.Lane, l.State, l.RunID, l.UpdatedAt.UTC(), string(l.Detail))
	return err
}

// LaneStates lists every lane's current state.
func (s *State) LaneStates(ctx context.Context) ([]store.LaneState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT lane, state, run_id, updated_at, detail FROM lane_state ORDER BY lane`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.LaneState
	for rows.Next() {
		var l store.LaneState
		var run, detail sql.NullString
		if err := rows.Scan(&l.Lane, &l.State, &run, &l.UpdatedAt, &detail); err != nil {
			return nil, err
		}
		l.RunID, l.UpdatedAt = run.String, l.UpdatedAt.UTC()
		if detail.String != "" {
			l.Detail = json.RawMessage(detail.String)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// Enqueue adds runs to the queue. An item already queued is left as it is.
func (s *State) Enqueue(ctx context.Context, items []store.QueueItem) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, it := range items {
		if it.RunID == "" || it.Arm == "" || it.Task == "" || it.Lane == "" {
			return fmt.Errorf("duck: queue item %+v is incomplete", it)
		}
		if it.EnqueuedAt.IsZero() {
			it.EnqueuedAt = time.Now()
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO queue (run_id, batch, arm, task, lane, enqueued_at) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
			it.RunID, it.Batch, it.Arm, it.Task, it.Lane, it.EnqueuedAt.UTC()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Queue lists the queued runs, oldest first.
func (s *State) Queue(ctx context.Context) ([]store.QueueItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT run_id, batch, arm, task, lane, enqueued_at FROM queue ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.QueueItem
	for rows.Next() {
		var it store.QueueItem
		var batch sql.NullString
		if err := rows.Scan(&it.RunID, &batch, &it.Arm, &it.Task, &it.Lane, &it.EnqueuedAt); err != nil {
			return nil, err
		}
		it.Batch, it.EnqueuedAt = batch.String, it.EnqueuedAt.UTC()
		out = append(out, it)
	}
	return out, rows.Err()
}

// Unqueue takes a run off the queue; it is not an error if it is not there.
func (s *State) Unqueue(ctx context.Context, runID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM queue WHERE run_id = ?`, runID)
	return err
}

// Query runs read-only SQL in DuckDB's dialect.
func (s *State) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return s.db.QueryContext(ctx, query, args...)
}

// Checkpoint flushes the write-ahead log into the database file.
func (s *State) Checkpoint(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CHECKPOINT`)
	return err
}

// Close closes the database.
func (s *State) Close() error { return s.db.Close() }

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

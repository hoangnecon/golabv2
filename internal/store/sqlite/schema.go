package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hoangnecon/golabv2/internal/domain"
)

type columnDef struct {
	name string
	decl string
}

func (s *Store) initSchema() error {
	baseDDL := `
	CREATE TABLE IF NOT EXISTS profiles (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		account_email TEXT DEFAULT '',
		display_name TEXT DEFAULT '',
		tier TEXT DEFAULT 'free',
		ccu_balance REAL DEFAULT 0,
		ccu_burn_rate REAL DEFAULT 0,
		eligible_gpus_json TEXT DEFAULT '[]',
		eligible_tpus_json TEXT DEFAULT '[]',
		last_telemetry_at DATETIME,
		user_data_dir TEXT NOT NULL UNIQUE,
		cdp_port INTEGER NOT NULL UNIQUE,
		chrome_pid INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS projects (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		notebook_id TEXT NOT NULL UNIQUE,
		notebook_url TEXT NOT NULL,
		owner_profile_id TEXT NOT NULL REFERENCES profiles(id) ON DELETE RESTRICT,
		project_token TEXT NOT NULL UNIQUE,
		locked_session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
		profile_id TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		notebook_id TEXT DEFAULT '',
		notebook_url TEXT NOT NULL DEFAULT '',
		session_token TEXT NOT NULL UNIQUE,
		tab_id TEXT DEFAULT '',
		permission TEXT NOT NULL DEFAULT 'unknown',
		connection_state TEXT NOT NULL DEFAULT 'browser_stopped',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_attached_at DATETIME
	);

	CREATE TABLE IF NOT EXISTS activity_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT DEFAULT '',
		project_id TEXT DEFAULT '',
		profile_id TEXT DEFAULT '',
		token_prefix TEXT DEFAULT '',
		tool_name TEXT NOT NULL,
		status TEXT NOT NULL,
		error_code TEXT DEFAULT '',
		error_message TEXT DEFAULT '',
		latency_ms INTEGER NOT NULL DEFAULT 0,
		request_json TEXT,
		request_bytes INTEGER,
		request_truncated BOOLEAN DEFAULT 0,
		response_json TEXT,
		response_bytes INTEGER,
		response_truncated BOOLEAN DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	if _, err := s.db.Exec(baseDDL); err != nil {
		return fmt.Errorf("exec base schema: %w", err)
	}

	if err := s.addMissingColumns("profiles", []columnDef{
		{"display_name", "TEXT DEFAULT ''"},
		{"ccu_balance", "REAL DEFAULT 0"},
		{"ccu_burn_rate", "REAL DEFAULT 0"},
		{"eligible_gpus_json", "TEXT DEFAULT '[]'"},
		{"eligible_tpus_json", "TEXT DEFAULT '[]'"},
		{"last_telemetry_at", "DATETIME"},
		{"chrome_pid", "INTEGER DEFAULT 0"},
		{"updated_at", "DATETIME"},
	}); err != nil {
		return err
	}

	if err := s.addMissingColumns("projects", []columnDef{
		{"notebook_id", "TEXT"},
		{"project_token", "TEXT"},
		{"locked_session_id", "TEXT"},
		{"updated_at", "DATETIME"},
	}); err != nil {
		return err
	}

	if err := s.addMissingColumns("sessions", []columnDef{
		{"project_id", "TEXT"},
		{"notebook_id", "TEXT DEFAULT ''"},
		{"notebook_url", "TEXT DEFAULT ''"},
		{"session_token", "TEXT"},
		{"tab_id", "TEXT DEFAULT ''"},
		{"permission", "TEXT DEFAULT 'unknown'"},
		{"connection_state", "TEXT DEFAULT 'browser_stopped'"},
	}); err != nil {
		return err
	}

	if err := s.addMissingColumns("activity_logs", []columnDef{
		{"project_id", "TEXT DEFAULT ''"},
		{"profile_id", "TEXT DEFAULT ''"},
		{"token_prefix", "TEXT DEFAULT ''"},
		{"error_code", "TEXT DEFAULT ''"},
		{"request_json", "TEXT"},
		{"request_bytes", "INTEGER"},
		{"request_truncated", "BOOLEAN DEFAULT 0"},
		{"response_json", "TEXT"},
		{"response_bytes", "INTEGER"},
		{"response_truncated", "BOOLEAN DEFAULT 0"},
	}); err != nil {
		return err
	}

	if err := s.backfillLegacyRows(); err != nil {
		return fmt.Errorf("backfill legacy rows: %w", err)
	}

	indexesDDL := `
	CREATE UNIQUE INDEX IF NOT EXISTS idx_profiles_user_data_dir ON profiles(user_data_dir);
	CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_notebook_id ON projects(notebook_id);
	CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_token ON projects(project_token);
	CREATE UNIQUE INDEX IF NOT EXISTS idx_sessions_project_profile ON sessions(project_id, profile_id);
	CREATE UNIQUE INDEX IF NOT EXISTS idx_sessions_token ON sessions(session_token);
	CREATE INDEX IF NOT EXISTS idx_activity_logs_created_id ON activity_logs(created_at DESC, id DESC);
	CREATE INDEX IF NOT EXISTS idx_activity_logs_project_id ON activity_logs(project_id, id DESC);
	CREATE INDEX IF NOT EXISTS idx_activity_logs_session_id ON activity_logs(session_id, id DESC);
	`
	if _, err := s.db.Exec(indexesDDL); err != nil {
		return fmt.Errorf("create indexes: %w", err)
	}
	return nil
}

func (s *Store) backfillLegacyRows() error {
	ctx := context.Background()

	// 1. Normalize legacy permission strings ('edit' -> 'editor', 'view' -> 'view_only').
	_, _ = s.db.ExecContext(ctx, `UPDATE sessions SET permission = 'editor' WHERE permission = 'edit'`)
	_, _ = s.db.ExecContext(ctx, `UPDATE sessions SET permission = 'view_only' WHERE permission = 'view'`)
	_, _ = s.db.ExecContext(ctx, `UPDATE sessions SET connection_state = 'browser_stopped' WHERE connection_state IS NULL OR connection_state = ''`)

	// 2. Backfill missing notebook_id and project_token on existing projects.
	type projRow struct {
		id  string
		url string
		nid string
		tok string
	}
	pRows, err := s.db.QueryContext(ctx, `SELECT id, notebook_url, COALESCE(notebook_id, ''), COALESCE(project_token, '') FROM projects`)
	if err != nil {
		return err
	}
	var existingProjects []projRow
	byNotebookID := make(map[string]string)
	for pRows.Next() {
		var r projRow
		if err := pRows.Scan(&r.id, &r.url, &r.nid, &r.tok); err != nil {
			pRows.Close()
			return err
		}
		existingProjects = append(existingProjects, r)
	}
	pRows.Close()

	for _, r := range existingProjects {
		nid := strings.TrimSpace(r.nid)
		canonURL := r.url
		if nid == "" {
			if normID, normURL, normErr := domain.NormalizeNotebookURL(r.url); normErr == nil {
				nid = normID
				canonURL = normURL
			} else {
				nid = "legacy/" + r.id
			}
		}
		tok := strings.TrimSpace(r.tok)
		if tok == "" {
			var genErr error
			tok, genErr = domain.GenerateToken(domain.TokenPrefixProject)
			if genErr != nil {
				return genErr
			}
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE projects SET notebook_id = ?, notebook_url = ?, project_token = ?, updated_at = COALESCE(updated_at, CURRENT_TIMESTAMP) WHERE id = ?`, nid, canonURL, tok, r.id); err != nil {
			return err
		}
		byNotebookID[nid] = r.id
	}

	// 3. Backfill orphan sessions (missing project_id or session_token) and deduplicate (project_id, profile_id) if needed.
	type sessRow struct {
		id        string
		projectID string
		profileID string
		name      string
		url       string
		nid       string
		tok       string
	}
	sRows, err := s.db.QueryContext(ctx, `SELECT id, COALESCE(project_id, ''), profile_id, name, COALESCE(notebook_url, ''), COALESCE(notebook_id, ''), COALESCE(session_token, '') FROM sessions`)
	if err != nil {
		return err
	}
	var allSessions []sessRow
	for sRows.Next() {
		var sr sessRow
		if err := sRows.Scan(&sr.id, &sr.projectID, &sr.profileID, &sr.name, &sr.url, &sr.nid, &sr.tok); err != nil {
			sRows.Close()
			return err
		}
		allSessions = append(allSessions, sr)
	}
	sRows.Close()

	seenPairs := make(map[string]string)
	for _, sr := range allSessions {
		nid := strings.TrimSpace(sr.nid)
		canonURL := strings.TrimSpace(sr.url)
		if normID, normURL, normErr := domain.NormalizeNotebookURL(sr.url); normErr == nil {
			nid = normID
			canonURL = normURL
		} else if nid == "" {
			nid = "legacy/" + sr.id
		}

		pid := strings.TrimSpace(sr.projectID)
		if pid == "" {
			if existingPID, ok := byNotebookID[nid]; ok {
				pid = existingPID
			} else {
				pid = uuid.New().String()
				ptok, err := domain.GenerateToken(domain.TokenPrefixProject)
				if err != nil {
					return err
				}
				projName := strings.TrimSpace(sr.name)
				if projName == "" {
					projName = domain.DefaultNotebookTitle(nid)
				}
				now := time.Now().UTC()
				if _, err := s.db.ExecContext(ctx,
					`INSERT INTO projects (id, name, notebook_id, notebook_url, owner_profile_id, project_token, created_at, updated_at)
					 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
					pid, projName, nid, canonURL, sr.profileID, ptok, now, now,
				); err != nil {
					return err
				}
				byNotebookID[nid] = pid
			}
		}

		pairKey := pid + "::" + sr.profileID
		if keepSessID, dup := seenPairs[pairKey]; dup {
			_, _ = s.db.ExecContext(ctx, `UPDATE activity_logs SET session_id = ? WHERE session_id = ?`, keepSessID, sr.id)
			_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sr.id)
			continue
		}
		seenPairs[pairKey] = sr.id

		stok := strings.TrimSpace(sr.tok)
		if stok == "" {
			var err error
			stok, err = domain.GenerateToken(domain.TokenPrefixSession)
			if err != nil {
				return err
			}
		}
		if _, err := s.db.ExecContext(ctx,
			`UPDATE sessions SET project_id = ?, notebook_id = ?, notebook_url = ?, session_token = ? WHERE id = ?`,
			pid, nid, canonURL, stok, sr.id,
		); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) addMissingColumns(table string, cols []columnDef) error {
	rows, err := s.db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return fmt.Errorf("read %s columns: %w", table, err)
	}
	have := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		have[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, c := range cols {
		if have[c.name] {
			continue
		}
		if _, err := s.db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, c.name, c.decl)); err != nil {
			return fmt.Errorf("add %s.%s: %w", table, c.name, err)
		}
	}
	return nil
}

func payloadColumns(p *domainPayload) (any, any, bool) {
	if p == nil || len(p.Body) == 0 {
		return nil, nil, false
	}
	return string(p.Body), p.Bytes, p.Truncated
}

func payloadFromColumns(body sql.NullString, size int, truncated bool) *domainPayload {
	if !body.Valid {
		return nil
	}
	return &domainPayload{Body: []byte(body.String), Bytes: size, Truncated: truncated}
}

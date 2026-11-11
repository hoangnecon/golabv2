package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hoangnecon/golabv2/internal/domain"
	_ "modernc.org/sqlite"
)

type domainPayload = domain.Payload

// Store implements domain.Store backed by SQLite WAL.
type Store struct {
	db     *sql.DB
	dbPath string
	mu     sync.RWMutex
}

// Open opens or creates a SQLite WAL database at dbPath with 0700 dir and 0600 file permissions.
func Open(dbPath string) (*Store, error) {
	if strings.TrimSpace(dbPath) == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve user home dir: %w", err)
		}
		dbPath = filepath.Join(home, ".golab", "state.db")
	}

	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create db dir %s: %w", dir, err)
	}
	_ = os.Chmod(dir, 0o700)

	// Touch file with 0600 permissions before sqlite opens it.
	f, err := os.OpenFile(dbPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("touch sqlite file %s: %w", dbPath, err)
	}
	_ = f.Close()
	_ = os.Chmod(dbPath, 0o600)

	dsn := dbPath + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}
	// Single writer serialized cleanly alongside Go-level RWMutex + SQLite WAL.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`PRAGMA journal_mode = WAL; PRAGMA busy_timeout = 5000; PRAGMA foreign_keys = ON; PRAGMA synchronous = NORMAL;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure sqlite pragmas: %w", err)
	}

	s := &Store{db: db, dbPath: dbPath}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return s, nil
}

// New is an alias for Open for backward compatibility.
func New(dbPath string) (*Store, error) {
	return Open(dbPath)
}

// Close closes the underlying SQLite connection.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Close()
}

// JournalMode returns the active PRAGMA journal_mode (e.g. "wal").
func (s *Store) JournalMode() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var mode string
	_ = s.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode)
	return strings.ToLower(strings.TrimSpace(mode))
}

// ForeignKeysEnabled reports whether PRAGMA foreign_keys is enabled.
func (s *Store) ForeignKeysEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var fk int
	_ = s.db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk)
	return fk == 1
}

func parseSQLiteTime(val any) time.Time {
	if val == nil {
		return time.Time{}
	}
	switch v := val.(type) {
	case time.Time:
		return v.UTC()
	case string:
		if strings.TrimSpace(v) == "" {
			return time.Time{}
		}
		layouts := []string{
			"2006-01-02 15:04:05.999999999 -0700 MST",
			"2006-01-02 15:04:05.999999999-07:00",
			"2006-01-02 15:04:05.999999999Z07:00",
			"2006-01-02 15:04:05",
			time.RFC3339Nano,
			time.RFC3339,
		}
		for _, l := range layouts {
			if t, err := time.Parse(l, v); err == nil {
				return t.UTC()
			}
		}
	}
	return time.Time{}
}

func parseOptionalTime(val any) *time.Time {
	t := parseSQLiteTime(val)
	if t.IsZero() {
		return nil
	}
	return &t
}

func marshalStringSlice(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	b, err := json.Marshal(items)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func unmarshalStringSlice(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return []string{}
	}
	return out
}

func mapSQLError(err error, entity string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return domain.NewAppError(domain.CodeNotFound, fmt.Sprintf("%s not found", entity), "", http.StatusNotFound)
	}
	msg := err.Error()
	if strings.Contains(msg, "UNIQUE constraint failed") {
		return domain.WrapAppError(domain.CodeConflict, fmt.Sprintf("%s already exists (%s)", entity, msg), "Use the existing entity or choose a distinct identifier.", http.StatusConflict, err)
	}
	if strings.Contains(msg, "FOREIGN KEY constraint failed") {
		return domain.WrapAppError(domain.CodeConflict, fmt.Sprintf("%s has dependent records or invalid reference", entity), "", http.StatusConflict, err)
	}
	return err
}

// --- Profiles ---

const profileSelectCols = `id, name, COALESCE(account_email, ''), COALESCE(display_name, ''), COALESCE(tier, 'free'),
	COALESCE(ccu_balance, 0), COALESCE(ccu_burn_rate, 0), COALESCE(eligible_gpus_json, '[]'), COALESCE(eligible_tpus_json, '[]'),
	last_telemetry_at, user_data_dir, cdp_port, COALESCE(chrome_pid, 0), created_at, COALESCE(updated_at, created_at)`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanProfile(r rowScanner) (*domain.Profile, error) {
	var p domain.Profile
	var gpusJSON, tpusJSON string
	var rawTelemetry, rawCreated, rawUpdated any
	err := r.Scan(
		&p.ID, &p.Name, &p.AccountEmail, &p.DisplayName, &p.Tier,
		&p.CCUBalance, &p.CCUBurnRate, &gpusJSON, &tpusJSON,
		&rawTelemetry, &p.UserDataDir, &p.CDPPort, &p.ChromePID, &rawCreated, &rawUpdated,
	)
	if err != nil {
		return nil, mapSQLError(err, "profile")
	}
	p.EligibleGPUs = unmarshalStringSlice(gpusJSON)
	p.EligibleTPUs = unmarshalStringSlice(tpusJSON)
	p.LastTelemetryAt = parseOptionalTime(rawTelemetry)
	p.CreatedAt = parseSQLiteTime(rawCreated)
	p.UpdatedAt = parseSQLiteTime(rawUpdated)
	return &p, nil
}

func (s *Store) CreateProfile(ctx context.Context, p *domain.Profile) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	if p.Tier == "" {
		p.Tier = domain.TierFree
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	if p.EligibleGPUs == nil {
		p.EligibleGPUs = []string{}
	}
	if p.EligibleTPUs == nil {
		p.EligibleTPUs = []string{}
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO profiles (id, name, account_email, display_name, tier, ccu_balance, ccu_burn_rate, eligible_gpus_json, eligible_tpus_json, last_telemetry_at, user_data_dir, cdp_port, chrome_pid, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.AccountEmail, p.DisplayName, p.Tier,
		p.CCUBalance, p.CCUBurnRate, marshalStringSlice(p.EligibleGPUs), marshalStringSlice(p.EligibleTPUs),
		p.LastTelemetryAt, p.UserDataDir, p.CDPPort, p.ChromePID, p.CreatedAt, p.UpdatedAt,
	)
	return mapSQLError(err, "profile")
}

func (s *Store) GetProfile(ctx context.Context, id string) (*domain.Profile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRowContext(ctx, `SELECT `+profileSelectCols+` FROM profiles WHERE id = ?`, id)
	return scanProfile(row)
}

func (s *Store) ListProfiles(ctx context.Context) ([]*domain.Profile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `SELECT `+profileSelectCols+` FROM profiles ORDER BY CASE WHEN id = 'default' THEN 0 ELSE 1 END, created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]*domain.Profile, 0)
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

func (s *Store) UpdateProfile(ctx context.Context, p *domain.Profile) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	p.UpdatedAt = time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`UPDATE profiles SET name = ?, account_email = ?, display_name = ?, tier = ?, user_data_dir = ?, cdp_port = ?, updated_at = ?
		 WHERE id = ?`,
		p.Name, p.AccountEmail, p.DisplayName, p.Tier, p.UserDataDir, p.CDPPort, p.UpdatedAt, p.ID,
	)
	if err != nil {
		return mapSQLError(err, "profile")
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return domain.NewAppError(domain.CodeNotFound, fmt.Sprintf("profile %q not found", p.ID), "", http.StatusNotFound)
	}
	return nil
}

func (s *Store) UpdateProfileRuntimeTelemetry(ctx context.Context, id string, email, displayName string, tier domain.ProfileTier, balance, burnRate float64, eligibleGPUs, eligibleTPUs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`UPDATE profiles SET
			account_email = CASE WHEN ? != '' THEN ? ELSE account_email END,
			display_name = CASE WHEN ? != '' THEN ? ELSE display_name END,
			tier = CASE WHEN ? != '' THEN ? ELSE tier END,
			ccu_balance = ?,
			ccu_burn_rate = ?,
			eligible_gpus_json = ?,
			eligible_tpus_json = ?,
			last_telemetry_at = ?,
			updated_at = ?
		 WHERE id = ?`,
		email, email,
		displayName, displayName,
		tier, tier,
		balance, burnRate, marshalStringSlice(eligibleGPUs), marshalStringSlice(eligibleTPUs), now, now, id,
	)
	return mapSQLError(err, "profile")
}

func (s *Store) SetProfileChromePID(ctx context.Context, id string, pid int, cdpPort int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	if cdpPort > 0 {
		_, err := s.db.ExecContext(ctx, `UPDATE profiles SET chrome_pid = ?, cdp_port = ?, updated_at = ? WHERE id = ?`, pid, cdpPort, now, id)
		return mapSQLError(err, "profile")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE profiles SET chrome_pid = ?, updated_at = ? WHERE id = ?`, pid, now, id)
	return mapSQLError(err, "profile")
}

func (s *Store) DeleteProfile(ctx context.Context, id string) error {
	if id == "default" {
		return domain.NewAppError(domain.CodeConflict, "cannot delete the default account profile", "The 'default' profile is protected.", http.StatusConflict)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var lockedProjectName string
	err := s.db.QueryRowContext(ctx,
		`SELECT p.name FROM projects p
		 JOIN sessions s ON p.locked_session_id = s.id
		 WHERE s.profile_id = ? LIMIT 1`, id,
	).Scan(&lockedProjectName)
	if err == nil && lockedProjectName != "" {
		return domain.NewAppError(
			domain.CodeConflict,
			fmt.Sprintf("cannot delete account %q while it holds the exclusive lock on project %q", id, lockedProjectName),
			"Unlock or switch the project lock to another account before deleting this account.",
			http.StatusConflict,
		)
	}

	var ownedProjectName string
	err = s.db.QueryRowContext(ctx, `SELECT name FROM projects WHERE owner_profile_id = ? LIMIT 1`, id).Scan(&ownedProjectName)
	if err == nil && ownedProjectName != "" {
		return domain.NewAppError(
			domain.CodeConflict,
			fmt.Sprintf("cannot delete account %q because it owns project %q", id, ownedProjectName),
			"Reassign project ownership or delete the project first.",
			http.StatusConflict,
		)
	}

	res, err := s.db.ExecContext(ctx, `DELETE FROM profiles WHERE id = ?`, id)
	if err != nil {
		return mapSQLError(err, "profile")
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return domain.NewAppError(domain.CodeNotFound, fmt.Sprintf("profile %q not found", id), "", http.StatusNotFound)
	}
	return nil
}

func (s *Store) GetNextAvailableCDPPort(ctx context.Context, startPort int) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if startPort <= 0 {
		startPort = 9222
	}
	rows, err := s.db.QueryContext(ctx, `SELECT cdp_port FROM profiles ORDER BY cdp_port ASC`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	used := make(map[int]bool)
	for rows.Next() {
		var port int
		if err := rows.Scan(&port); err == nil {
			used[port] = true
		}
	}
	for port := startPort; ; port++ {
		if !used[port] {
			return port, nil
		}
	}
}

// --- Projects ---

const projectSelectCols = `id, name, COALESCE(notebook_id, ''), notebook_url, owner_profile_id, COALESCE(project_token, ''), COALESCE(locked_session_id, ''), created_at, COALESCE(updated_at, created_at)`

func scanProject(r rowScanner) (*domain.Project, error) {
	var p domain.Project
	var rawCreated, rawUpdated any
	if err := r.Scan(&p.ID, &p.Name, &p.NotebookID, &p.NotebookURL, &p.OwnerProfileID, &p.ProjectToken, &p.LockedSessionID, &rawCreated, &rawUpdated); err != nil {
		return nil, mapSQLError(err, "project")
	}
	p.CreatedAt = parseSQLiteTime(rawCreated)
	p.UpdatedAt = parseSQLiteTime(rawUpdated)
	return &p, nil
}

func (s *Store) CreateProject(ctx context.Context, p *domain.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	if p.NotebookID == "" || p.NotebookURL == "" {
		nid, canonURL, err := domain.NormalizeNotebookURL(p.NotebookURL)
		if err != nil {
			return err
		}
		p.NotebookID = nid
		p.NotebookURL = canonURL
	}
	if p.Name == "" {
		p.Name = domain.DefaultNotebookTitle(p.NotebookID)
	}
	if p.ProjectToken == "" {
		tok, err := domain.GenerateToken(domain.TokenPrefixProject)
		if err != nil {
			return err
		}
		p.ProjectToken = tok
	}
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now

	var lockedVal any
	if p.LockedSessionID != "" {
		lockedVal = p.LockedSessionID
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO projects (id, name, notebook_id, notebook_url, owner_profile_id, project_token, locked_session_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.NotebookID, p.NotebookURL, p.OwnerProfileID, p.ProjectToken, lockedVal, p.CreatedAt, p.UpdatedAt,
	)
	return mapSQLError(err, "project")
}

func (s *Store) GetProject(ctx context.Context, id string) (*domain.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return scanProject(s.db.QueryRowContext(ctx, `SELECT `+projectSelectCols+` FROM projects WHERE id = ?`, id))
}

func (s *Store) GetProjectByNotebookID(ctx context.Context, notebookID string) (*domain.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return scanProject(s.db.QueryRowContext(ctx, `SELECT `+projectSelectCols+` FROM projects WHERE notebook_id = ?`, notebookID))
}

func (s *Store) GetProjectByToken(ctx context.Context, token string) (*domain.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	p, err := scanProject(s.db.QueryRowContext(ctx, `SELECT `+projectSelectCols+` FROM projects WHERE project_token = ?`, token))
	if err != nil {
		var appErr *domain.AppError
		if errors.As(err, &appErr) && appErr.Code == domain.CodeNotFound {
			return nil, domain.NewAppError(domain.CodeInvalidToken, "unrecognized project MCP token", "Copy the current glp_... token from the Project MCP Config dialog in GoLab UI.", http.StatusUnauthorized)
		}
		return nil, err
	}
	return p, nil
}

func (s *Store) ListProjects(ctx context.Context) ([]*domain.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `SELECT `+projectSelectCols+` FROM projects ORDER BY updated_at DESC, created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]*domain.Project, 0)
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

func (s *Store) UpdateProject(ctx context.Context, p *domain.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	p.UpdatedAt = time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`UPDATE projects SET name = ?, owner_profile_id = ?, updated_at = ? WHERE id = ?`,
		p.Name, p.OwnerProfileID, p.UpdatedAt, p.ID,
	)
	if err != nil {
		return mapSQLError(err, "project")
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return domain.NewAppError(domain.CodeNotFound, fmt.Sprintf("project %q not found", p.ID), "", http.StatusNotFound)
	}
	return nil
}

func (s *Store) RotateProjectToken(ctx context.Context, id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tok, err := domain.GenerateToken(domain.TokenPrefixProject)
	if err != nil {
		return "", err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE projects SET project_token = ?, updated_at = ? WHERE id = ?`, tok, time.Now().UTC(), id)
	if err != nil {
		return "", mapSQLError(err, "project")
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return "", domain.NewAppError(domain.CodeNotFound, fmt.Sprintf("project %q not found", id), "", http.StatusNotFound)
	}
	return tok, nil
}

func (s *Store) DeleteProject(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `UPDATE projects SET locked_session_id = NULL WHERE id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE project_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return mapSQLError(err, "project")
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return domain.NewAppError(domain.CodeNotFound, fmt.Sprintf("project %q not found", id), "", http.StatusNotFound)
	}
	return tx.Commit()
}

// --- Sessions & Per-Project Exclusive Lock ---

const sessionSelectCols = `s.id, COALESCE(s.project_id, ''), s.profile_id, s.name, COALESCE(s.notebook_id, COALESCE(p.notebook_id, '')),
	CASE WHEN COALESCE(s.notebook_url, '') != '' THEN s.notebook_url ELSE COALESCE(p.notebook_url, '') END,
	COALESCE(s.session_token, ''), COALESCE(s.tab_id, ''),
	CASE WHEN p.locked_session_id = s.id THEN 1 ELSE 0 END AS is_locked,
	COALESCE(s.permission, 'unknown'), COALESCE(s.connection_state, 'browser_stopped'),
	s.created_at, s.last_attached_at`

const sessionFromJoin = `FROM sessions s LEFT JOIN projects p ON p.id = s.project_id`

func scanSession(r rowScanner) (*domain.Session, error) {
	var sess domain.Session
	var isLockedInt int
	var rawCreated, rawAttached any
	err := r.Scan(
		&sess.ID, &sess.ProjectID, &sess.ProfileID, &sess.Name, &sess.NotebookID,
		&sess.NotebookURL, &sess.SessionToken, &sess.TabID, &isLockedInt,
		&sess.Permission, &sess.ConnectionState, &rawCreated, &rawAttached,
	)
	if err != nil {
		return nil, mapSQLError(err, "session")
	}
	sess.IsLocked = isLockedInt == 1
	sess.CreatedAt = parseSQLiteTime(rawCreated)
	sess.LastAttachedAt = parseOptionalTime(rawAttached)
	return &sess, nil
}

func (s *Store) CreateSession(ctx context.Context, sess *domain.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sess.ID == "" {
		sess.ID = uuid.New().String()
	}
	if sess.ProjectID == "" || sess.ProfileID == "" {
		return domain.NewAppError(domain.CodeInvalidInput, "project_id and profile_id are required", "", http.StatusBadRequest)
	}

	// Always verify parent Project exists and inherit notebook_id / notebook_url if omitted.
	var nid, nurl string
	if err := s.db.QueryRowContext(ctx, `SELECT notebook_id, notebook_url FROM projects WHERE id = ?`, sess.ProjectID).Scan(&nid, &nurl); err != nil {
		return mapSQLError(err, "project")
	}
	if sess.NotebookID == "" {
		sess.NotebookID = nid
	}
	if sess.NotebookURL == "" {
		sess.NotebookURL = nurl
	}
	if sess.Name == "" {
		var profName string
		_ = s.db.QueryRowContext(ctx, `SELECT name FROM profiles WHERE id = ?`, sess.ProfileID).Scan(&profName)
		if profName != "" {
			sess.Name = profName
		} else {
			sess.Name = "Account Session"
		}
	}
	if sess.SessionToken == "" {
		tok, err := domain.GenerateToken(domain.TokenPrefixSession)
		if err != nil {
			return err
		}
		sess.SessionToken = tok
	}
	if sess.Permission == "" {
		sess.Permission = domain.PermissionUnknown
	}
	if sess.ConnectionState == "" {
		sess.ConnectionState = domain.StateBrowserStopped
	}
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = time.Now().UTC()
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, project_id, profile_id, name, notebook_id, notebook_url, session_token, tab_id, permission, connection_state, created_at, last_attached_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.ProjectID, sess.ProfileID, sess.Name, sess.NotebookID, sess.NotebookURL,
		sess.SessionToken, sess.TabID, sess.Permission, sess.ConnectionState, sess.CreatedAt, sess.LastAttachedAt,
	)
	return mapSQLError(err, "session")
}

func (s *Store) GetSession(ctx context.Context, id string) (*domain.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return scanSession(s.db.QueryRowContext(ctx, `SELECT `+sessionSelectCols+` `+sessionFromJoin+` WHERE s.id = ?`, id))
}

func (s *Store) GetSessionByPair(ctx context.Context, projectID, profileID string) (*domain.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return scanSession(s.db.QueryRowContext(ctx, `SELECT `+sessionSelectCols+` `+sessionFromJoin+` WHERE s.project_id = ? AND s.profile_id = ?`, projectID, profileID))
}

func (s *Store) GetSessionByToken(ctx context.Context, token string) (*domain.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, err := scanSession(s.db.QueryRowContext(ctx, `SELECT `+sessionSelectCols+` `+sessionFromJoin+` WHERE s.session_token = ?`, token))
	if err != nil {
		var appErr *domain.AppError
		if errors.As(err, &appErr) && appErr.Code == domain.CodeNotFound {
			return nil, domain.NewAppError(domain.CodeInvalidToken, "unrecognized session MCP token", "Copy the current gls_... token from the Project Sessions table in GoLab UI.", http.StatusUnauthorized)
		}
		return nil, err
	}
	return sess, nil
}

func (s *Store) ListSessions(ctx context.Context, projectID string) ([]*domain.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var (
		rows *sql.Rows
		err  error
	)
	if strings.TrimSpace(projectID) != "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT `+sessionSelectCols+` `+sessionFromJoin+` WHERE s.project_id = ? ORDER BY is_locked DESC, s.created_at ASC`,
			projectID,
		)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT `+sessionSelectCols+` `+sessionFromJoin+` ORDER BY is_locked DESC, s.created_at ASC`,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]*domain.Session, 0)
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, sess)
	}
	return list, rows.Err()
}

func (s *Store) UpdateSessionState(ctx context.Context, id string, tabID string, perm domain.PermissionLevel, state domain.ConnectionState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var attachedAt any
	if tabID != "" && state != domain.StateBrowserStopped && state != domain.StateTabClosed {
		attachedAt = time.Now().UTC()
	}

	res, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET
			tab_id = ?,
			permission = CASE WHEN ? != '' THEN ? ELSE permission END,
			connection_state = CASE WHEN ? != '' THEN ? ELSE connection_state END,
			last_attached_at = COALESCE(?, last_attached_at)
		 WHERE id = ?`,
		tabID, perm, perm, state, state, attachedAt, id,
	)
	if err != nil {
		return mapSQLError(err, "session")
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return domain.NewAppError(domain.CodeNotFound, fmt.Sprintf("session %q not found", id), "", http.StatusNotFound)
	}
	return nil
}

func (s *Store) RotateSessionToken(ctx context.Context, id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tok, err := domain.GenerateToken(domain.TokenPrefixSession)
	if err != nil {
		return "", err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE sessions SET session_token = ? WHERE id = ?`, tok, id)
	if err != nil {
		return "", mapSQLError(err, "session")
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return "", domain.NewAppError(domain.CodeNotFound, fmt.Sprintf("session %q not found", id), "", http.StatusNotFound)
	}
	return tok, nil
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, _ = tx.ExecContext(ctx, `UPDATE projects SET locked_session_id = NULL, updated_at = ? WHERE locked_session_id = ?`, time.Now().UTC(), id)
	res, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return mapSQLError(err, "session")
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return domain.NewAppError(domain.CodeNotFound, fmt.Sprintf("session %q not found", id), "", http.StatusNotFound)
	}
	return tx.Commit()
}

// AcquireProjectLock atomically acquires or switches the single-account exclusive lock for a session's Project.
func (s *Store) AcquireProjectLock(ctx context.Context, sessionID string, forceSwitch bool) (prevLocked *domain.Session, updatedProject *domain.Project, lockedSession *domain.Session, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return nil, nil, nil, fmt.Errorf("begin immediate tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()

	targetSess, err := scanSession(conn.QueryRowContext(ctx, `SELECT `+sessionSelectCols+` `+sessionFromJoin+` WHERE s.id = ?`, sessionID))
	if err != nil {
		return nil, nil, nil, err
	}

	proj, err := scanProject(conn.QueryRowContext(ctx, `SELECT `+projectSelectCols+` FROM projects WHERE id = ?`, targetSess.ProjectID))
	if err != nil {
		return nil, nil, nil, err
	}

	if proj.LockedSessionID != "" && proj.LockedSessionID != targetSess.ID {
		prev, prevErr := scanSession(conn.QueryRowContext(ctx, `SELECT `+sessionSelectCols+` `+sessionFromJoin+` WHERE s.id = ?`, proj.LockedSessionID))
		if prevErr == nil {
			prevLocked = prev
		}
		if !forceSwitch {
			holder := proj.LockedSessionID
			if prevLocked != nil {
				holder = fmt.Sprintf("%s (%s)", prevLocked.Name, prevLocked.ProfileID)
			}
			return prevLocked, nil, nil, domain.NewAppError(
				domain.CodeProjectLockedByOtherAccount,
				fmt.Sprintf("project %q is already locked by %s", proj.Name, holder),
				"Use Switch Lock (force_switch=true) to flush save, close the previous account's tab, and transfer the exclusive lock.",
				http.StatusConflict,
			)
		}
	}

	now := time.Now().UTC()
	if _, err := conn.ExecContext(ctx, `UPDATE projects SET locked_session_id = ?, updated_at = ? WHERE id = ?`, targetSess.ID, now, proj.ID); err != nil {
		return nil, nil, nil, mapSQLError(err, "project")
	}

	updatedProj, err := scanProject(conn.QueryRowContext(ctx, `SELECT `+projectSelectCols+` FROM projects WHERE id = ?`, proj.ID))
	if err != nil {
		return nil, nil, nil, err
	}
	updatedSess, err := scanSession(conn.QueryRowContext(ctx, `SELECT `+sessionSelectCols+` `+sessionFromJoin+` WHERE s.id = ?`, targetSess.ID))
	if err != nil {
		return nil, nil, nil, err
	}

	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return nil, nil, nil, fmt.Errorf("commit lock tx: %w", err)
	}
	committed = true
	return prevLocked, updatedProj, updatedSess, nil
}

// ReleaseProjectLock clears locked_session_id for the specified project.
func (s *Store) ReleaseProjectLock(ctx context.Context, projectID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `UPDATE projects SET locked_session_id = NULL, updated_at = ? WHERE id = ?`, now, projectID)
	if err != nil {
		return mapSQLError(err, "project")
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return domain.NewAppError(domain.CodeNotFound, fmt.Sprintf("project %q not found", projectID), "", http.StatusNotFound)
	}
	return nil
}

// ListLockedSessions returns all sessions currently holding their project's exclusive lock.
func (s *Store) ListLockedSessions(ctx context.Context) ([]*domain.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx,
		`SELECT `+sessionSelectCols+` FROM sessions s JOIN projects p ON p.id = s.project_id AND p.locked_session_id = s.id ORDER BY p.updated_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]*domain.Session, 0)
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, sess)
	}
	return list, rows.Err()
}

// --- Activity Audit Logs ---

const activitySelectCols = `id, COALESCE(session_id, ''), COALESCE(project_id, ''), COALESCE(profile_id, ''),
	COALESCE(token_prefix, ''), tool_name, status, COALESCE(error_code, ''), COALESCE(error_message, ''),
	COALESCE(latency_ms, 0), (request_json IS NOT NULL OR response_json IS NOT NULL), created_at`

func scanActivity(r rowScanner) (*domain.ActivityLog, error) {
	var a domain.ActivityLog
	var rawCreated any
	err := r.Scan(
		&a.ID, &a.SessionID, &a.ProjectID, &a.ProfileID,
		&a.TokenPrefix, &a.ToolName, &a.Status, &a.ErrorCode, &a.ErrorMessage,
		&a.LatencyMs, &a.HasPayload, &rawCreated,
	)
	if err != nil {
		return nil, mapSQLError(err, "activity_log")
	}
	a.CreatedAt = parseSQLiteTime(rawCreated)
	return &a, nil
}

func (s *Store) AppendActivity(ctx context.Context, rec *domain.ActivityRecord) (*domain.ActivityLog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	created := rec.CreatedAt.UTC()
	if created.IsZero() {
		created = time.Now().UTC()
	}
	reqBody, reqBytes, reqTrunc := payloadColumns(rec.Request)
	respBody, respBytes, respTrunc := payloadColumns(rec.Response)

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO activity_logs (
			session_id, project_id, profile_id, token_prefix, tool_name, status, error_code, error_message, latency_ms,
			request_json, request_bytes, request_truncated, response_json, response_bytes, response_truncated, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.SessionID, rec.ProjectID, rec.ProfileID, rec.TokenPrefix, rec.ToolName, rec.Status,
		rec.ErrorCode, rec.ErrorMessage, rec.LatencyMs,
		reqBody, reqBytes, reqTrunc, respBody, respBytes, respTrunc, created,
	)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &domain.ActivityLog{
		ID:           id,
		SessionID:    rec.SessionID,
		ProjectID:    rec.ProjectID,
		ProfileID:    rec.ProfileID,
		TokenPrefix:  rec.TokenPrefix,
		ToolName:     rec.ToolName,
		Status:       rec.Status,
		ErrorCode:    rec.ErrorCode,
		ErrorMessage: rec.ErrorMessage,
		LatencyMs:    rec.LatencyMs,
		HasPayload:   rec.Request != nil || rec.Response != nil,
		CreatedAt:    created,
	}, nil
}

func (s *Store) ListActivity(ctx context.Context, filter domain.ActivityFilter) ([]*domain.ActivityLog, int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	var clauses []string
	var args []any

	if filter.Cursor > 0 {
		clauses = append(clauses, `id < ?`)
		args = append(args, filter.Cursor)
	}
	if v := strings.TrimSpace(filter.ProjectID); v != "" {
		clauses = append(clauses, `project_id = ?`)
		args = append(args, v)
	}
	if v := strings.TrimSpace(filter.SessionID); v != "" {
		clauses = append(clauses, `session_id = ?`)
		args = append(args, v)
	}
	if v := strings.TrimSpace(filter.ProfileID); v != "" {
		clauses = append(clauses, `profile_id = ?`)
		args = append(args, v)
	}
	if v := strings.TrimSpace(filter.Status); v != "" {
		clauses = append(clauses, `status = ?`)
		args = append(args, v)
	}
	if v := strings.TrimSpace(filter.ToolName); v != "" {
		clauses = append(clauses, `LOWER(tool_name) LIKE ?`)
		args = append(args, "%"+strings.ToLower(v)+"%")
	}

	query := `SELECT ` + activitySelectCols + ` FROM activity_logs`
	if len(clauses) > 0 {
		query += ` WHERE ` + strings.Join(clauses, ` AND `)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]*domain.ActivityLog, 0, limit)
	for rows.Next() {
		item, err := scanActivity(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	var nextCursor int64
	if len(items) > limit {
		items = items[:limit]
		nextCursor = items[len(items)-1].ID
	}
	return items, nextCursor, nil
}

func (s *Store) GetActivity(ctx context.Context, id int64) (*domain.ActivityDetail, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `SELECT ` + activitySelectCols + `,
		request_json, COALESCE(request_bytes, 0), COALESCE(request_truncated, 0),
		response_json, COALESCE(response_bytes, 0), COALESCE(response_truncated, 0)
	FROM activity_logs WHERE id = ?`

	var d domain.ActivityDetail
	var rawCreated any
	var reqBody, respBody sql.NullString
	var reqBytes, respBytes int
	var reqTrunc, respTrunc bool

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&d.ID, &d.SessionID, &d.ProjectID, &d.ProfileID,
		&d.TokenPrefix, &d.ToolName, &d.Status, &d.ErrorCode, &d.ErrorMessage,
		&d.LatencyMs, &d.HasPayload, &rawCreated,
		&reqBody, &reqBytes, &reqTrunc, &respBody, &respBytes, &respTrunc,
	)
	if err != nil {
		return nil, mapSQLError(err, "activity_log")
	}
	d.CreatedAt = parseSQLiteTime(rawCreated)
	d.Request = payloadFromColumns(reqBody, reqBytes, reqTrunc)
	d.Response = payloadFromColumns(respBody, respBytes, respTrunc)
	return &d, nil
}

func (s *Store) CountActivity(ctx context.Context) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM activity_logs`).Scan(&count)
	return count, err
}

// Compile-time check that *Store satisfies domain.Store.
var _ domain.Store = (*Store)(nil)

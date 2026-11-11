package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hoangnecon/golabv2/internal/domain"
	"github.com/hoangnecon/golabv2/internal/store/sqlite"
	_ "modernc.org/sqlite"
)

func TestWALAndForeignKeysEnabled(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "golab_sec")
	dbPath := filepath.Join(dir, "state.db")

	st, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer st.Close()

	if st.JournalMode() != "wal" {
		t.Fatalf("expected journal_mode=wal, got %q", st.JournalMode())
	}
	if !st.ForeignKeysEnabled() {
		t.Fatal("expected foreign_keys=ON")
	}

	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("stat db: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("expected db file permissions 0600, got %o", mode)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if mode := dirInfo.Mode().Perm(); mode != 0o700 {
		t.Fatalf("expected dir permissions 0700, got %o", mode)
	}
}

func TestLegacySchemaMigrationAndOrphanSessionBackfill(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "legacy_oct29.db")

	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy sql: %v", err)
	}
	legacyDDL := `
	CREATE TABLE profiles (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		account_email TEXT,
		tier TEXT DEFAULT 'free',
		user_data_dir TEXT NOT NULL,
		cdp_port INTEGER NOT NULL UNIQUE,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE projects (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		notebook_url TEXT NOT NULL UNIQUE,
		owner_profile_id TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE sessions (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		profile_id TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
		notebook_url TEXT NOT NULL,
		notebook_id TEXT,
		tab_id TEXT,
		is_active BOOLEAN DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_attached_at DATETIME
	);
	CREATE TABLE activity_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT,
		tool_name TEXT NOT NULL,
		latency_ms INTEGER,
		status TEXT NOT NULL,
		error_message TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	INSERT INTO profiles (id, name, account_email, tier, user_data_dir, cdp_port)
	VALUES ('default', 'Default Account', 'owner@gmail.com', 'pro', '/tmp/prof_default', 9222);
	INSERT INTO sessions (id, name, profile_id, notebook_url, tab_id, is_active)
	VALUES ('orphan_sess_1', 'Legacy Training', 'default', 'https://colab.research.google.com/drive/1YsugLzQOOw3CJUOiqhVJM0U7fP62x4Du?usp=sharing#scrollTo=123', 'tab_abc', 1);
	`
	if _, err := rawDB.Exec(legacyDDL); err != nil {
		rawDB.Close()
		t.Fatalf("seed legacy DDL: %v", err)
	}
	rawDB.Close()

	st, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open on legacy DB failed: %v", err)
	}
	defer st.Close()

	projects, err := st.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 backfilled project from orphan session, got %d", len(projects))
	}
	bp := projects[0]
	if bp.NotebookID != "drive/1YsugLzQOOw3CJUOiqhVJM0U7fP62x4Du" {
		t.Fatalf("expected normalized notebook_id, got %q", bp.NotebookID)
	}
	if !strings.HasPrefix(bp.ProjectToken, "glp_") {
		t.Fatalf("expected backfilled project_token glp_*, got %q", bp.ProjectToken)
	}

	sess, err := st.GetSession(ctx, "orphan_sess_1")
	if err != nil {
		t.Fatalf("GetSession(orphan_sess_1): %v", err)
	}
	if sess.ProjectID != bp.ID {
		t.Fatalf("expected orphan session linked to backfilled project %q, got %q", bp.ID, sess.ProjectID)
	}
	if !strings.HasPrefix(sess.SessionToken, "gls_") {
		t.Fatalf("expected backfilled session_token gls_*, got %q", sess.SessionToken)
	}
}

func TestPerProjectExclusiveLockConcurrency(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "locks.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	for i := 1; i <= 3; i++ {
		if err := st.CreateProfile(ctx, &domain.Profile{
			ID:          fmt.Sprintf("acc_%d", i),
			Name:        fmt.Sprintf("Account %d", i),
			Tier:        domain.TierPro,
			UserDataDir: fmt.Sprintf("/tmp/u_%d", i),
			CDPPort:     9220 + i,
		}); err != nil {
			t.Fatalf("CreateProfile %d: %v", i, err)
		}
	}

	projA := &domain.Project{
		ID:             "proj_a",
		Name:           "Project Alpha",
		NotebookID:     "drive/alpha1234567890123",
		NotebookURL:    "https://colab.research.google.com/drive/alpha1234567890123",
		OwnerProfileID: "acc_1",
	}
	projB := &domain.Project{
		ID:             "proj_b",
		Name:           "Project Beta",
		NotebookID:     "drive/beta12345678901234",
		NotebookURL:    "https://colab.research.google.com/drive/beta12345678901234",
		OwnerProfileID: "acc_2",
	}
	if err := st.CreateProject(ctx, projA); err != nil {
		t.Fatalf("CreateProject A: %v", err)
	}
	if err := st.CreateProject(ctx, projB); err != nil {
		t.Fatalf("CreateProject B: %v", err)
	}

	sessIDs := make([]string, 3)
	for i := 1; i <= 3; i++ {
		id := fmt.Sprintf("sess_a_%d", i)
		sessIDs[i-1] = id
		if err := st.CreateSession(ctx, &domain.Session{
			ID:        id,
			ProjectID: projA.ID,
			ProfileID: fmt.Sprintf("acc_%d", i),
			Name:      fmt.Sprintf("Alpha Acc %d", i),
		}); err != nil {
			t.Fatalf("CreateSession %s: %v", id, err)
		}
	}
	if err := st.CreateSession(ctx, &domain.Session{
		ID:        "sess_b_2",
		ProjectID: projB.ID,
		ProfileID: "acc_2",
		Name:      "Beta Acc 2",
	}); err != nil {
		t.Fatalf("CreateSession B: %v", err)
	}

	// Duplicate (project_id, profile_id) must fail with conflict.
	errDup := st.CreateSession(ctx, &domain.Session{
		ID:        "sess_dup",
		ProjectID: projA.ID,
		ProfileID: "acc_1",
		Name:      "Dup",
	})
	if errDup == nil {
		t.Fatal("expected UNIQUE(project_id, profile_id) violation")
	}

	// 20 goroutines racing to lock Project A across 3 DIFFERENT accounts, plus concurrent lock on Project B.
	var winsA atomic.Int32
	var conflictsA atomic.Int32
	var otherErrs atomic.Int32
	var wg sync.WaitGroup

	wg.Add(21)
	for g := 0; g < 20; g++ {
		targetSess := sessIDs[g%3]
		go func(sid string) {
			defer wg.Done()
			_, _, _, err := st.AcquireProjectLock(ctx, sid, false)
			if err == nil {
				winsA.Add(1)
				return
			}
			var appErr *domain.AppError
			if errors.As(err, &appErr) && appErr.Code == domain.CodeProjectLockedByOtherAccount {
				conflictsA.Add(1)
				return
			}
			t.Logf("unexpected error in race: %v", err)
			otherErrs.Add(1)
		}(targetSess)
	}

	var errProjB error
	go func() {
		defer wg.Done()
		_, _, _, errProjB = st.AcquireProjectLock(ctx, "sess_b_2", false)
	}()

	wg.Wait()

	if errProjB != nil {
		t.Fatalf("Project B lock should succeed independently: %v", errProjB)
	}
	if otherErrs.Load() != 0 {
		t.Fatalf("unexpected errors during lock race: %d", otherErrs.Load())
	}
	if winsA.Load() < 1 {
		t.Fatalf("expected at least 1 lock winner on Project A, got 0")
	}
	// Verify exactly 1 session holds the lock on Project A!
	gotProjA, err := st.GetProject(ctx, projA.ID)
	if err != nil {
		t.Fatalf("GetProject A: %v", err)
	}
	if gotProjA.LockedSessionID == "" {
		t.Fatal("Project A must have a locked_session_id")
	}

	// Pick a different account session on Project A: forceSwitch=false MUST fail, forceSwitch=true MUST transfer!
	otherSess := "sess_a_2"
	if gotProjA.LockedSessionID == "sess_a_2" {
		otherSess = "sess_a_3"
	}
	if _, _, _, err := st.AcquireProjectLock(ctx, otherSess, false); err == nil {
		t.Fatal("expected forceSwitch=false to fail with GOLAB_PROJECT_LOCKED_BY_OTHER_ACCOUNT")
	}

	prev, updatedProj, lockedSess, err := st.AcquireProjectLock(ctx, otherSess, true)
	if err != nil {
		t.Fatalf("AcquireProjectLock(forceSwitch=true) failed: %v", err)
	}
	if prev == nil || prev.ID != gotProjA.LockedSessionID {
		t.Fatalf("expected prevLocked=%q, got %+v", gotProjA.LockedSessionID, prev)
	}
	if updatedProj.LockedSessionID != otherSess || !lockedSess.IsLocked {
		t.Fatalf("expected lock transferred to %q, got proj=%+v sess=%+v", otherSess, updatedProj, lockedSess)
	}

	lockedList, err := st.ListLockedSessions(ctx)
	if err != nil || len(lockedList) != 2 {
		t.Fatalf("expected 2 locked sessions across Project A & Project B, got %d (%v)", len(lockedList), err)
	}
}

func TestDeleteDefaultOrLockedProfileBlocked(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "protections.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	_ = st.CreateProfile(ctx, &domain.Profile{ID: "default", Name: "Default", Tier: domain.TierFree, UserDataDir: "/tmp/def", CDPPort: 9222})
	_ = st.CreateProfile(ctx, &domain.Profile{ID: "acc_locked", Name: "Locked Acc", Tier: domain.TierPro, UserDataDir: "/tmp/lk", CDPPort: 9223})

	if err := st.DeleteProfile(ctx, "default"); err == nil {
		t.Fatal("deleting default profile MUST be blocked")
	}

	_ = st.CreateProject(ctx, &domain.Project{
		ID:             "p1",
		Name:           "P1",
		NotebookID:     "drive/1234567890123456",
		NotebookURL:    "https://colab.research.google.com/drive/1234567890123456",
		OwnerProfileID: "acc_locked",
	})
	_ = st.CreateSession(ctx, &domain.Session{ID: "s1", ProjectID: "p1", ProfileID: "acc_locked", Name: "S1"})
	if _, _, _, err := st.AcquireProjectLock(ctx, "s1", false); err != nil {
		t.Fatalf("lock s1: %v", err)
	}

	if err := st.DeleteProfile(ctx, "acc_locked"); err == nil {
		t.Fatal("deleting profile holding an active Project Lock MUST be blocked")
	}

	if err := st.ReleaseProjectLock(ctx, "p1"); err != nil {
		t.Fatalf("ReleaseProjectLock: %v", err)
	}
	// Still owner_profile_id of p1 unless project deleted/reassigned; delete project first then profile.
	if err := st.DeleteProject(ctx, "p1"); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if err := st.DeleteProfile(ctx, "acc_locked"); err != nil {
		t.Fatalf("DeleteProfile after unlock & project removal failed: %v", err)
	}
}

func TestActivityLogCursorPaginationAndPreSessionErrors(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "activity.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	// 1. Pre-resolution error call (empty session_id / project_id)
	errLog, err := st.AppendActivity(ctx, &domain.ActivityRecord{
		TokenPrefix:  "glp_deadbeef...",
		ToolName:     "colab_get_cells",
		Status:       "error",
		ErrorCode:    domain.CodeAmbiguousMultiProjectSession,
		ErrorMessage: "Multiple projects locked",
		LatencyMs:    2,
		Request:      &domain.Payload{Body: []byte(`{"start_index":0}`), Bytes: 18},
		Response:     &domain.Payload{Body: []byte(`{"status":"error"}`), Bytes: 18},
		CreatedAt:    time.Now().UTC(),
	})
	if err != nil || errLog.ID <= 0 {
		t.Fatalf("AppendActivity pre-session error: %v", err)
	}

	// 2. Append 4 normal calls on project p1
	for i := 0; i < 4; i++ {
		_, err := st.AppendActivity(ctx, &domain.ActivityRecord{
			ProjectID:   "p1",
			SessionID:   "s1",
			ProfileID:   "default",
			TokenPrefix: "glp_alpha123...",
			ToolName:    fmt.Sprintf("colab_tool_%d", i),
			Status:      "success",
			LatencyMs:   int64(10 + i),
			Request:     &domain.Payload{Body: []byte(`{}`), Bytes: 2},
			Response:    &domain.Payload{Body: []byte(`{"ok":true}`), Bytes: 11},
		})
		if err != nil {
			t.Fatalf("AppendActivity %d: %v", i, err)
		}
	}

	page1, cursor1, err := st.ListActivity(ctx, domain.ActivityFilter{Limit: 3})
	if err != nil || len(page1) != 3 || cursor1 == 0 {
		t.Fatalf("page1 unexpected: len=%d cursor=%d err=%v", len(page1), cursor1, err)
	}
	page2, cursor2, err := st.ListActivity(ctx, domain.ActivityFilter{Cursor: cursor1, Limit: 3})
	if err != nil || len(page2) != 2 || cursor2 != 0 {
		t.Fatalf("page2 unexpected: len=%d cursor=%d err=%v", len(page2), cursor2, err)
	}

	errOnly, _, err := st.ListActivity(ctx, domain.ActivityFilter{Status: "error"})
	if err != nil || len(errOnly) != 1 || errOnly[0].ErrorCode != domain.CodeAmbiguousMultiProjectSession {
		t.Fatalf("Status=error filter mismatch: %+v (%v)", errOnly, err)
	}

	detail, err := st.GetActivity(ctx, errLog.ID)
	if err != nil || detail.ErrorCode != domain.CodeAmbiguousMultiProjectSession || detail.Request == nil {
		t.Fatalf("GetActivity detail mismatch: %+v (%v)", detail, err)
	}
}

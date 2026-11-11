package service_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoangnecon/golabv2/internal/domain"
	"github.com/hoangnecon/golabv2/internal/service"
	"github.com/hoangnecon/golabv2/internal/session"
	"github.com/hoangnecon/golabv2/internal/store/sqlite"
)

type noopBrowser struct{}

func (noopBrowser) LaunchProfile(ctx context.Context, profile domain.Profile, initialURL string) (int, int, error) {
	return 5555, profile.CDPPort, nil
}
func (noopBrowser) StopProfile(ctx context.Context, profile domain.Profile) error { return nil }
func (noopBrowser) IsProfileRunning(ctx context.Context, cdpPort int) bool        { return false }
func (noopBrowser) ListTargets(ctx context.Context, cdpPort int) ([]domain.CDPTarget, error) {
	return nil, nil
}
func (noopBrowser) OpenOrFocusTab(ctx context.Context, cdpPort int, targetURL string, matchSubstr string) (domain.CDPTarget, error) {
	return domain.CDPTarget{ID: "t1", URL: targetURL}, nil
}
func (noopBrowser) FocusTab(ctx context.Context, cdpPort int, tabID string) error { return nil }
func (noopBrowser) CloseTab(ctx context.Context, cdpPort int, tabID string) error { return nil }
func (noopBrowser) ShutdownAdapter() error                                        { return nil }

type noopDriverFactory struct{}

func (noopDriverFactory) NewDriver(ctx context.Context, cdpPort int, target domain.CDPTarget) (domain.RuntimeDriver, error) {
	return nil, errors.New("no live cdp in unit test")
}

func TestControlPlaneApplicationServices(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "services_test.db")

	st, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer st.Close()

	bus := session.NewEventBus()
	browser := noopBrowser{}
	coord := session.NewCoordinator(st, browser, noopDriverFactory{}, bus, session.CoordinatorOptions{})
	defer coord.Close()

	profileSvc := service.NewProfileService(st, browser, coord, bus, filepath.Join(tmp, "profiles"))
	projectSvc := service.NewProjectService(st, coord, bus)
	sessionSvc := service.NewSessionService(st, coord, bus)
	activitySvc := service.NewActivityService(st, bus)
	workspaceSvc := service.NewWorkspaceService(st, browser, "v2.0.0", "http://127.0.0.1:9120/mcp")

	// 1. Create Profile
	prof, err := profileSvc.CreateProfile(ctx, "Research Account", "res@gmail.com", domain.TierPro)
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	if prof.Name != "Research Account" || prof.CDPPort != 9222 {
		t.Fatalf("unexpected profile: %+v", prof)
	}

	// 2. Create Project -> auto-normalizes notebook_id AND auto-creates initial Owner Session!
	proj, ownerSess, err := projectSvc.CreateProject(ctx, "LLM Eval", "https://colab.research.google.com/drive/1YsugLzQOOw3CJUOiqhVJM0U7fP62x4Du?usp=sharing#scrollTo=xyz", prof.ID, false)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if proj.NotebookID != "drive/1YsugLzQOOw3CJUOiqhVJM0U7fP62x4Du" || !strings.HasPrefix(proj.ProjectToken, "glp_") {
		t.Fatalf("unexpected project: %+v", proj)
	}
	if ownerSess == nil || ownerSess.ProfileID != prof.ID || !strings.HasPrefix(ownerSess.SessionToken, "gls_") {
		t.Fatalf("expected auto-created owner session, got %+v", ownerSess)
	}

	// 3. Invalid project URL rejected
	if _, _, errBad := projectSvc.CreateProject(ctx, "Bad", "https://example.com/not-colab", prof.ID, false); errBad == nil {
		t.Fatalf("expected invalid notebook URL error")
	}

	// 4. Add second Account Session to same Project
	prof2, err := profileSvc.CreateProfile(ctx, "Account 2", "acc2@gmail.com", domain.TierFree)
	if err != nil {
		t.Fatalf("CreateProfile 2: %v", err)
	}
	sess2, err := sessionSvc.CreateSession(ctx, proj.ID, prof2.ID, "Editor 2", false)
	if err != nil {
		t.Fatalf("CreateSession 2: %v", err)
	}
	if sess2.ProjectID != proj.ID {
		t.Fatalf("unexpected sess2: %+v", sess2)
	}

	// 5. Record Activity & Workspace Snapshot
	entry, err := activitySvc.Append(ctx, &domain.ActivityRecord{
		ProjectID:   proj.ID,
		SessionID:   ownerSess.ID,
		ProfileID:   prof.ID,
		TokenPrefix: domain.MaskTokenPrefix(proj.ProjectToken),
		ToolName:    "colab_get_cells",
		Status:      "success",
		LatencyMs:   18,
		Request:     &domain.Payload{Body: []byte(`{"start":0}`), Bytes: 11},
		Response:    &domain.Payload{Body: []byte(`{"status":"success"}`), Bytes: 20},
	})
	if err != nil || entry.ID == 0 {
		t.Fatalf("Append activity: %+v, err=%v", entry, err)
	}

	ws, err := workspaceSvc.GetSnapshot(ctx)
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}
	if len(ws.Profiles) != 2 || len(ws.Projects) != 1 || len(ws.Sessions) != 2 || ws.RecentLogsCount != 1 {
		t.Fatalf("unexpected workspace snapshot: %+v", ws)
	}
}

package service

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hoangnecon/golabv2/internal/domain"
)

// ProfileService manages isolated Google Account / Chrome browser profiles.
type ProfileService struct {
	store          domain.Store
	browser        domain.BrowserManager
	coord          domain.SessionCoordinator
	bus            domain.EventBus
	profilesRoot   string
}

func NewProfileService(
	store domain.Store,
	browser domain.BrowserManager,
	coord domain.SessionCoordinator,
	bus domain.EventBus,
	profilesRoot string,
) *ProfileService {
	if strings.TrimSpace(profilesRoot) == "" {
		home, _ := os.UserHomeDir()
		profilesRoot = filepath.Join(home, ".golab", "profiles")
	}
	return &ProfileService{
		store:        store,
		browser:      browser,
		coord:        coord,
		bus:          bus,
		profilesRoot: profilesRoot,
	}
}

func (s *ProfileService) publish(evtType domain.EventType, data any) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(domain.WorkspaceEvent{
		Type:      evtType,
		Timestamp: time.Now().UTC(),
		Data:      data,
	})
}

func (s *ProfileService) EnsureDefaultProfile(ctx context.Context) (*domain.Profile, error) {
	if existing, err := s.store.GetProfile(ctx, "default"); err == nil && existing != nil {
		if s.browser != nil {
			existing.IsRunning = s.browser.IsProfileRunning(ctx, existing.CDPPort)
		}
		return existing, nil
	}
	now := time.Now().UTC()
	def := &domain.Profile{
		ID:          "default",
		Name:        "Default Account",
		Tier:        domain.TierFree,
		UserDataDir: filepath.Join(s.profilesRoot, "default"),
		CDPPort:     9222,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.store.CreateProfile(ctx, def); err != nil {
		return nil, err
	}
	return def, nil
}

func (s *ProfileService) CreateProfile(ctx context.Context, name, accountEmail string, tier domain.ProfileTier) (*domain.Profile, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, domain.NewAppError(domain.CodeInvalidInput, "profile name cannot be empty", "", http.StatusBadRequest)
	}
	if tier == "" {
		tier = domain.TierFree
	}

	profID := uuid.New().String()
	port, err := s.store.GetNextAvailableCDPPort(ctx, 9222)
	if err != nil {
		port = 9222
	}

	now := time.Now().UTC()
	prof := &domain.Profile{
		ID:           profID,
		Name:         name,
		AccountEmail: strings.TrimSpace(accountEmail),
		Tier:         tier,
		EligibleGPUs: []string{},
		EligibleTPUs: []string{},
		UserDataDir:  filepath.Join(s.profilesRoot, profID),
		CDPPort:      port,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := s.store.CreateProfile(ctx, prof); err != nil {
		return nil, err
	}
	s.publish(domain.EventProfileUpdated, prof)
	return prof, nil
}

func (s *ProfileService) GetProfile(ctx context.Context, id string) (*domain.Profile, error) {
	p, err := s.store.GetProfile(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.browser != nil {
		p.IsRunning = s.browser.IsProfileRunning(ctx, p.CDPPort)
	}
	return p, nil
}

func (s *ProfileService) ListProfiles(ctx context.Context) ([]*domain.Profile, error) {
	list, err := s.store.ListProfiles(ctx)
	if err != nil {
		return nil, err
	}
	if s.browser != nil {
		for _, p := range list {
			p.IsRunning = s.browser.IsProfileRunning(ctx, p.CDPPort)
		}
	}
	return list, nil
}

type ProfilePatch struct {
	Name         *string             `json:"name,omitempty"`
	AccountEmail *string             `json:"account_email,omitempty"`
	Tier         *domain.ProfileTier `json:"tier,omitempty"`
}

func (s *ProfileService) UpdateProfile(ctx context.Context, id string, patch ProfilePatch) (*domain.Profile, error) {
	p, err := s.store.GetProfile(ctx, id)
	if err != nil {
		return nil, err
	}
	if patch.Name != nil {
		trimmed := strings.TrimSpace(*patch.Name)
		if trimmed == "" {
			return nil, domain.NewAppError(domain.CodeInvalidInput, "profile name cannot be empty", "", http.StatusBadRequest)
		}
		p.Name = trimmed
	}
	if patch.AccountEmail != nil {
		p.AccountEmail = strings.TrimSpace(*patch.AccountEmail)
	}
	if patch.Tier != nil && *patch.Tier != "" {
		p.Tier = *patch.Tier
	}
	p.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateProfile(ctx, p); err != nil {
		return nil, err
	}
	if s.browser != nil {
		p.IsRunning = s.browser.IsProfileRunning(ctx, p.CDPPort)
	}
	s.publish(domain.EventProfileUpdated, p)
	return p, nil
}

func (s *ProfileService) LaunchProfile(ctx context.Context, id string, initialURL string) (*domain.Profile, error) {
	p, err := s.store.GetProfile(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.browser == nil {
		return nil, domain.NewAppError(domain.CodeInternalError, "browser manager unavailable", "", http.StatusInternalServerError)
	}
	if initialURL == "" {
		initialURL = "https://colab.research.google.com/"
	}
	pid, actualPort, err := s.browser.LaunchProfile(ctx, *p, initialURL)
	if err != nil {
		return nil, err
	}
	_ = s.store.SetProfileChromePID(ctx, p.ID, pid, actualPort)
	updated, err := s.GetProfile(ctx, id)
	if err != nil {
		return nil, err
	}
	updated.IsRunning = true
	s.publish(domain.EventProfileUpdated, updated)
	return updated, nil
}

func (s *ProfileService) StopProfile(ctx context.Context, id string) (*domain.Profile, error) {
	p, err := s.store.GetProfile(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.browser != nil {
		if err := s.browser.StopProfile(ctx, *p); err != nil {
			return nil, err
		}
	}
	_ = s.store.SetProfileChromePID(ctx, p.ID, 0, p.CDPPort)
	// Mark sessions belonging to this profile as browser_stopped
	if sessions, errS := s.store.ListSessions(ctx, ""); errS == nil {
		for _, sess := range sessions {
			if sess.ProfileID == id {
				if s.coord != nil {
					_ = s.coord.DetachSession(ctx, sess.ID, false)
				}
				_ = s.store.UpdateSessionState(ctx, sess.ID, sess.TabID, sess.Permission, domain.StateBrowserStopped)
				if refreshed, errG := s.store.GetSession(ctx, sess.ID); errG == nil {
					s.publish(domain.EventSessionUpdated, refreshed)
				}
			}
		}
	}
	updated, err := s.GetProfile(ctx, id)
	if err != nil {
		return nil, err
	}
	updated.IsRunning = false
	s.publish(domain.EventProfileUpdated, updated)
	return updated, nil
}

func (s *ProfileService) DeleteProfile(ctx context.Context, id string) error {
	p, err := s.store.GetProfile(ctx, id)
	if err != nil {
		return err
	}
	// First check store delete eligibility (blocks 'default' and locked profiles BEFORE killing Chrome!)
	if id == "default" {
		return domain.NewAppError(domain.CodeConflict, "cannot delete the default account profile", "", http.StatusConflict)
	}
	locked, _ := s.store.ListLockedSessions(ctx)
	for _, ls := range locked {
		if ls.ProfileID == id {
			return domain.NewAppError(
				domain.CodeConflict,
				"cannot delete an account profile that currently holds an active Project Lock",
				"Unlock or switch the Project Lock first.",
				http.StatusConflict,
			)
		}
	}
	if s.browser != nil {
		_ = s.browser.StopProfile(ctx, *p)
	}
	if err := s.store.DeleteProfile(ctx, id); err != nil {
		return err
	}
	s.publish(domain.EventProfileDeleted, map[string]string{"id": id})
	return nil
}

func (s *ProfileService) GetQuota(ctx context.Context, id string) (*domain.AccountQuota, error) {
	if s.coord == nil {
		return nil, domain.NewAppError(domain.CodeInternalError, "session coordinator unavailable", "", http.StatusInternalServerError)
	}
	return s.coord.RefreshAccountQuota(ctx, id)
}

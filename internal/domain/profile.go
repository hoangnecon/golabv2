package domain

import (
	"context"
	"time"
)

// ProfileTier represents Google Colab subscription tiers.
type ProfileTier string

const (
	TierFree    ProfileTier = "free"
	TierPro     ProfileTier = "pro"
	TierProPlus ProfileTier = "pro_plus"
)

// Profile represents an isolated Google Account / Chrome user profile.
type Profile struct {
	ID              string      `json:"id"`
	Name            string      `json:"name"`
	AccountEmail    string      `json:"account_email"`
	DisplayName     string      `json:"display_name"`
	Tier            ProfileTier `json:"tier"`
	CCUBalance      float64     `json:"ccu_balance"`
	CCUBurnRate     float64     `json:"ccu_burn_rate"`
	EligibleGPUs    []string    `json:"eligible_gpus"`
	EligibleTPUs    []string    `json:"eligible_tpus"`
	LastTelemetryAt *time.Time  `json:"last_telemetry_at,omitempty"`
	UserDataDir     string      `json:"user_data_dir"`
	CDPPort         int         `json:"cdp_port"`
	ChromePID       int         `json:"chrome_pid"`
	IsRunning       bool        `json:"is_running"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

// ComputeUnitsInfo holds Colab paid resource quota and hardware eligibility.
type ComputeUnitsInfo struct {
	AssignmentsCount      int         `json:"assignments_count"`
	Balance               float64     `json:"balance"`
	ConsumptionRateHourly float64     `json:"consumption_rate_hourly"`
	FormattedBalance      string      `json:"formatted_balance"`
	Tier                  ProfileTier `json:"tier"`
	EligibleGPUs          []string    `json:"eligible_gpus"`
	IneligibleGPUs        []string    `json:"ineligible_gpus"`
	EligibleTPUs          []string    `json:"eligible_tpus"`
	IneligibleTPUs        []string    `json:"ineligible_tpus"`
}

// ColabVMSession represents an active Colab runtime VM on a Google Account.
type ColabVMSession struct {
	FileID           string `json:"file_id"`
	Title            string `json:"title"`
	KernelID         string `json:"kernel_id"`
	SessionID        string `json:"session_id"`
	Accelerator      string `json:"accelerator"`
	AcceleratorModel string `json:"accelerator_model"`
	Shape            string `json:"shape"`
	ManagedID        string `json:"managed_id"`
	ProxyURL         string `json:"proxy_url,omitempty"`
	TokenExpiresSec  int    `json:"token_expires_sec,omitempty"`
}

// AccountQuota aggregates live account identity, compute units, and active Colab VMs.
type AccountQuota struct {
	ProfileID       string            `json:"profile_id"`
	AccountEmail    string            `json:"account_email"`
	DisplayName     string            `json:"display_name"`
	IsRunningChrome bool              `json:"is_running_chrome"`
	ComputeUnits    *ComputeUnitsInfo `json:"compute_units,omitempty"`
	ActiveVMs       []ColabVMSession  `json:"active_vms"`
	ObservedAt      time.Time         `json:"observed_at"`
}

// ProfileRepository defines persistence operations for Profiles (kept as subset of Store).
type ProfileRepository interface {
	CreateProfile(ctx context.Context, p *Profile) error
	GetProfile(ctx context.Context, id string) (*Profile, error)
	ListProfiles(ctx context.Context) ([]*Profile, error)
	UpdateProfile(ctx context.Context, p *Profile) error
	DeleteProfile(ctx context.Context, id string) error
	GetNextAvailableCDPPort(ctx context.Context, startPort int) (int, error)
}

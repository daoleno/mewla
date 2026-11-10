// Package doctor inspects whether a machine can run Mewla without performing
// destructive or paid actions. The Report type is the stable contract later
// consumed by `mewla setup` and daemon/app control APIs.
package doctor

import (
	"errors"
	"time"

	"github.com/daoleno/mewla/daemon/work"
)

// ErrNotReady is returned by the CLI after a successful diagnosis when Ready
// is false. Callers should treat the printed Report as authoritative.
var ErrNotReady = errors.New("mewla doctor: environment is not ready")

// Status is a machine-readable check outcome.
type Status string

const (
	StatusOK      Status = "ok"
	StatusWarn    Status = "warn"
	StatusFail    Status = "fail"
	StatusUnknown Status = "unknown"
)

// AuthState is best-effort executor authentication state.
// Unknown means no safe official probe exists or the probe failed ambiguously.
type AuthState string

const (
	AuthAuthenticated   AuthState = "authenticated"
	AuthUnauthenticated AuthState = "unauthenticated"
	AuthUnknown         AuthState = "unknown"
)

// Remediation codes are stable identifiers for automated onboarding flows.
type Remediation string

const (
	RemediationUnsupportedPlatform Remediation = "unsupported_platform"
	RemediationStateDirUnwritable  Remediation = "state_dir_unwritable"
	RemediationPortInUse           Remediation = "port_in_use"
	RemediationInstallExecutor     Remediation = "install_executor"
	RemediationAuthenticateExec    Remediation = "authenticate_executor"
	RemediationConfigureExecutor   Remediation = "configure_executor"
)

// RecommendationConfidence describes how strongly doctor trusts a recommendation.
type RecommendationConfidence string

const (
	// ConfidenceVerified means at least one recommended executor has verified auth.
	ConfidenceVerified RecommendationConfidence = "verified"
	// ConfidenceUnverified means recommendations rely on auth-unknown runnable candidates.
	ConfidenceUnverified RecommendationConfidence = "unverified"
	// ConfidenceNone means no runnable executor was available to recommend.
	ConfidenceNone RecommendationConfidence = "none"
)

// Report is the top-level doctor result. JSON field names are part of the
// onboarding API surface and should remain stable.
type Report struct {
	Ready        bool           `json:"ready"`
	GeneratedAt  time.Time      `json:"generated_at"`
	Platform     PlatformCheck  `json:"platform"`
	StateDir     StateDirCheck  `json:"state_dir"`
	Listen       ListenCheck    `json:"listen"`
	Executors    ExecutorsCheck `json:"executors"`
	Addresses    []AddressCheck `json:"addresses,omitempty"`
	Remote       RemoteCheck    `json:"remote"`
	Network      NetworkCheck   `json:"network"`
	Checks       []NamedCheck   `json:"checks"`
	Warnings     []string       `json:"warnings,omitempty"`
	Remediations []Remediation  `json:"remediations"`
}

// RemoteCheck reports hosted remote access: sign-in, relay connection and
// the hostname certificate, as last written by the running daemon.
type RemoteCheck struct {
	SignedIn     bool      `json:"signed_in"`
	URL          string    `json:"url,omitempty"`
	Account      string    `json:"account,omitempty"`
	Relay        string    `json:"relay,omitempty"`
	Phase        string    `json:"phase,omitempty"`
	CertNotAfter time.Time `json:"cert_not_after,omitempty"`
	Status       Status    `json:"status"`
	Summary      string    `json:"summary"`
}

type AddressCheck struct {
	URL       string `json:"url"`
	Reachable bool   `json:"reachable"`
	TLS       bool   `json:"tls"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
	Status    Status `json:"status"`
	Summary   string `json:"summary"`
}

type NetworkCheck struct {
	TailscaleFound bool   `json:"tailscale_found"`
	Direct         bool   `json:"direct"`
	Relay          string `json:"relay,omitempty"`
	NATType        string `json:"nat_type,omitempty"`
	ProxySignature string `json:"proxy_signature,omitempty"`
	Cloudflare     string `json:"cloudflare,omitempty"`
	Status         Status `json:"status"`
	Summary        string `json:"summary"`
}

// NamedCheck is a flat summary entry for UI/API consumers.
type NamedCheck struct {
	ID          string      `json:"id"`
	Status      Status      `json:"status"`
	Remediation Remediation `json:"remediation,omitempty"`
	Summary     string      `json:"summary"`
}

// PlatformCheck reports OS/arch support.
type PlatformCheck struct {
	OS          string      `json:"os"`
	Arch        string      `json:"arch"`
	Supported   bool        `json:"supported"`
	Status      Status      `json:"status"`
	Remediation Remediation `json:"remediation,omitempty"`
	Summary     string      `json:"summary"`
}

// StateDirCheck reports Mewla state directory readiness.
type StateDirCheck struct {
	Path        string      `json:"path"`
	Writable    bool        `json:"writable"`
	Created     bool        `json:"created"`
	Status      Status      `json:"status"`
	Remediation Remediation `json:"remediation,omitempty"`
	Summary     string      `json:"summary"`
}

// ListenCheck reports daemon listen readiness.
type ListenCheck struct {
	Addr          string      `json:"addr"`
	Available     bool        `json:"available"`
	DaemonRunning bool        `json:"daemon_running"`
	DaemonID      string      `json:"daemon_id,omitempty"`
	Status        Status      `json:"status"`
	Remediation   Remediation `json:"remediation,omitempty"`
	Summary       string      `json:"summary"`
}

// ExecutorsCheck summarizes configured executors and recommendations.
type ExecutorsCheck struct {
	ConfigPath               string                   `json:"config_path"`
	ConfigExists             bool                     `json:"config_exists"`
	Status                   Status                   `json:"status"`
	Remediation              Remediation              `json:"remediation,omitempty"`
	Summary                  string                   `json:"summary"`
	Items                    []ExecutorCheck          `json:"items"`
	UsableCount              int                      `json:"usable_count"`
	VerifiedCount            int                      `json:"verified_count"`
	RecommendedHost          string                   `json:"recommended_host,omitempty"`
	RecommendationConfidence RecommendationConfidence `json:"recommendation_confidence"`
	Warnings                 []string                 `json:"warnings,omitempty"`
}

// ExecutorCheck is one configured executor's capability and readiness view.
//
// Runnable/Usable means the binary is present and not explicitly
// unauthenticated. VerifiedAuthenticated is stricter: an official probe
// confirmed login. Auth-unknown candidates are usable with warning.
type ExecutorCheck struct {
	ID                    string                  `json:"id"`
	Name                  string                  `json:"name"`
	Provider              string                  `json:"provider"`
	Configured            bool                    `json:"configured"`
	Command               string                  `json:"command,omitempty"`
	BinaryFound           bool                    `json:"binary_found"`
	BinaryPath            string                  `json:"binary_path,omitempty"`
	Version               string                  `json:"version,omitempty"`
	Auth                  AuthState               `json:"auth"`
	Runnable              bool                    `json:"runnable"`
	Usable                bool                    `json:"usable"`
	VerifiedAuthenticated bool                    `json:"verified_authenticated"`
	Capabilities          work.WorkerCapabilities `json:"capabilities"`
	Status                Status                  `json:"status"`
	Remediation           Remediation             `json:"remediation,omitempty"`
	Summary               string                  `json:"summary"`
	// ConfigDir is set for a separate Claude Code or Codex sign-in.
	ConfigDir string `json:"config_dir,omitempty"`
	// Account is who the CLI is signed in as (an email address), when the CLI
	// records it; empty when unknown. Never a token.
	Account string `json:"account,omitempty"`
	// OpenCode-only non-mutating probes. StatusUnknown means the probe was not
	// run or failed ambiguously; never includes model lists or secrets.
	ModelsStatus  Status `json:"models_status,omitempty"`
	ServiceStatus Status `json:"service_status,omitempty"`
}

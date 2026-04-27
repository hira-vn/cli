package daemon

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultServerURL             = "ws://localhost:8080/ws"
	DefaultPollInterval          = 3 * time.Second
	DefaultHeartbeatInterval     = 15 * time.Second
	DefaultAgentTimeout          = 2 * time.Hour
	DefaultRuntimeName           = "Local Agent"
	DefaultWorkspaceSyncInterval = 30 * time.Second
	DefaultHealthPort            = 19514
	DefaultMaxConcurrentTasks    = 20
	DefaultGCInterval            = 1 * time.Hour
	DefaultGCTTL                 = 5 * 24 * time.Hour // 5 days
	DefaultGCOrphanTTL           = 30 * 24 * time.Hour // 30 days
)

// Config holds all daemon configuration.
type Config struct {
	ServerBaseURL      string
	DaemonID           string
	LegacyDaemonIDs    []string              // historical daemon_ids this machine may have registered under; reported at register time so the server can merge old runtime rows
	DeviceName         string
	RuntimeName        string
	CLIVersion         string                // hira CLI version (e.g. "0.1.13")
	LaunchedBy         string                // "desktop" when spawned by the Electron app, empty for standalone
	Profile            string                // profile name (empty = default)
	Agents             map[string]AgentEntry // keyed by provider: claude, codex, opencode, openclaw, hermes, gemini, pi
	WorkspacesRoot     string                // base path for execution envs (default: ~/hira_workspaces)
	KeepEnvAfterTask   bool                  // preserve env after task for debugging
	HealthPort         int                   // local HTTP port for health checks (default: 19514)
	MaxConcurrentTasks int                   // max tasks running in parallel (default: 20)
	GCEnabled          bool                  // enable periodic workspace garbage collection (default: true)
	GCInterval         time.Duration         // how often the GC loop runs (default: 1h)
	GCTTL              time.Duration         // clean dirs whose issue is done/canceled and updated_at < now()-TTL (default: 5d)
	GCOrphanTTL        time.Duration         // clean orphan dirs (no meta or unknown issue) older than this (default: 30d)
	PollInterval       time.Duration
	HeartbeatInterval  time.Duration
	AgentTimeout       time.Duration
}

// Overrides allows CLI flags to override environment variables and defaults.
// Zero values are ignored and the env/default value is used instead.
type Overrides struct {
	ServerURL          string
	WorkspacesRoot     string
	PollInterval       time.Duration
	HeartbeatInterval  time.Duration
	AgentTimeout       time.Duration
	MaxConcurrentTasks int
	DaemonID           string
	DeviceName         string
	RuntimeName        string
	Profile            string // profile name (empty = default)
	HealthPort         int    // health check port (0 = use default)
}

// LoadConfig builds the daemon configuration from environment variables
// and optional CLI flag overrides.
func LoadConfig(overrides Overrides) (Config, error) {
	// Server URL: override > env > default
	rawServerURL := envOrDefault("HIRA_SERVER_URL", DefaultServerURL)
	if overrides.ServerURL != "" {
		rawServerURL = overrides.ServerURL
	}
	serverBaseURL, err := NormalizeServerBaseURL(rawServerURL)
	if err != nil {
		return Config{}, err
	}

	// Augment $PATH with common user-local install dirs before probing.
	// Daemons launched via launchctl, nohup, or service managers often
	// inherit a stripped PATH that misses ~/.local/bin or /opt/homebrew/bin —
	// where most agent CLIs (claude, codex, copilot, …) actually live. The
	// augment is idempotent and applies to both LookPath calls below and
	// every later exec.Command spawned by the daemon (since cmd.Env starts
	// from os.Environ).
	ensureAgentDiscoveryPATH()

	// Probe available agent CLIs.
	//
	// We resolve each requested path to its absolute location via LookPath
	// and store the absolute result. Two reasons:
	//   1. If a sibling tool later modifies PATH (e.g. an external auto-
	//      updater removes the version-pinned dir we found "claude" in),
	//      our exec.Command call fails with "no such file: <abs path>"
	//      instead of a misleading "not found in $PATH". Much easier to
	//      debug.
	//   2. The daemon's heartbeat / pre-spawn check can Stat() the absolute
	//      path to detect external moves and surface a clear "restart
	//      daemon" hint before the agent crashes.
	agents := map[string]AgentEntry{}
	type probe struct {
		name        string
		envPath     string
		envModel    string
		defaultName string
	}
	for _, p := range []probe{
		{"claude", "HIRA_CLAUDE_PATH", "HIRA_CLAUDE_MODEL", "claude"},
		{"codex", "HIRA_CODEX_PATH", "HIRA_CODEX_MODEL", "codex"},
		{"opencode", "HIRA_OPENCODE_PATH", "HIRA_OPENCODE_MODEL", "opencode"},
		{"openclaw", "HIRA_OPENCLAW_PATH", "HIRA_OPENCLAW_MODEL", "openclaw"},
		{"hermes", "HIRA_HERMES_PATH", "HIRA_HERMES_MODEL", "hermes"},
		{"gemini", "HIRA_GEMINI_PATH", "HIRA_GEMINI_MODEL", "gemini"},
		{"pi", "HIRA_PI_PATH", "HIRA_PI_MODEL", "pi"},
		{"cursor", "HIRA_CURSOR_PATH", "HIRA_CURSOR_MODEL", "cursor-agent"},
		{"copilot", "HIRA_COPILOT_PATH", "HIRA_COPILOT_MODEL", "copilot"},
	} {
		raw := envOrDefault(p.envPath, p.defaultName)
		abs, err := exec.LookPath(raw)
		if err != nil {
			continue
		}
		agents[p.name] = AgentEntry{
			Path:  abs,
			Model: strings.TrimSpace(os.Getenv(p.envModel)),
		}
	}
	if len(agents) == 0 {
		return Config{}, fmt.Errorf("no agent CLI found: install claude, codex, copilot, opencode, openclaw, hermes, gemini, pi, or cursor-agent and ensure it is on PATH")
	}

	// Host info
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = "local-machine"
	}

	// Durations: override > env > default
	pollInterval, err := durationFromEnv("HIRA_DAEMON_POLL_INTERVAL", DefaultPollInterval)
	if err != nil {
		return Config{}, err
	}
	if overrides.PollInterval > 0 {
		pollInterval = overrides.PollInterval
	}

	heartbeatInterval, err := durationFromEnv("HIRA_DAEMON_HEARTBEAT_INTERVAL", DefaultHeartbeatInterval)
	if err != nil {
		return Config{}, err
	}
	if overrides.HeartbeatInterval > 0 {
		heartbeatInterval = overrides.HeartbeatInterval
	}

	agentTimeout, err := durationFromEnv("HIRA_AGENT_TIMEOUT", DefaultAgentTimeout)
	if err != nil {
		return Config{}, err
	}
	if overrides.AgentTimeout > 0 {
		agentTimeout = overrides.AgentTimeout
	}

	maxConcurrentTasks, err := intFromEnv("HIRA_DAEMON_MAX_CONCURRENT_TASKS", DefaultMaxConcurrentTasks)
	if err != nil {
		return Config{}, err
	}
	if overrides.MaxConcurrentTasks > 0 {
		maxConcurrentTasks = overrides.MaxConcurrentTasks
	}

	// Profile
	profile := overrides.Profile

	// daemon_id resolution: override > env > persistent UUID on disk.
	// The persistent UUID is written once to `<profile-dir>/daemon.id` and
	// then reused forever so hostname drift (.local suffix, system rename,
	// mDNS state, profile switch) no longer mints a new runtime identity.
	// Callers may still pin a specific id via HIRA_DAEMON_ID or the
	// override field (e.g. for tests or embedded environments).
	daemonID := strings.TrimSpace(os.Getenv("HIRA_DAEMON_ID"))
	if overrides.DaemonID != "" {
		daemonID = overrides.DaemonID
	}
	if daemonID == "" {
		persisted, err := EnsureDaemonID(profile)
		if err != nil {
			return Config{}, fmt.Errorf("ensure daemon id: %w", err)
		}
		daemonID = persisted
	}
	// Historical daemon_ids derived from the current hostname/profile. The
	// server uses these at register time to merge any pre-UUID runtime rows
	// for this machine into the new UUID-keyed row and delete the stale ones.
	legacyDaemonIDs := LegacyDaemonIDs(host, profile)
	// Pre-change (#1220) daemon identity was stored per profile, which means
	// the same machine could end up with multiple leftover daemon.id files
	// — e.g. ~/.hira/daemon.id (default) plus ~/.hira/profiles/<x>/
	// daemon.id. Surface those UUIDs so the server can merge their runtime
	// rows into the canonical machine UUID. Fatal-free: a broken profiles
	// dir shouldn't block startup.
	if uuids, err := LegacyDaemonUUIDs(); err == nil {
		legacyDaemonIDs = append(legacyDaemonIDs, uuids...)
	}
	// Strip anything that collides with the resolved daemon_id (e.g. when
	// the user explicitly pins HIRA_DAEMON_ID=<hostname>, or when the
	// canonical id was itself promoted from a pre-change profile file).
	legacyDaemonIDs = filterLegacyIDs(legacyDaemonIDs, daemonID)

	deviceName := envOrDefault("HIRA_DAEMON_DEVICE_NAME", host)
	if overrides.DeviceName != "" {
		deviceName = overrides.DeviceName
	}

	runtimeName := envOrDefault("HIRA_AGENT_RUNTIME_NAME", DefaultRuntimeName)
	if overrides.RuntimeName != "" {
		runtimeName = overrides.RuntimeName
	}

	// Workspaces root: override > env > default (~/hira_workspaces or ~/hira_workspaces_<profile>)
	workspacesRoot := strings.TrimSpace(os.Getenv("HIRA_WORKSPACES_ROOT"))
	if overrides.WorkspacesRoot != "" {
		workspacesRoot = overrides.WorkspacesRoot
	}
	if workspacesRoot == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Config{}, fmt.Errorf("resolve home directory: %w (set HIRA_WORKSPACES_ROOT to override)", err)
		}
		if profile != "" {
			workspacesRoot = filepath.Join(home, "hira_workspaces_"+profile)
		} else {
			workspacesRoot = filepath.Join(home, "hira_workspaces")
		}
	}
	workspacesRoot, err = filepath.Abs(workspacesRoot)
	if err != nil {
		return Config{}, fmt.Errorf("resolve absolute workspaces root: %w", err)
	}

	// Health port: override > default
	healthPort := DefaultHealthPort
	if overrides.HealthPort > 0 {
		healthPort = overrides.HealthPort
	}

	// Keep env after task: env > default (false)
	keepEnv := os.Getenv("HIRA_KEEP_ENV_AFTER_TASK") == "true" || os.Getenv("HIRA_KEEP_ENV_AFTER_TASK") == "1"

	// GC config: env > defaults
	gcEnabled := true
	if v := os.Getenv("HIRA_GC_ENABLED"); v == "false" || v == "0" {
		gcEnabled = false
	}
	gcInterval, err := durationFromEnv("HIRA_GC_INTERVAL", DefaultGCInterval)
	if err != nil {
		return Config{}, err
	}
	gcTTL, err := durationFromEnv("HIRA_GC_TTL", DefaultGCTTL)
	if err != nil {
		return Config{}, err
	}
	gcOrphanTTL, err := durationFromEnv("HIRA_GC_ORPHAN_TTL", DefaultGCOrphanTTL)
	if err != nil {
		return Config{}, err
	}

	return Config{
		ServerBaseURL:      serverBaseURL,
		DaemonID:           daemonID,
		LegacyDaemonIDs:    legacyDaemonIDs,
		DeviceName:         deviceName,
		RuntimeName:        runtimeName,
		Profile:            profile,
		Agents:             agents,
		WorkspacesRoot:     workspacesRoot,
		KeepEnvAfterTask:   keepEnv,
		GCEnabled:          gcEnabled,
		GCInterval:         gcInterval,
		GCTTL:              gcTTL,
		GCOrphanTTL:        gcOrphanTTL,
		HealthPort:         healthPort,
		MaxConcurrentTasks: maxConcurrentTasks,
		PollInterval:       pollInterval,
		HeartbeatInterval:  heartbeatInterval,
		AgentTimeout:       agentTimeout,
	}, nil
}

// ensureAgentDiscoveryPATH prepends common user-local install dirs to $PATH
// so the daemon can discover agent CLIs even when launched from a context
// with a stripped environment (launchctl, nohup, system service managers).
//
// We only add directories that actually exist; missing ones are skipped so
// LookPath does not waste a syscall per call. Existing PATH entries are
// preserved verbatim. Already-present dirs are de-duplicated so calling
// the helper twice is a no-op.
func ensureAgentDiscoveryPATH() {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	candidates := []string{
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, "bin"),
		"/opt/homebrew/bin",
		"/opt/homebrew/sbin",
		"/usr/local/bin",
		"/usr/local/sbin",
	}
	current := os.Getenv("PATH")
	parts := strings.Split(current, string(os.PathListSeparator))
	have := make(map[string]bool, len(parts))
	for _, p := range parts {
		have[p] = true
	}
	var prepend []string
	for _, c := range candidates {
		if c == "" || have[c] {
			continue
		}
		if info, err := os.Stat(c); err != nil || !info.IsDir() {
			continue
		}
		prepend = append(prepend, c)
		have[c] = true
	}
	if len(prepend) == 0 {
		return
	}
	combined := strings.Join(append(prepend, parts...), string(os.PathListSeparator))
	_ = os.Setenv("PATH", combined)
}

// NormalizeServerBaseURL converts a WebSocket or HTTP URL to a base HTTP URL.
func NormalizeServerBaseURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("invalid HIRA_SERVER_URL: %w", err)
	}
	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	case "http", "https":
	default:
		return "", fmt.Errorf("HIRA_SERVER_URL must use ws, wss, http, or https")
	}
	if u.Path == "/ws" {
		u.Path = ""
	}
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), nil
}

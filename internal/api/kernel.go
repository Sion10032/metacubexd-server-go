package api

type KernelStatus string

const (
	StatusStopped  KernelStatus = "stopped"
	StatusStarting KernelStatus = "starting"
	StatusRunning  KernelStatus = "running"
	StatusStopping KernelStatus = "stopping"
	StatusErrored  KernelStatus = "errored"
)

// KernelState is the JSON shape served by GET /api/control/kernel/status.
type KernelState struct {
	Status             KernelStatus `json:"status"`
	PID                *int         `json:"pid,omitempty"`
	StartedAt          *int64       `json:"startedAt,omitempty"`
	Version            string       `json:"version,omitempty"`
	ExternalController string       `json:"externalController"`
	Secret             string       `json:"secret"`
	LastExitCode       *int         `json:"lastExitCode,omitempty"`
	LastError          string       `json:"lastError,omitempty"`
}

// KernelLogLine is one line emitted by mihomo on stdout/stderr.
type KernelLogLine struct {
	Stream string `json:"stream"` // "stdout" | "stderr"
	Line   string `json:"line"`
	TS     int64  `json:"ts"`
}

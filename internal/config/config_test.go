package config

import (
	"testing"
)

// TestDefaults verifies all default values when env is empty.
func TestDefaults(t *testing.T) {
	t.Setenv("CONTROL_PORT", "")
	t.Setenv("CLASH_API_PORT", "")
	t.Setenv("MIXED_PORT", "")
	t.Setenv("DATA_DIR", "")
	t.Setenv("MIHOMO_BIN", "")
	t.Setenv("CONTROL_TOKEN", "")
	t.Setenv("CLASH_SECRET", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("UI_DIST", "")
	t.Setenv("MIHOMO_VERSION", "")

	env := FromEnv()

	if env.ControlPort != 8080 {
		t.Errorf("ControlPort = %d, want 8080", env.ControlPort)
	}
	if env.ClashAPIPort != 9090 {
		t.Errorf("ClashAPIPort = %d, want 9090", env.ClashAPIPort)
	}
	if env.MixedPort != 7890 {
		t.Errorf("MixedPort = %d, want 7890", env.MixedPort)
	}
	if env.DataDir != "data" {
		t.Errorf("DataDir = %q, want \"data\"", env.DataDir)
	}
	if env.MihomoBin != "/usr/local/bin/mihomo" {
		t.Errorf("MihomoBin = %q, want /usr/local/bin/mihomo", env.MihomoBin)
	}
	if env.ControlToken != "" {
		t.Errorf("ControlToken = %q, want empty", env.ControlToken)
	}
	if env.ClashSecret != "" {
		t.Errorf("ClashSecret = %q, want empty", env.ClashSecret)
	}
	if env.MihomoVersion != "v1.19.27" {
		t.Errorf("MihomoVersion = %q, want v1.19.27", env.MihomoVersion)
	}
}

// TestCustomEnv verifies overrides from environment.
func TestCustomEnv(t *testing.T) {
	t.Setenv("CONTROL_PORT", "8080")
	t.Setenv("CLASH_API_PORT", "9091")
	t.Setenv("MIXED_PORT", "1080")
	t.Setenv("DATA_DIR", "/data")
	t.Setenv("MIHOMO_BIN", "/opt/mihomo")
	t.Setenv("CONTROL_TOKEN", "secret-token")
	t.Setenv("CLASH_SECRET", "my-secret")
	t.Setenv("GITHUB_TOKEN", "ghp_xxx")
	t.Setenv("UI_DIST", "/ui")
	t.Setenv("MIHOMO_VERSION", "v1.20.0")

	env := FromEnv()

	if env.ControlPort != 8080 {
		t.Errorf("ControlPort = %d, want 8080", env.ControlPort)
	}
	if env.ClashAPIPort != 9091 {
		t.Errorf("ClashAPIPort = %d, want 9091", env.ClashAPIPort)
	}
	if env.MixedPort != 1080 {
		t.Errorf("MixedPort = %d, want 1080", env.MixedPort)
	}
	if env.DataDir != "/data" {
		t.Errorf("DataDir = %q, want /data", env.DataDir)
	}
	if env.MihomoBin != "/opt/mihomo" {
		t.Errorf("MihomoBin = %q, want /opt/mihomo", env.MihomoBin)
	}
	if env.ControlToken != "secret-token" {
		t.Errorf("ControlToken = %q, want secret-token", env.ControlToken)
	}
	if env.ClashSecret != "my-secret" {
		t.Errorf("ClashSecret = %q, want my-secret", env.ClashSecret)
	}
	if env.GitHubToken != "ghp_xxx" {
		t.Errorf("GitHubToken = %q, want ghp_xxx", env.GitHubToken)
	}
	if env.UIDist != "/ui" {
		t.Errorf("UIDist = %q, want /ui", env.UIDist)
	}
	if env.MihomoVersion != "v1.20.0" {
		t.Errorf("MihomoVersion = %q, want v1.20.0", env.MihomoVersion)
	}
}

// TestInvalidPortFallsBack verifies non-numeric ports fall back to defaults.
func TestInvalidPortFallsBack(t *testing.T) {
	t.Setenv("CONTROL_PORT", "not-a-number")
	t.Setenv("CLASH_API_PORT", "")
	t.Setenv("MIXED_PORT", "abc")

	env := FromEnv()

	if env.ControlPort != 8080 {
		t.Errorf("ControlPort = %d, want 8080 (fallback on invalid)", env.ControlPort)
	}
	if env.MixedPort != 7890 {
		t.Errorf("MixedPort = %d, want 7890 (fallback on invalid)", env.MixedPort)
	}
}

// TestProfilesDir verifies helper path construction.
func TestProfilesDir(t *testing.T) {
	t.Setenv("DATA_DIR", "/tmp/mydata")
	env := FromEnv()
	got := env.ProfilesDir()
	want := "/tmp/mydata/profiles"
	if got != want {
		t.Errorf("ProfilesDir() = %q, want %q", got, want)
	}
}

// TestActiveConfigPath verifies helper path construction.
func TestActiveConfigPath(t *testing.T) {
	t.Setenv("DATA_DIR", "/tmp/mydata")
	env := FromEnv()
	got := env.ActiveConfigPath()
	want := "/tmp/mydata/active.yaml"
	if got != want {
		t.Errorf("ActiveConfigPath() = %q, want %q", got, want)
	}
}

// TestExternalController verifies bind spec format.
func TestExternalController(t *testing.T) {
	t.Setenv("CLASH_API_PORT", "9091")
	env := FromEnv()
	got := env.ExternalController()
	want := "0.0.0.0:9091"
	if got != want {
		t.Errorf("ExternalController() = %q, want %q", got, want)
	}
}

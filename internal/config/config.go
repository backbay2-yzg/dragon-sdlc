package config

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	AppName        = "Dragon"
	AppIdentifier  = "com.dragon.app"
	AppVersion     = "0.1.0"
	DefaultDBName  = "dragon.db"
)

// Paths holds all filesystem directories and files managed by Dragon.
type Paths struct {
	RootDir      string `json:"rootDir"`
	DBPath       string `json:"dbPath"`
	ReposDir     string `json:"reposDir"`
	WorktreesDir string `json:"worktreesDir"`
	ArtifactsDir string `json:"artifactsDir"`
}

// ResolvePaths computes Dragon's storage directories. If customRoot is empty,
// it uses %LOCALAPPDATA%\Dragon on Windows or os.UserConfigDir()/Dragon elsewhere.
func ResolvePaths(customRoot string) (Paths, error) {
	root := customRoot
	if root == "" {
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			root = filepath.Join(localAppData, AppName)
		} else {
			cfgDir, err := os.UserConfigDir()
			if err != nil {
				return Paths{}, err
			}
			root = filepath.Join(cfgDir, AppName)
		}
	}

	return Paths{
		RootDir:      root,
		DBPath:       filepath.Join(root, DefaultDBName),
		ReposDir:     filepath.Join(root, "repos"),
		WorktreesDir: filepath.Join(root, "worktrees"),
		ArtifactsDir: filepath.Join(root, "artifacts"),
	}, nil
}

// EnsureDirs creates all managed directories if they do not exist.
func (p Paths) EnsureDirs() error {
	for _, dir := range []string{p.RootDir, p.ReposDir, p.WorktreesDir, p.ArtifactsDir} {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// AppSettings stores persistent application-level preferences.
type AppSettings struct {
	ThemeMode                string `json:"themeMode"`                // system | light | dark
	BackdropMaterial         string `json:"backdropMaterial"`         // mica | mica_alt | acrylic
	MaxConcurrency           int    `json:"maxConcurrency"`           // 1..6 (default 2)
	MinimizeToTray           bool   `json:"minimizeToTray"`           // default true
	EnableToastNotifications bool   `json:"enableToastNotifications"` // default true
	DefaultAutonomyMode      string `json:"defaultAutonomyMode"`      // template | semi_auto | full_auto | step_by_step
	DefaultWorkspaceRoot     string `json:"defaultWorkspaceRoot"`     // optional parent directory for cloned repo workspaces
	AuthMethod               string `json:"authMethod"`               // gh_cli | pat | oauth_device | none
	OAuthClientID            string `json:"oauthClientId"`            // remembered GitHub OAuth App Client ID
	LLMProvider              string `json:"llmProvider"`              // none | openai_compatible | anthropic
	LLMBaseURL               string `json:"llmBaseUrl"`
	LLMModel                 string `json:"llmModel"`
}

// DefaultSettings returns Dragon's recommended out-of-the-box configuration.
func DefaultSettings() AppSettings {
	return AppSettings{
		ThemeMode:                "system",
		BackdropMaterial:         "mica",
		MaxConcurrency:           2,
		MinimizeToTray:           true,
		EnableToastNotifications: true,
		DefaultAutonomyMode:      "template",
		AuthMethod:               "gh_cli",
		LLMProvider:              "none",
		LLMBaseURL:               "https://api.openai.com/v1",
		LLMModel:                 "gpt-4o-mini",
	}
}

// Normalize clamps and validates AppSettings values.
func (s AppSettings) Normalize() AppSettings {
	def := DefaultSettings()
	if s.ThemeMode != "system" && s.ThemeMode != "light" && s.ThemeMode != "dark" {
		s.ThemeMode = def.ThemeMode
	}
	if s.BackdropMaterial != "mica" && s.BackdropMaterial != "mica_alt" && s.BackdropMaterial != "acrylic" {
		s.BackdropMaterial = def.BackdropMaterial
	}
	if s.MaxConcurrency < 1 {
		s.MaxConcurrency = 1
	} else if s.MaxConcurrency > 6 {
		s.MaxConcurrency = 6
	}
	if s.DefaultAutonomyMode != "template" && s.DefaultAutonomyMode != "semi_auto" && s.DefaultAutonomyMode != "full_auto" && s.DefaultAutonomyMode != "step_by_step" {
		s.DefaultAutonomyMode = def.DefaultAutonomyMode
	}
	if s.AuthMethod != "gh_cli" && s.AuthMethod != "pat" && s.AuthMethod != "oauth_device" && s.AuthMethod != "none" {
		s.AuthMethod = def.AuthMethod
	}
	if s.LLMProvider != "none" && s.LLMProvider != "openai_compatible" && s.LLMProvider != "anthropic" {
		s.LLMProvider = def.LLMProvider
	}
	s.DefaultWorkspaceRoot = strings.TrimSpace(s.DefaultWorkspaceRoot)
	if s.DefaultWorkspaceRoot != "" {
		s.DefaultWorkspaceRoot = filepath.Clean(s.DefaultWorkspaceRoot)
	}
	return s
}

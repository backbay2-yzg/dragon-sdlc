//go:build !windows

package agent

import (
	"os"
	"os/exec"
	"path/filepath"
)

func discoverCodexDesktop() (ResolvedExec, bool) {
	return ResolvedExec{}, false
}

func discoverDesktopInstallDirs(nameMatch string) []string {
	var dirs []string
	candidates := []string{
		filepath.Join("/Applications", nameMatch+".app", "Contents"),
		filepath.Join("/opt", nameMatch),
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates,
			filepath.Join(home, "Applications", nameMatch+".app", "Contents"),
			filepath.Join(home, ".local", "share", nameMatch),
		)
	}
	for _, d := range candidates {
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

func findNodeExecutable() string {
	if path, err := exec.LookPath("node"); err == nil {
		return path
	}
	for _, c := range []string{"/usr/local/bin/node", "/opt/homebrew/bin/node", "/usr/bin/node"} {
		if isRegularFile(c) {
			return c
		}
	}
	return ""
}

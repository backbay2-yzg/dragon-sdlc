//go:build windows

package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func discoverCodexDesktop() (ResolvedExec, bool) {
	var candidates []string
	seen := make(map[string]bool)
	addCand := func(p string) {
		if p == "" {
			return
		}
		p = filepath.Clean(p)
		key := strings.ToLower(p)
		if !seen[key] {
			seen[key] = true
			candidates = append(candidates, p)
		}
	}

	// 1. Read AppX package registration from Windows Registry.
	// Note: C:\Program Files\WindowsApps denies directory listing to standard users,
	// so filepath.Glob cannot enumerate it, whereas PackageRootFolder from Registry
	// points directly to the readable package directory.
	appModelRoots := []struct {
		root registry.Key
		path string
	}{
		{registry.CURRENT_USER, `Software\Classes\Local Settings\Software\Microsoft\Windows\CurrentVersion\AppModel\Repository\Packages`},
		{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\AppModel\PackageRepository\Packages`},
	}
	for _, hive := range appModelRoots {
		k, err := registry.OpenKey(hive.root, hive.path, registry.READ)
		if err != nil {
			continue
		}
		subNames, err := k.ReadSubKeyNames(-1)
		_ = k.Close()
		if err != nil {
			continue
		}
		for _, sub := range subNames {
			if !strings.HasPrefix(strings.ToLower(sub), "openai.codex_") {
				continue
			}
			sk, err := registry.OpenKey(hive.root, hive.path+`\`+sub, registry.READ)
			if err != nil {
				continue
			}
			pkgRoot, _, _ := sk.GetStringValue("PackageRootFolder")
			_ = sk.Close()
			if pkgRoot != "" {
				addCand(filepath.Join(pkgRoot, "app", "resources", "codex.exe"))
			} else if pf := os.Getenv("ProgramFiles"); pf != "" {
				addCand(filepath.Join(pf, "WindowsApps", sub, "app", "resources", "codex.exe"))
			}
		}
	}

	// 2. Fallback glob if WindowsApps is readable on this host.
	var patterns []string
	if pf := os.Getenv("ProgramFiles"); pf != "" {
		patterns = append(patterns, filepath.Join(pf, "WindowsApps", "OpenAI.Codex_*", "app", "resources", "codex.exe"))
	}
	defaultPF := `C:\Program Files`
	if !strings.EqualFold(os.Getenv("ProgramFiles"), defaultPF) {
		patterns = append(patterns, filepath.Join(defaultPF, "WindowsApps", "OpenAI.Codex_*", "app", "resources", "codex.exe"))
	}
	for _, pat := range patterns {
		if found, err := filepath.Glob(pat); err == nil {
			for _, f := range found {
				addCand(f)
			}
		}
	}

	if len(candidates) == 0 {
		return ResolvedExec{}, false
	}
	// Sort descending so newer versioned package folders take precedence.
	sort.Slice(candidates, func(i, j int) bool {
		return strings.ToLower(candidates[i]) > strings.ToLower(candidates[j])
	})
	for _, m := range candidates {
		if isRegularFile(m) {
			return ResolvedExec{
				Executable:  m,
				DisplayPath: m,
				Source:      "windowsapps",
			}, true
		}
	}
	return ResolvedExec{}, false
}

func discoverDesktopInstallDirs(nameMatch string) []string {
	var dirs []string
	seen := make(map[string]bool)
	addDir := func(d string) {
		d = strings.TrimSpace(strings.Trim(d, `"`))
		if d == "" {
			return
		}
		d = filepath.Clean(d)
		key := strings.ToLower(d)
		if seen[key] {
			return
		}
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			seen[key] = true
			dirs = append(dirs, d)
		}
	}

	needle := strings.ToLower(strings.TrimSpace(nameMatch))
	uninstallHives := []struct {
		root registry.Key
		path string
	}{
		{registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.LOCAL_MACHINE, `Software\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.LOCAL_MACHINE, `Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`},
	}

	for _, hive := range uninstallHives {
		k, err := registry.OpenKey(hive.root, hive.path, registry.READ)
		if err != nil {
			continue
		}
		subNames, err := k.ReadSubKeyNames(-1)
		_ = k.Close()
		if err != nil {
			continue
		}
		for _, sub := range subNames {
			sk, err := registry.OpenKey(hive.root, hive.path+`\`+sub, registry.READ)
			if err != nil {
				continue
			}
			dispName, _, _ := sk.GetStringValue("DisplayName")
			if needle == "" || strings.Contains(strings.ToLower(dispName), needle) {
				if loc, _, err := sk.GetStringValue("InstallLocation"); err == nil && loc != "" {
					addDir(loc)
				}
				if icon, _, err := sk.GetStringValue("DisplayIcon"); err == nil && icon != "" {
					addDir(filepath.Dir(stripIconSuffix(icon)))
				}
			}
			_ = sk.Close()
		}
	}

	// Fallback standard installation roots across drives.
	if localApp := os.Getenv("LOCALAPPDATA"); localApp != "" {
		addDir(filepath.Join(localApp, "Programs", nameMatch))
	}
	if pf := os.Getenv("ProgramFiles"); pf != "" {
		addDir(filepath.Join(pf, nameMatch))
	}
	for _, root := range []string{`D:\Programs`, `C:\Programs`, `E:\Programs`, `D:\`, `E:\`} {
		addDir(filepath.Join(root, nameMatch))
	}

	return dirs
}

func stripIconSuffix(raw string) string {
	s := strings.TrimSpace(raw)
	if idx := strings.LastIndexByte(s, ','); idx != -1 {
		s = s[:idx]
	}
	return strings.Trim(strings.TrimSpace(s), `"`)
}

func findNodeExecutable() string {
	if path, err := exec.LookPath("node"); err == nil {
		return path
	}
	var cands []string
	if pf := os.Getenv("ProgramFiles"); pf != "" {
		cands = append(cands, filepath.Join(pf, "nodejs", "node.exe"))
	}
	cands = append(cands,
		`C:\Program Files\nodejs\node.exe`,
		`D:\Program Files\nodejs\node.exe`,
	)
	if localApp := os.Getenv("LOCALAPPDATA"); localApp != "" {
		cands = append(cands, filepath.Join(localApp, "Programs", "nodejs", "node.exe"))
	}
	for _, c := range cands {
		if isRegularFile(c) {
			return c
		}
	}
	return ""
}

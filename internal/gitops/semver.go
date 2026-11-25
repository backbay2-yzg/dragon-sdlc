package gitops

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ConventionalChange represents a parsed Conventional Commit item for SemVer and Changelog generation.
type ConventionalChange struct {
	PRNumber int
	Title    string
	Type     string
	Scope    string
	Summary  string
	Breaking bool
}

var conventionalRegex = regexp.MustCompile(`^([a-zA-Z]+)(?:\(([^)]+)\))?(!)?:\s*(.+)$`)

// ParseConventionalTitle parses titles like "feat(ui)!: add mica material".
func ParseConventionalTitle(prNumber int, title string) ConventionalChange {
	trimmed := strings.TrimSpace(title)
	if m := conventionalRegex.FindStringSubmatch(trimmed); m != nil {
		return ConventionalChange{
			PRNumber: prNumber,
			Title:    trimmed,
			Type:     strings.ToLower(m[1]),
			Scope:    strings.TrimSpace(m[2]),
			Summary:  strings.TrimSpace(m[4]),
			Breaking: m[3] == "!" || strings.Contains(strings.ToUpper(trimmed), "BREAKING CHANGE"),
		}
	}
	lower := strings.ToLower(trimmed)
	ctype := "chore"
	switch {
	case strings.Contains(lower, "fix") || strings.Contains(lower, "bug"):
		ctype = "fix"
	case strings.Contains(lower, "feat") || strings.Contains(lower, "add") || strings.Contains(lower, "support"):
		ctype = "feat"
	case strings.Contains(lower, "doc") || strings.Contains(lower, "readme"):
		ctype = "docs"
	}
	return ConventionalChange{
		PRNumber: prNumber,
		Title:    trimmed,
		Type:     ctype,
		Summary:  trimmed,
		Breaking: strings.Contains(strings.ToUpper(trimmed), "BREAKING CHANGE"),
	}
}

type semVer struct {
	major int
	minor int
	patch int
}

func parseSemVer(tag string) (semVer, bool) {
	t := strings.TrimSpace(strings.TrimPrefix(tag, "v"))
	if idx := strings.IndexAny(t, "-+"); idx != -1 {
		t = t[:idx]
	}
	parts := strings.Split(t, ".")
	if len(parts) != 3 {
		return semVer{}, false
	}
	maj, err1 := strconv.Atoi(parts[0])
	min, err2 := strconv.Atoi(parts[1])
	pat, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return semVer{}, false
	}
	return semVer{major: maj, minor: min, patch: pat}, true
}

// LatestSemVerTag picks the highest valid SemVer tag from tags (e.g. "v1.2.3").
func LatestSemVerTag(tags []string) string {
	var bestTag string
	var bestVer semVer
	hasBest := false

	for _, tag := range tags {
		v, ok := parseSemVer(tag)
		if !ok {
			continue
		}
		if !hasBest || compareSemVer(v, bestVer) > 0 {
			bestTag = tag
			bestVer = v
			hasBest = true
		}
	}
	return bestTag
}

func compareSemVer(a, b semVer) int {
	if a.major != b.major {
		if a.major > b.major {
			return 1
		}
		return -1
	}
	if a.minor != b.minor {
		if a.minor > b.minor {
			return 1
		}
		return -1
	}
	if a.patch != b.patch {
		if a.patch > b.patch {
			return 1
		}
		return -1
	}
	return 0
}

// NextVersion computes the next SemVer tag ("vX.Y.Z") and bump type ("major"|"minor"|"patch").
func NextVersion(currentTag string, items []ConventionalChange) (nextTag string, version string, bumpType string) {
	cur, ok := parseSemVer(currentTag)
	if !ok {
		cur = semVer{major: 0, minor: 1, patch: 0}
		if currentTag == "" {
			// Starting fresh from v0.1.0 baseline when no tags exist yet.
			cur = semVer{major: 0, minor: 0, patch: 0}
		}
	}

	bumpType = "patch"
	for _, it := range items {
		if it.Breaking {
			bumpType = "major"
			break
		}
		if it.Type == "feat" && bumpType != "major" {
			bumpType = "minor"
		}
	}

	switch bumpType {
	case "major":
		cur.major++
		cur.minor = 0
		cur.patch = 0
	case "minor":
		cur.minor++
		cur.patch = 0
	default:
		cur.patch++
	}

	version = fmt.Sprintf("%d.%d.%d", cur.major, cur.minor, cur.patch)
	return "v" + version, version, bumpType
}

// GenerateChangelog formats ConventionalChange entries into grouped Markdown release notes.
func GenerateChangelog(tag string, repoFullName string, items []ConventionalChange) string {
	var breaking, feats, fixes, others []ConventionalChange
	for _, it := range items {
		switch {
		case it.Breaking:
			breaking = append(breaking, it)
		case it.Type == "feat":
			feats = append(feats, it)
		case it.Type == "fix":
			fixes = append(fixes, it)
		default:
			others = append(others, it)
		}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("## %s\n\n", tag))

	writeSection := func(heading string, list []ConventionalChange) {
		if len(list) == 0 {
			return
		}
		b.WriteString("### " + heading + "\n\n")
		for _, it := range list {
			prefix := ""
			if it.Scope != "" {
				prefix = "**" + it.Scope + "**: "
			}
			prRef := ""
			if it.PRNumber > 0 {
				if repoFullName != "" {
					prRef = fmt.Sprintf(" ([#%d](https://github.com/%s/pull/%d))", it.PRNumber, repoFullName, it.PRNumber)
				} else {
					prRef = fmt.Sprintf(" (#%d)", it.PRNumber)
				}
			}
			b.WriteString(fmt.Sprintf("- %s%s%s\n", prefix, it.Summary, prRef))
		}
		b.WriteString("\n")
	}

	writeSection("Breaking Changes", breaking)
	writeSection("Features", feats)
	writeSection("Bug Fixes", fixes)
	writeSection("Documentation & Maintenance", others)

	if len(items) == 0 {
		b.WriteString("- Maintenance release.\n")
	}
	return strings.TrimSpace(b.String()) + "\n"
}

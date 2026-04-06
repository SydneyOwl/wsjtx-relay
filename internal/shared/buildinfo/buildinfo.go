package buildinfo

import (
	"fmt"
	"io"
	"strings"
)

var (
	Version = "dev"
	Tag     = ""
	Commit  = "unknown"
	Date    = ""
)

func ReleaseVersion() string {
	return normalizedOrDefault(Version, "dev")
}

func WriteVersion(w io.Writer, binaryName string) error {
	parts := []string{
		binaryName,
		fmt.Sprintf("version=%s", ReleaseVersion()),
	}

	if tag := strings.TrimSpace(Tag); tag != "" {
		parts = append(parts, fmt.Sprintf("tag=%s", tag))
	}
	if commit := normalizedOrDefault(Commit, "unknown"); commit != "unknown" {
		parts = append(parts, fmt.Sprintf("commit=%s", commit))
	}
	if date := strings.TrimSpace(Date); date != "" {
		parts = append(parts, fmt.Sprintf("date=%s", date))
	}

	_, err := fmt.Fprintln(w, strings.Join(parts, " "))
	return err
}

func normalizedOrDefault(value, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

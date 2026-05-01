package version

import "strings"

var (
	value  = "dev"
	commit = "unknown"
)

func Value() string {
	if strings.TrimSpace(value) == "" {
		return "dev"
	}
	return value
}

func Commit() string {
	if strings.TrimSpace(commit) == "" {
		return "unknown"
	}
	return commit
}

func ShortCommit() string {
	c := Commit()
	if len(c) > 12 {
		return c[:12]
	}
	return c
}

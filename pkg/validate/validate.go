package validate

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	namePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)
	idPattern   = regexp.MustCompile(`^[a-f0-9-]{36}$`) // UUID format.
)

// Name validates a user-provided name (session, profile, etc.).
func Name(name string) error {
	if name == "" {
		return nil // Names are optional.
	}
	if !namePattern.MatchString(name) {
		return fmt.Errorf("invalid name %q: must be alphanumeric with ._- (max 128 chars)", name)
	}
	return nil
}

// ID validates a UUID.
func ID(id string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("invalid ID %q: must be a UUID", id)
	}
	return nil
}

// Mode validates an execution mode.
func Mode(mode string) error {
	switch mode {
	case "explore", "execute", "ask", "":
		return nil
	default:
		return fmt.Errorf("invalid mode %q: must be explore, execute, or ask", mode)
	}
}

// PackageName validates a package spec (name or name@version).
func PackageName(pkg string) error {
	parts := strings.SplitN(pkg, "@", 2)
	if parts[0] == "" {
		return fmt.Errorf("empty package name")
	}
	if len(parts[0]) > 64 {
		return fmt.Errorf("package name too long (max 64 chars)")
	}
	return nil
}

// StringLength validates a string is within bounds.
func StringLength(field, value string, maxLen int) error {
	if len(value) > maxLen {
		return fmt.Errorf("%s too long: %d chars (max %d)", field, len(value), maxLen)
	}
	return nil
}

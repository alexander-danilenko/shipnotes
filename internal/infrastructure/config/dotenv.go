package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// loadDotEnvFile copies the variables defined in a .env file into the process
// environment. Variables already present in the environment are left untouched,
// so real environment values take precedence.
//
// explicitPath, when non-empty, names the exact file to load (the --env-file
// flag). A problem opening that file IS an error, because the user asked for it
// specifically. When explicitPath is empty we instead search for the nearest
// .env file (current directory, then walking up through parent directories); a
// missing or unreadable auto-discovered file is not an error, since the tool
// also works with plain environment variables.
func loadDotEnvFile(explicitPath string) error {
	path := explicitPath
	if path == "" {
		discovered, found := findDotEnvFile()
		if !found {
			return nil
		}

		path = discovered
	}

	file, err := os.Open(path) //nolint:gosec // path is user-supplied (--env-file) or found by walking local dirs.
	if err != nil {
		if explicitPath == "" {
			return nil // An auto-discovered file that vanished is not fatal.
		}

		return fmt.Errorf("could not open --env-file %q: %w", explicitPath, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, ok := parseDotEnvLine(scanner.Text())
		if !ok {
			continue
		}

		if _, alreadySet := os.LookupEnv(key); alreadySet {
			continue
		}

		_ = os.Setenv(key, value)
	}

	return nil
}

// findDotEnvFile walks up from the current working directory looking for a
// file named ".env". It returns the path and true on the first match.
func findDotEnvFile() (string, bool) {
	dir, err := os.Getwd()
	if err != nil {
		return "", false
	}

	for {
		candidate := filepath.Join(dir, ".env")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate, true
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false // Reached the filesystem root.
		}

		dir = parent
	}
}

// parseDotEnvLine turns a single line of a .env file into a key/value pair.
// It understands comments (# ...), blank lines, an optional "export " prefix,
// and values wrapped in single or double quotes. It returns ok=false for lines
// that do not define a variable.
func parseDotEnvLine(line string) (key, value string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}

	trimmed = strings.TrimPrefix(trimmed, "export ")

	name, rawValue, found := strings.Cut(trimmed, "=")
	if !found {
		return "", "", false
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", false
	}

	return name, unquoteDotEnvValue(strings.TrimSpace(rawValue)), true
}

// unquoteDotEnvValue removes a matching pair of surrounding quotes, if present.
func unquoteDotEnvValue(value string) string {
	const minQuotedLength = 2 // An opening and a closing quote.
	if len(value) >= minQuotedLength {
		first, last := value[0], value[len(value)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return value[1 : len(value)-1]
		}
	}

	return value
}

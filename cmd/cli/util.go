package main

import (
	"crypto/rand"
	"strings"
	"time"
)

// executionsDir is the default CLI history directory (matches the server
// default so CLI runs and replays share history).
func executionsDir() string { return "./executions" }

// randomHexCLI returns n hex characters of randomness for execution IDs.
func randomHexCLI(n int) string {
	const hex = "0123456789abcdef"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "0000"
	}
	out := make([]byte, n)
	for i, c := range b {
		out[i] = hex[c%16]
	}
	return string(out)
}

func parseRFC3339(s string) (time.Time, error) { return time.Parse(time.RFC3339, s) }

func filepathBase(p string) string {
	// strip dir + yaml suffix to mirror the server's WorkflowFile field.
	base := p
	if i := strings.LastIndexAny(base, "/\\"); i >= 0 {
		base = base[i+1:]
	}
	return strings.TrimSuffix(strings.TrimSuffix(base, ".yaml"), ".yml")
}

// sortedKeys returns map keys in sorted order for deterministic display.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// simple insertion sort (maps are small here)
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}

func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// resolveDir returns the value of the deprecated --dir alias when it was set,
// otherwise the value of the new (renamed) flag.
func resolveDir(newVal, legacyVal string) string {
	if legacyVal != "" {
		return legacyVal
	}
	return newVal
}

// parseVarFlags converts repeated --var key=value pairs into a map. Pairs
// without '=' are silently dropped (same as the old hand-rolled parser).
func parseVarFlags(pairs []string) map[string]string {
	vars := make(map[string]string, len(pairs))
	for _, p := range pairs {
		if kv := strings.SplitN(p, "=", 2); len(kv) == 2 {
			vars[kv[0]] = kv[1]
		}
	}
	return vars
}

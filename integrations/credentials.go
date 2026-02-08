package integrations

import (
	"fmt"
	"os"
	"strings"
)

func ResolveSecret(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("secret ref is required")
	}
	if strings.HasPrefix(ref, "env:") {
		key := strings.TrimSpace(strings.TrimPrefix(ref, "env:"))
		if key == "" {
			return "", fmt.Errorf("empty env key in secret ref")
		}
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			return "", fmt.Errorf("secret env %q is empty", key)
		}
		return value, nil
	}
	return "", fmt.Errorf("unsupported secret ref %q", ref)
}

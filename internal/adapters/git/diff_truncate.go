package git

import "unicode/utf8"

// TruncateDiff caps a diff string to maxSizeKB and returns whether it was truncated.
// If maxSizeKB <= 0, the diff is dropped and marked truncated.
func TruncateDiff(diff string, maxSizeKB int) (string, bool) {
	if maxSizeKB <= 0 {
		if diff == "" {
			return "", false
		}
		return "", true
	}

	maxBytes := maxSizeKB * 1024
	if len(diff) <= maxBytes {
		return diff, false
	}

	data := []byte(diff)
	if len(data) <= maxBytes {
		return diff, false
	}

	truncated := data[:maxBytes]
	for len(truncated) > 0 && !utf8.Valid(truncated) {
		truncated = truncated[:len(truncated)-1]
	}

	return string(truncated), true
}

package codex

import "testing"

func TestSummarizeToolForExplored_SkipsViewImage(t *testing.T) {
	got := summarizeToolForExplored("view_image", map[string]interface{}{
		"path": ".cdev/images/img_6ab0243f-1a6.jpg",
	})
	if got != "" {
		t.Fatalf("got %q, want empty summary", got)
	}
}

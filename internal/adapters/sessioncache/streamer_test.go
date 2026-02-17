package sessioncache

import "testing"

func TestNormalizeStreamerToolInput_ViewImageCompactsDotCdevPath(t *testing.T) {
	input := map[string]interface{}{
		"path": "/Users/brianly/Projects/cdev/.cdev/images/img_6ab0243f-1a6.jpg",
	}

	normalizeStreamerToolInput("view_image", input)

	got, _ := input["path"].(string)
	want := ".cdev/images/img_6ab0243f-1a6.jpg"
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

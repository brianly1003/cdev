package events

// GitDiffPayload is the payload for git_diff events.
type GitDiffPayload struct {
	File      string `json:"file"`
	Diff      string `json:"diff"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	IsStaged  bool   `json:"is_staged"`
	IsNewFile bool   `json:"is_new_file"`
}

// NewGitDiffEvent creates a new git_diff event.
func NewGitDiffEvent(file, diff string, additions, deletions int, isStaged, isNewFile bool) *BaseEvent {
	return NewEvent(EventTypeGitDiff, GitDiffPayload{
		File:      file,
		Diff:      diff,
		Additions: additions,
		Deletions: deletions,
		IsStaged:  isStaged,
		IsNewFile: isNewFile,
	})
}

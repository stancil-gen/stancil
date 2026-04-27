package diff

import (
	"strings"
)

// LineKind classifies a diff line.
type LineKind int

const (
	LineContext LineKind = iota // unchanged line shown for context
	LineAdded                   // line present only in new
	LineRemoved                 // line present only in old
)

// DiffLine is one line in a unified diff hunk.
type DiffLine struct {
	Kind    LineKind
	Content string // raw text, no trailing newline
}

// Hunk is one contiguous region of change in a unified diff.
type Hunk struct {
	OldStart int // 1-based line number in old file
	OldLen   int // number of lines from old file in this hunk
	NewStart int // 1-based line number in new file
	NewLen   int // number of lines from new file in this hunk
	Lines    []DiffLine
}

// FileDiff is the complete diff for one file pair.
type FileDiff struct {
	OldPath string  // path of the old file ("" when new)
	NewPath string  // path of the new file ("" when deleted)
	IsNew   bool    // true when the file didn't exist before
	IsDeleted bool  // true when the file no longer exists
	Hunks   []Hunk
}

// DiffFiles computes a unified diff between old and new text, using context
// lines on each side of a change. contextLines is typically 3.
func DiffFiles(oldPath, newPath, oldText, newText string, contextLines int) FileDiff {
	fd := FileDiff{OldPath: oldPath, NewPath: newPath}

	old := splitLines(oldText)
	nw := splitLines(newText)

	// Compute the edit script via LCS.
	ops := editScript(old, nw)

	// Group into hunks with context.
	fd.Hunks = groupHunks(ops, old, nw, contextLines)
	return fd
}

// NewFileDiff builds a FileDiff for a brand-new file (all lines added).
func NewFileDiff(path, text string, contextLines int) FileDiff {
	lines := splitLines(text)
	hunkLines := make([]DiffLine, len(lines))
	for i, l := range lines {
		hunkLines[i] = DiffLine{Kind: LineAdded, Content: l}
	}
	fd := FileDiff{
		OldPath: "/dev/null",
		NewPath: path,
		IsNew:   true,
	}
	if len(hunkLines) > 0 {
		fd.Hunks = []Hunk{{
			OldStart: 0,
			OldLen:   0,
			NewStart: 1,
			NewLen:   len(lines),
			Lines:    hunkLines,
		}}
	}
	return fd
}

// DeletedFileDiff builds a FileDiff for a file that no longer exists.
func DeletedFileDiff(path, text string, contextLines int) FileDiff {
	lines := splitLines(text)
	hunkLines := make([]DiffLine, len(lines))
	for i, l := range lines {
		hunkLines[i] = DiffLine{Kind: LineRemoved, Content: l}
	}
	fd := FileDiff{
		OldPath:   path,
		NewPath:   "/dev/null",
		IsDeleted: true,
	}
	if len(hunkLines) > 0 {
		fd.Hunks = []Hunk{{
			OldStart: 1,
			OldLen:   len(lines),
			NewStart: 0,
			NewLen:   0,
			Lines:    hunkLines,
		}}
	}
	return fd
}

// HasChanges reports whether the diff contains any added or removed lines.
func (fd FileDiff) HasChanges() bool {
	return len(fd.Hunks) > 0
}

// ─── Edit script via LCS ──────────────────────────────────────────────────────

type opKind int

const (
	opEqual   opKind = iota
	opInsert         // line exists only in new
	opDelete         // line exists only in old
)

type op struct {
	kind   opKind
	oldIdx int // index into old slice (-1 for insert)
	newIdx int // index into new slice (-1 for delete)
}

// editScript computes the minimal edit sequence using LCS.
func editScript(old, nw []string) []op {
	m, n := len(old), len(nw)

	// dp[i][j] = LCS length for old[:i] and nw[:j]
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if old[i-1] == nw[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] > dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	// Backtrack to produce the edit script.
	ops := make([]op, 0, m+n)
	i, j := m, n
	for i > 0 || j > 0 {
		switch {
		case i > 0 && j > 0 && old[i-1] == nw[j-1]:
			ops = append(ops, op{kind: opEqual, oldIdx: i - 1, newIdx: j - 1})
			i--
			j--
		case j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]):
			ops = append(ops, op{kind: opInsert, oldIdx: -1, newIdx: j - 1})
			j--
		default:
			ops = append(ops, op{kind: opDelete, oldIdx: i - 1, newIdx: -1})
			i--
		}
	}

	// Reverse (backtracking gave us reverse order).
	for l, r := 0, len(ops)-1; l < r; l, r = l+1, r-1 {
		ops[l], ops[r] = ops[r], ops[l]
	}
	return ops
}

// ─── Hunk grouping ────────────────────────────────────────────────────────────

func groupHunks(ops []op, old, nw []string, ctx int) []Hunk {
	// Identify changed op indices.
	var changedIdx []int
	for i, o := range ops {
		if o.kind != opEqual {
			changedIdx = append(changedIdx, i)
		}
	}
	if len(changedIdx) == 0 {
		return nil
	}

	// Build hunk windows: [start, end) op indices with context padding.
	type window struct{ start, end int }
	var windows []window
	for _, ci := range changedIdx {
		s := max(0, ci-ctx)
		e := min(len(ops), ci+ctx+1)
		if len(windows) > 0 && s <= windows[len(windows)-1].end {
			windows[len(windows)-1].end = e // merge overlapping windows
		} else {
			windows = append(windows, window{s, e})
		}
	}

	var hunks []Hunk
	for _, w := range windows {
		// Count old/new line numbers for the hunk header.
		oldStart, newStart := -1, -1
		oldLen, newLen := 0, 0
		var lines []DiffLine

		for _, o := range ops[w.start:w.end] {
			switch o.kind {
			case opEqual:
				if oldStart == -1 {
					oldStart = o.oldIdx + 1
					newStart = o.newIdx + 1
				}
				lines = append(lines, DiffLine{Kind: LineContext, Content: old[o.oldIdx]})
				oldLen++
				newLen++
			case opDelete:
				if oldStart == -1 {
					oldStart = o.oldIdx + 1
					newStart = 1 // will be refined below
				}
				lines = append(lines, DiffLine{Kind: LineRemoved, Content: old[o.oldIdx]})
				oldLen++
			case opInsert:
				if newStart == -1 || (oldStart == -1) {
					if oldStart == -1 {
						oldStart = 1
					}
					if newStart == -1 {
						newStart = o.newIdx + 1
					}
				}
				lines = append(lines, DiffLine{Kind: LineAdded, Content: nw[o.newIdx]})
				newLen++
			}
		}

		// Fix newStart when first op is a delete.
		if newStart == -1 || newStart == 1 {
			// Use the new index of the first context or insert op.
			for _, o := range ops[w.start:w.end] {
				if o.newIdx >= 0 {
					newStart = o.newIdx + 1
					break
				}
			}
			if newStart == -1 {
				newStart = 1
			}
		}
		if oldStart == -1 {
			oldStart = 1
		}

		hunks = append(hunks, Hunk{
			OldStart: oldStart,
			OldLen:   oldLen,
			NewStart: newStart,
			NewLen:   newLen,
			Lines:    lines,
		})
	}
	return hunks
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	text = strings.TrimRight(text, "\n")
	return strings.Split(text, "\n")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

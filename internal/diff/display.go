package diff

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// ANSI color codes.
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorCyan   = "\033[36m"
	colorBold   = "\033[1m"
	colorGray   = "\033[90m"
)

// DiffSummary holds counts across all files in a diff run.
type DiffSummary struct {
	Added     int // files that are entirely new
	Modified  int // files that changed
	Deleted   int // files that no longer exist
	Unchanged int // files with no diff
}

// PrintFileDiff writes a single FileDiff to w in unified diff format with ANSI colors.
// Pass color=false for plain text (useful when piped or when NO_COLOR is set).
func PrintFileDiff(w io.Writer, fd FileDiff, color bool) {
	if !fd.HasChanges() {
		return
	}

	// File header
	oldLabel := "a/" + fd.OldPath
	newLabel := "b/" + fd.NewPath
	if fd.IsNew {
		oldLabel = "/dev/null"
	}
	if fd.IsDeleted {
		newLabel = "/dev/null"
	}

	// The label in "diff --stencil <path>" uses the meaningful path in all cases.
	displayPath := fd.NewPath
	if fd.IsDeleted {
		displayPath = fd.OldPath
	}

	header := fmt.Sprintf("--- %s\n+++ %s\n", oldLabel, newLabel)
	if color {
		fmt.Fprintf(w, "%s%s%s%s", colorBold, "diff --stencil "+displayPath+"\n", colorReset, header)
	} else {
		fmt.Fprintf(w, "diff --stencil %s\n%s", displayPath, header)
	}

	for _, hunk := range fd.Hunks {
		// Hunk header
		hunkHeader := fmt.Sprintf("@@ -%d,%d +%d,%d @@\n",
			hunk.OldStart, hunk.OldLen,
			hunk.NewStart, hunk.NewLen)
		if color {
			fmt.Fprintf(w, "%s%s%s", colorCyan, hunkHeader, colorReset)
		} else {
			fmt.Fprint(w, hunkHeader)
		}

		for _, line := range hunk.Lines {
			switch line.Kind {
			case LineAdded:
				if color {
					fmt.Fprintf(w, "%s+%s%s\n", colorGreen, line.Content, colorReset)
				} else {
					fmt.Fprintf(w, "+%s\n", line.Content)
				}
			case LineRemoved:
				if color {
					fmt.Fprintf(w, "%s-%s%s\n", colorRed, line.Content, colorReset)
				} else {
					fmt.Fprintf(w, "-%s\n", line.Content)
				}
			case LineContext:
				if color {
					fmt.Fprintf(w, "%s %s%s\n", colorGray, line.Content, colorReset)
				} else {
					fmt.Fprintf(w, " %s\n", line.Content)
				}
			}
		}
	}
}

// PrintSummary writes the final summary line to w.
func PrintSummary(w io.Writer, s DiffSummary, color bool) {
	if s.Added+s.Modified+s.Deleted == 0 {
		if color {
			fmt.Fprintf(w, "\n%s✓ No changes — generated code is up to date.%s\n", colorGreen, colorReset)
		} else {
			fmt.Fprintln(w, "\n✓ No changes — generated code is up to date.")
		}
		return
	}

	parts := []string{}
	if s.Added > 0 {
		parts = append(parts, fmt.Sprintf("%d added", s.Added))
	}
	if s.Modified > 0 {
		parts = append(parts, fmt.Sprintf("%d modified", s.Modified))
	}
	if s.Deleted > 0 {
		parts = append(parts, fmt.Sprintf("%d deleted", s.Deleted))
	}
	if s.Unchanged > 0 {
		parts = append(parts, fmt.Sprintf("%d unchanged", s.Unchanged))
	}

	line := "\n" + strings.Join(parts, ", ")
	if color {
		fmt.Fprintf(w, "%s%s%s\n", colorBold, line, colorReset)
	} else {
		fmt.Fprintln(w, line)
	}
}

// UseColor returns true when the terminal supports ANSI color and NO_COLOR is not set.
func UseColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	// Output is a terminal, not a pipe or file.
	return (fi.Mode() & os.ModeCharDevice) != 0
}

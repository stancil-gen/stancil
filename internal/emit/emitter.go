package emitter

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/tools/imports"
)

const lockFileName = ".stencil.lock"

type File struct {
	Path     string
	Content  []byte
	Scaffold bool // if true, write to ScaffoldDir instead of OutputDir
}

type Emitter struct {
	OutputDir   string
	ScaffoldDir string // project root — for main.go, go.mod, hooks/, config/
	Staged      []File
	Scaffold    []File // scaffold files — only written if they don't exist
	lockHash    string
}

func NewEmitter(outDir, scaffoldDir, specHash string) *Emitter {
	return &Emitter{
		OutputDir:   outDir,
		ScaffoldDir: scaffoldDir,
		lockHash:    specHash,
	}
}

// Stage natively aggregates into local buffer matrices
func (e *Emitter) Stage(f File) {
	e.Staged = append(e.Staged, f)
}

// StageScaffold queues a file to be written to ScaffoldDir (only if it doesn't already exist)
func (e *Emitter) StageScaffold(f File) {
	e.Scaffold = append(e.Scaffold, f)
}

// Flush safely sweeps buffered boundaries utilizing protective permissions.
// It backs up the existing output, wipes it clean, writes all staged files,
// then locks everything read-only. Stale files from previous runs are removed.
func (e *Emitter) Flush() error {
	if len(e.Staged) == 0 {
		return nil
	}

	e.unlockOutputDir()
	if err := e.backup(); err != nil {
		return err
	}

	// Wipe the output directory so stale files from previous runs are removed.
	if err := os.RemoveAll(e.OutputDir); err != nil {
		return err
	}

	for _, f := range e.Staged {
		if err := e.writeFile(f); err != nil {
			e.rollback()
			return err
		}
	}

	e.lockOutputDir()

	// Write scaffold files (only if they don't already exist).
	for _, f := range e.Scaffold {
		fullPath := filepath.Join(e.ScaffoldDir, f.Path)
		if _, err := os.Stat(fullPath); err == nil {
			continue // file exists — don't overwrite
		}
		_ = e.writeScaffoldFile(f)
	}

	// Generation succeeded — remove the backup.
	_ = os.RemoveAll(e.getBackupDir())

	return e.writeLockFile()
}

func (e *Emitter) writeFile(f File) error {
	fullPath := filepath.Join(e.OutputDir, f.Path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return err
	}

	// Auto format logic seamlessly checks Go target bounds!
	if strings.HasSuffix(f.Path, ".go") {
		if formatted, err := imports.Process(fullPath, f.Content, nil); err == nil {
			f.Content = formatted
		}
	}

	return os.WriteFile(fullPath, f.Content, 0644)
}

func (e *Emitter) writeScaffoldFile(f File) error {
	fullPath := filepath.Join(e.ScaffoldDir, f.Path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return err
	}
	// Auto-format Go files
	if strings.HasSuffix(f.Path, ".go") {
		if formatted, err := imports.Process(fullPath, f.Content, nil); err == nil {
			f.Content = formatted
		}
	}
	return os.WriteFile(fullPath, f.Content, 0644)
}

func (e *Emitter) getBackupDir() string {
	return e.OutputDir + "_stencil_backup"
}

func (e *Emitter) backup() error {
	// If the output directory doesn't exist natively, there is no prior framework to backup!
	if _, err := os.Stat(e.OutputDir); os.IsNotExist(err) {
		return nil
	}

	backupDir := e.getBackupDir()
	
	// Remove any stale backup blocks from previous runtime catastrophes just in case
	_ = os.RemoveAll(backupDir)

	// Recursively duplicate existing bounds safely into memory isolation
	return e.copyDir(e.OutputDir, backupDir)
}

func (e *Emitter) rollback() {
	backupDir := e.getBackupDir()
	
	// If no backup exists natively, it means this was a fresh target; we must just wipe the corrupted partial files we just created.
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		_ = os.RemoveAll(e.OutputDir)
		return
	}

	// Vaporize corrupted partial output arrays entirely
	_ = os.RemoveAll(e.OutputDir)
	
	// Restore baseline backup gracefully natively shielding the user's project
	_ = os.Rename(backupDir, e.OutputDir)
}

// copyDir safely duplicates an entire directory structure iteratively protecting physical dependencies 
func (e *Emitter) copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		
		targetPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(targetPath, info.Mode())
		}
		
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		
		return os.WriteFile(targetPath, data, info.Mode())
	})
}

// unlockOutputDir removes immutable flags and restores write permissions
// so the emitter can wipe and rewrite the output directory.
func (e *Emitter) unlockOutputDir() {
	if runtime.GOOS == "darwin" {
		_ = exec.Command("chflags", "-R", "nouchg", e.OutputDir).Run()
	}
	_ = filepath.WalkDir(e.OutputDir, func(path string, d fs.DirEntry, err error) error {
		if err == nil {
			_ = os.Chmod(path, 0755)
		}
		return nil
	})
}

// lockOutputDir makes all generated files and directories immutable.
// On macOS: uses chflags uchg so even the file owner cannot edit without
// explicitly removing the flag first. On Linux: falls back to 0444/0555 chmod.
func (e *Emitter) lockOutputDir() {
	_ = filepath.WalkDir(e.OutputDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			_ = os.Chmod(path, 0555)
		} else {
			_ = os.Chmod(path, 0444)
		}
		return nil
	})
	if runtime.GOOS == "darwin" {
		_ = exec.Command("chflags", "-R", "uchg", e.OutputDir).Run()
	}
}

// writeLockFile writes .stencil.lock into ScaffoldDir (next to stencil.yaml).
func (e *Emitter) writeLockFile() error {
	files := make([]string, len(e.Staged))
	for i, f := range e.Staged {
		files[i] = f.Path
	}
	lock := map[string]interface{}{
		"spec_hash": e.lockHash,
		"files":     files,
	}
	data, _ := json.MarshalIndent(lock, "", "  ")
	return os.WriteFile(filepath.Join(e.ScaffoldDir, lockFileName), data, 0644)
}

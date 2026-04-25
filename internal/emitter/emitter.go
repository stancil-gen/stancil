package emitter

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/imports"
)

type File struct {
	Path    string
	Content []byte
}

type Emitter struct {
	OutputDir string
	Staged    []File
	lockHash  string
}

func NewEmitter(outDir, specHash string) *Emitter {
	return &Emitter{
		OutputDir: outDir,
		lockHash:  specHash,
	}
}

// Stage natively aggregates into local buffer matrices
func (e *Emitter) Stage(f File) {
	e.Staged = append(e.Staged, f)
}

// Flush safely sweeps buffered boundaries identically utilizing protective permissions 
func (e *Emitter) Flush() error {
	if len(e.Staged) == 0 {
		return nil
	}

	e.unlockOutputDir()
	if err := e.backup(); err != nil {
		return err
	}

	for _, f := range e.Staged {
		if err := e.writeFile(f); err != nil {
			e.rollback() // Trap any mathematical disk anomalies and revert prior
			return err
		}
	}

	e.lockOutputDir()
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

// unlockOutputDir drops access structures universally exposing writing rights
func (e *Emitter) unlockOutputDir() {
	_ = filepath.WalkDir(e.OutputDir, func(path string, d fs.DirEntry, err error) error {
		if err == nil {
			_ = os.Chmod(path, 0755)
		}
		return nil
	})
}

// lockOutputDir aggressively secures generated paths to heavily punish user-led alterations against the AST generated bounds!
func (e *Emitter) lockOutputDir() {
	_ = filepath.WalkDir(e.OutputDir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			_ = os.Chmod(path, 0444) // Strict Read Only Mode Enforcement
		}
		return nil
	})
}

// writeLockFile builds the absolute root path validation array manifest protecting cross-run executions natively
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
	return os.WriteFile(".stencil.lock", data, 0644)
}

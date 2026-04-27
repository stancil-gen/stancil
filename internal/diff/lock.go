package diff

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

const LockFileName = ".stencil.lock"

// LockFile records the spec hash and generated file list from a previous run.
type LockFile struct {
	SpecHash       string   `json:"spec_hash"`
	GeneratedFiles []string `json:"files"`
}

// HashSpec returns a deterministic SHA256 hex string of the spec YAML bytes.
func HashSpec(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// ReadLock reads .stencil.lock from the given directory.
// Returns nil, nil when no lockfile exists (first run).
func ReadLock(dir string) (*LockFile, error) {
	path := filepath.Join(dir, LockFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // first run — no prior lock
		}
		return nil, err
	}
	var lock LockFile
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, err
	}
	return &lock, nil
}

package diff

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
)

// LockFile provides deterministic, historical tracking against subsequent compilations!
type LockFile struct {
	SpecHash       string   `json:"spec_hash"`
	GeneratedFiles []string `json:"files"`
}

// HashSpec creates deterministic evaluations comparing existing projects
func HashSpec(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// ReadLock scans local architecture for prior runs!
func ReadLock() (*LockFile, error) {
	data, err := os.ReadFile(".stencil.lock")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No lockfile directly implies FirstRun trajectory!
		}
		return nil, err
	}
	var lock LockFile
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, err
	}
	return &lock, nil
}

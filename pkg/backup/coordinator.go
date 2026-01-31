// Package backup provides backup and recovery for GibRAM
package backup

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// BackupCoordinator implements two-phase commit for backup operations
type BackupCoordinator struct {
	walWriter    *WAL // Changed from WALWriter to WAL
	snapshotPath string
	mu           sync.Mutex
	state        BackupState
	preparedLSN  uint64
	tmpFiles     []string // Track temp files for cleanup on abort
}

// BackupState represents the state of a backup operation
type BackupState int

const (
	BackupStateIdle BackupState = iota
	BackupStatePrepared
	BackupStateCommitted
	BackupStateAborted
)

// NewBackupCoordinator creates a new backup coordinator
func NewBackupCoordinator(walWriter *WAL, snapshotPath string) *BackupCoordinator {
	return &BackupCoordinator{
		walWriter:    walWriter,
		snapshotPath: snapshotPath,
		state:        BackupStateIdle,
	}
}

// Prepare implements phase 1 of 2PC - prepare all resources
func (bc *BackupCoordinator) Prepare() (uint64, error) {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if bc.state != BackupStateIdle {
		return 0, fmt.Errorf("backup already in progress (state: %d)", bc.state)
	}

	// Phase 1a: Flush WAL to ensure all operations are persisted
	if err := bc.walWriter.Flush(); err != nil {
		return 0, fmt.Errorf("failed to flush WAL: %w", err)
	}

	// Phase 1b: Get current LSN - this is our consistency point
	lsn := bc.walWriter.GetCurrentLSN()
	bc.preparedLSN = lsn

	// Phase 1c: Mark state as prepared
	bc.state = BackupStatePrepared

	return lsn, nil
}

// Commit implements phase 2 of 2PC - commit the backup
func (bc *BackupCoordinator) Commit(writeFunc func(w *SnapshotWriter) error) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if bc.state != BackupStatePrepared {
		return fmt.Errorf("backup not prepared (state: %d)", bc.state)
	}

	// Phase 2a: Create snapshot with prepared LSN
	// The snapshot writer already uses atomic write-to-temp pattern
	err := CreateSnapshot(bc.snapshotPath, bc.preparedLSN, writeFunc)
	if err != nil {
		bc.state = BackupStateAborted
		return fmt.Errorf("failed to create snapshot: %w", err)
	}

	// Phase 2b: Flush WAL again to ensure snapshot metadata is persisted
	if err := bc.walWriter.Flush(); err != nil {
		// Snapshot was created but WAL flush failed
		// Remove the snapshot to maintain consistency
		bc.state = BackupStateAborted
		if rmErr := os.Remove(bc.snapshotPath); rmErr != nil && !os.IsNotExist(rmErr) {
			return fmt.Errorf("failed to flush WAL after snapshot: %v (cleanup failed: %v)", err, rmErr)
		}
		return fmt.Errorf("failed to flush WAL after snapshot: %w", err)
	}

	// Phase 2c: Mark state as committed
	bc.state = BackupStateCommitted

	return nil
}

// Abort aborts the backup operation
func (bc *BackupCoordinator) Abort() error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if bc.state != BackupStatePrepared {
		return fmt.Errorf("backup not prepared (state: %d)", bc.state)
	}

	// Clean up any temporary files
	var cleanupErr error
	for _, tmpFile := range bc.tmpFiles {
		if err := os.Remove(tmpFile); err != nil && !os.IsNotExist(err) && cleanupErr == nil {
			cleanupErr = err
		}
	}
	bc.tmpFiles = nil

	bc.state = BackupStateAborted
	if cleanupErr != nil {
		return fmt.Errorf("cleanup failed: %w", cleanupErr)
	}
	return nil
}

// Reset resets the coordinator for a new backup operation
func (bc *BackupCoordinator) Reset() {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	bc.state = BackupStateIdle
	bc.preparedLSN = 0
	bc.tmpFiles = nil
}

// GetState returns the current state
func (bc *BackupCoordinator) GetState() BackupState {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	return bc.state
}

// ExecuteBackup performs a complete 2PC backup operation
func (bc *BackupCoordinator) ExecuteBackup(writeFunc func(w *SnapshotWriter) error) error {
	// Phase 1: Prepare
	_, err := bc.Prepare()
	if err != nil {
		return fmt.Errorf("prepare phase failed: %w", err)
	}

	// Phase 2: Commit (or abort on error)
	err = bc.Commit(writeFunc)
	if err != nil {
		// Attempt to abort if commit failed
		if abortErr := bc.Abort(); abortErr != nil {
			return fmt.Errorf("commit phase failed: %v (abort failed: %v)", err, abortErr)
		}
		return fmt.Errorf("commit phase failed: %w", err)
	}

	// Reset for next backup
	bc.Reset()

	return nil
}

// BackupMetadata stores metadata about completed backups
type BackupMetadata struct {
	LSN       uint64
	Timestamp time.Time
	Path      string
	Size      int64
}

// GetBackupMetadata retrieves metadata for a completed backup
func (bc *BackupCoordinator) GetBackupMetadata() (*BackupMetadata, error) {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if bc.state != BackupStateCommitted {
		return nil, fmt.Errorf("backup not committed (state: %d)", bc.state)
	}

	stat, err := os.Stat(bc.snapshotPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat snapshot: %w", err)
	}

	return &BackupMetadata{
		LSN:       bc.preparedLSN,
		Timestamp: stat.ModTime(),
		Path:      bc.snapshotPath,
		Size:      stat.Size(),
	}, nil
}

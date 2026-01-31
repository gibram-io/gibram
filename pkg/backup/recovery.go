// Package backup provides backup and recovery for GibRAM
package backup

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Recovery handles disaster recovery and data restoration
type Recovery struct {
	dataDir     string
	walDir      string
	snapshotDir string
	mu          sync.Mutex
}

// NewRecovery creates a new recovery handler
func NewRecovery(dataDir string) *Recovery {
	return &Recovery{
		dataDir:     dataDir,
		walDir:      filepath.Join(dataDir, "wal"),
		snapshotDir: filepath.Join(dataDir, "snapshots"),
	}
}

// RecoveryPlan describes how to recover
type RecoveryPlan struct {
	SnapshotPath string
	WALStartLSN  uint64
	WALFiles     []string
	EstimatedOps int
}

// Plan creates a recovery plan
func (r *Recovery) Plan() (*RecoveryPlan, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	plan := &RecoveryPlan{}

	// Find latest snapshot
	snapshots, err := r.listSnapshots()
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	if len(snapshots) > 0 {
		// Use latest snapshot
		plan.SnapshotPath = snapshots[len(snapshots)-1]

		// Read snapshot header to get LSN
		reader, err := NewSnapshotReader(plan.SnapshotPath)
		if err != nil {
			return nil, err
		}
		plan.WALStartLSN = reader.Header().LSN
		if err := reader.Close(); err != nil {
			return nil, err
		}
	}

	// Find WAL files to replay
	walFiles, err := r.listWALFiles()
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	plan.WALFiles = walFiles

	return plan, nil
}

func (r *Recovery) listSnapshots() ([]string, error) {
	files, err := filepath.Glob(filepath.Join(r.snapshotDir, "*.gibram"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func (r *Recovery) listWALFiles() ([]string, error) {
	files, err := filepath.Glob(filepath.Join(r.walDir, "wal_*.log"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// Execute executes recovery
func (r *Recovery) Execute(plan *RecoveryPlan, restoreFunc func(path string) error, replayFunc func(entry *WALEntry) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Step 1: Restore from snapshot
	if plan.SnapshotPath != "" {
		log.Printf("Recovery: restoring from snapshot %s", plan.SnapshotPath)
		if err := restoreFunc(plan.SnapshotPath); err != nil {
			return fmt.Errorf("restore snapshot: %w", err)
		}
	}

	// Step 2: Replay WAL
	if len(plan.WALFiles) > 0 {
		log.Printf("Recovery: replaying %d WAL files from LSN %d", len(plan.WALFiles), plan.WALStartLSN)

		entries, err := ReadEntries(r.walDir, plan.WALStartLSN)
		if err != nil {
			return fmt.Errorf("read WAL: %w", err)
		}

		for _, entry := range entries {
			if err := replayFunc(entry); err != nil {
				return fmt.Errorf("replay WAL entry %d: %w", entry.LSN, err)
			}
		}

		log.Printf("Recovery: replayed %d WAL entries", len(entries))
	}

	return nil
}

// Cleanup removes old snapshots and WAL files
func (r *Recovery) Cleanup(keepSnapshots, keepWALDays int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Cleanup old snapshots
	snapshots, err := r.listSnapshots()
	if err != nil {
		return err
	}

	if len(snapshots) > keepSnapshots {
		for _, path := range snapshots[:len(snapshots)-keepSnapshots] {
			log.Printf("Cleanup: removing old snapshot %s", path)
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}

	// Cleanup old WAL files
	cutoff := time.Now().AddDate(0, 0, -keepWALDays)
	walFiles, err := r.listWALFiles()
	if err != nil {
		return err
	}

	for _, path := range walFiles {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			log.Printf("Cleanup: removing old WAL %s", path)
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}

	return nil
}

// Verify verifies backup integrity
func (r *Recovery) Verify() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Verify snapshots
	snapshots, _ := r.listSnapshots()
	for _, path := range snapshots {
		reader, err := NewSnapshotReader(path)
		if err != nil {
			return fmt.Errorf("invalid snapshot %s: %w", path, err)
		}
		if err := reader.Close(); err != nil {
			return err
		}
	}

	// Verify WAL files can be read
	_, err := ReadEntries(r.walDir, 0)
	if err != nil {
		return fmt.Errorf("invalid WAL: %w", err)
	}

	return nil
}

// CopyFile copies a file
func CopyFile(src, dst string) (retErr error) {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		if err := srcFile.Close(); err != nil && retErr == nil {
			retErr = err
		}
	}()

	// Create parent directory
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		if err := dstFile.Close(); err != nil && retErr == nil {
			retErr = err
		}
	}()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// GenerateSnapshotName generates a timestamped snapshot name
func GenerateSnapshotName(prefix string) string {
	ts := time.Now().Format("20060102_150405")
	return fmt.Sprintf("%s_%s.gibram", prefix, ts)
}

// ParseSnapshotTime parses timestamp from snapshot filename
func ParseSnapshotTime(filename string) (time.Time, error) {
	// Expected format: prefix_20060102_150405.gibram
	base := filepath.Base(filename)
	base = strings.TrimSuffix(base, ".gibram")

	parts := strings.Split(base, "_")
	if len(parts) < 2 {
		return time.Time{}, fmt.Errorf("invalid snapshot name: %s", filename)
	}

	// Get last two parts (date_time)
	dateStr := parts[len(parts)-2] + "_" + parts[len(parts)-1]
	return time.Parse("20060102_150405", dateStr)
}

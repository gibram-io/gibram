// Package backup provides backup and recovery for GibRAM
package backup

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cespare/xxhash/v2"
)

// WAL (Write-Ahead Log) provides durability through logging
type WAL struct {
	dir  string
	file *os.File
	mu   sync.Mutex

	// State
	currentLSN uint64
	flushedLSN uint64
	segmentNum int

	// Configuration
	maxSegmentSize int64
	syncMode       SyncMode
}

// SyncMode defines when to sync WAL to disk
type SyncMode int

const (
	// SyncEveryWrite syncs after every write (safest, slowest)
	SyncEveryWrite SyncMode = iota

	// SyncPeriodic syncs periodically (balanced)
	SyncPeriodic

	// SyncNever relies on OS buffering (fastest, least safe)
	SyncNever
)

// WALEntry represents a single WAL entry
type WALEntry struct {
	LSN       uint64
	Timestamp int64
	Type      EntryType
	Key       string
	Data      []byte
	Checksum  uint64 // xxHash64 checksum
}

// EntryType defines the type of WAL entry
type EntryType uint8

const (
	EntryInsert EntryType = iota + 1
	EntryUpdate
	EntryDelete
	EntryCheckpoint
)

// NewWAL creates a new WAL
func NewWAL(dir string, syncMode SyncMode) (*WAL, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create WAL directory: %w", err)
	}

	w := &WAL{
		dir:            dir,
		maxSegmentSize: 64 * 1024 * 1024, // 64MB
		syncMode:       syncMode,
	}

	// Open or create current segment
	if err := w.openSegment(0); err != nil {
		return nil, err
	}

	return w, nil
}

func (w *WAL) openSegment(num int) error {
	path := filepath.Join(w.dir, fmt.Sprintf("wal_%08d.log", num))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	if w.file != nil {
		if err := w.file.Close(); err != nil {
			return err
		}
	}

	w.file = f
	w.segmentNum = num
	return nil
}

// Append appends an entry to the WAL
func (w *WAL) Append(entryType EntryType, key string, data []byte) (uint64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.currentLSN++
	entry := &WALEntry{
		LSN:       w.currentLSN,
		Timestamp: time.Now().UnixNano(),
		Type:      entryType,
		Key:       key,
		Data:      data,
	}

	// Calculate checksum
	entry.Checksum = w.calculateChecksum(entry)

	// Write entry
	if err := w.writeEntry(entry); err != nil {
		return 0, err
	}

	// Sync if needed
	if w.syncMode == SyncEveryWrite {
		if err := w.file.Sync(); err != nil {
			return 0, err
		}
		w.flushedLSN = w.currentLSN
	}

	// Check if need to rotate
	info, _ := w.file.Stat()
	if info.Size() > w.maxSegmentSize {
		if err := w.openSegment(w.segmentNum + 1); err != nil {
			return 0, err
		}
	}

	return entry.LSN, nil
}

func (w *WAL) writeEntry(entry *WALEntry) error {
	// Format: [8 LSN][8 timestamp][1 type][4 key_len][key][4 data_len][data][8 checksum]
	keyBytes := []byte(entry.Key)
	totalLen := 8 + 8 + 1 + 4 + len(keyBytes) + 4 + len(entry.Data) + 8 // xxHash64 = 8 bytes

	buf := make([]byte, totalLen)
	offset := 0

	binary.BigEndian.PutUint64(buf[offset:], entry.LSN)
	offset += 8

	binary.BigEndian.PutUint64(buf[offset:], uint64(entry.Timestamp))
	offset += 8

	buf[offset] = byte(entry.Type)
	offset++

	binary.BigEndian.PutUint32(buf[offset:], uint32(len(keyBytes)))
	offset += 4

	copy(buf[offset:], keyBytes)
	offset += len(keyBytes)

	binary.BigEndian.PutUint32(buf[offset:], uint32(len(entry.Data)))
	offset += 4

	copy(buf[offset:], entry.Data)
	offset += len(entry.Data)

	binary.BigEndian.PutUint64(buf[offset:], entry.Checksum)

	_, err := w.file.Write(buf)
	return err
}

func (w *WAL) calculateChecksum(entry *WALEntry) uint64 {
	h := xxhash.New()
	if err := binary.Write(h, binary.BigEndian, entry.LSN); err != nil {
		return 0
	}
	if err := binary.Write(h, binary.BigEndian, entry.Timestamp); err != nil {
		return 0
	}
	if _, err := h.Write([]byte{byte(entry.Type)}); err != nil {
		return 0
	}
	if _, err := h.Write([]byte(entry.Key)); err != nil {
		return 0
	}
	if _, err := h.Write(entry.Data); err != nil {
		return 0
	}
	return h.Sum64()
}

// Sync flushes WAL to disk
func (w *WAL) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.file.Sync(); err != nil {
		return err
	}
	w.flushedLSN = w.currentLSN
	return nil
}

// Flush is an alias for Sync for compatibility with BackupCoordinator
func (w *WAL) Flush() error {
	return w.Sync()
}

// GetCurrentLSN returns the current LSN (alias for CurrentLSN for coordinator compatibility)
func (w *WAL) GetCurrentLSN() uint64 {
	return w.CurrentLSN()
}

// Close closes the WAL
func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file != nil {
		if err := w.file.Sync(); err != nil {
			if closeErr := w.file.Close(); closeErr != nil {
				return fmt.Errorf("sync failed: %v (close failed: %v)", err, closeErr)
			}
			return err
		}
		return w.file.Close()
	}
	return nil
}

// CurrentLSN returns the current LSN
func (w *WAL) CurrentLSN() uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.currentLSN
}

// FlushedLSN returns the flushed LSN
func (w *WAL) FlushedLSN() uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.flushedLSN
}

// SegmentCount returns the current number of WAL segments
func (w *WAL) SegmentCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.segmentNum + 1
}

// TotalSize returns the total size of all WAL files in bytes
func (w *WAL) TotalSize() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()

	var totalSize int64
	files, err := filepath.Glob(filepath.Join(w.dir, "wal_*.log"))
	if err != nil {
		return 0
	}

	for _, path := range files {
		info, err := os.Stat(path)
		if err == nil {
			totalSize += info.Size()
		}
	}
	return totalSize
}

// TruncateBefore removes WAL entries before the given LSN
// It deletes old segment files that are fully below the target LSN
func (w *WAL) TruncateBefore(targetLSN uint64) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	files, err := filepath.Glob(filepath.Join(w.dir, "wal_*.log"))
	if err != nil {
		return err
	}

	for _, path := range files {
		// Don't delete the current segment
		if w.file != nil {
			currentPath := filepath.Join(w.dir, fmt.Sprintf("wal_%08d.log", w.segmentNum))
			if path == currentPath {
				continue
			}
		}

		// Check if all entries in this file are below target LSN
		entries, err := readEntriesFromFile(path, 0)
		if err != nil {
			continue
		}

		// If file is empty or all entries are below target, delete it
		allBelow := true
		for _, entry := range entries {
			if entry.LSN >= targetLSN {
				allBelow = false
				break
			}
		}

		if allBelow && len(entries) > 0 {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}

	return nil
}

// Rotate forces rotation to a new WAL segment
func (w *WAL) Rotate() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Sync current segment
	if w.file != nil {
		if err := w.file.Sync(); err != nil {
			return err
		}
		w.flushedLSN = w.currentLSN
	}

	// Open new segment
	return w.openSegment(w.segmentNum + 1)
}

// ReadEntries reads all entries from WAL directory
func ReadEntries(dir string, fromLSN uint64) ([]*WALEntry, error) {
	files, err := filepath.Glob(filepath.Join(dir, "wal_*.log"))
	if err != nil {
		return nil, err
	}

	var entries []*WALEntry
	for _, path := range files {
		fileEntries, err := readEntriesFromFile(path, fromLSN)
		if err != nil {
			return nil, err
		}
		entries = append(entries, fileEntries...)
	}

	return entries, nil
}

func readEntriesFromFile(path string, fromLSN uint64) (entries []*WALEntry, retErr error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := f.Close(); err != nil && retErr == nil {
			retErr = err
		}
	}()

	for {
		entry, err := readEntry(f)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		if entry.LSN >= fromLSN {
			entries = append(entries, entry)
		}
	}

	return entries, nil
}

func readEntry(r io.Reader) (*WALEntry, error) {
	entry := &WALEntry{}

	// Read fixed header
	header := make([]byte, 17) // 8 + 8 + 1
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}

	entry.LSN = binary.BigEndian.Uint64(header[0:8])
	entry.Timestamp = int64(binary.BigEndian.Uint64(header[8:16]))
	entry.Type = EntryType(header[16])

	// Read key
	var keyLen uint32
	if err := binary.Read(r, binary.BigEndian, &keyLen); err != nil {
		return nil, err
	}
	keyBytes := make([]byte, keyLen)
	if _, err := io.ReadFull(r, keyBytes); err != nil {
		return nil, err
	}
	entry.Key = string(keyBytes)

	// Read data
	var dataLen uint32
	if err := binary.Read(r, binary.BigEndian, &dataLen); err != nil {
		return nil, err
	}
	entry.Data = make([]byte, dataLen)
	if _, err := io.ReadFull(r, entry.Data); err != nil {
		return nil, err
	}

	// Read checksum
	if err := binary.Read(r, binary.BigEndian, &entry.Checksum); err != nil {
		return nil, err
	}

	return entry, nil
}

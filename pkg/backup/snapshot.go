// Package backup provides backup and recovery for GibRAM
package backup

import (
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"time"
)

// Snapshot represents a point-in-time snapshot
type Snapshot struct {
	Version     uint32
	Timestamp   int64
	LSN         uint64
	EntityCount uint64
	DataSize    uint64
}

// SnapshotHeader is written at the beginning of snapshot files
type SnapshotHeader struct {
	Magic     [4]byte // "GRAM" (GibRAM magic)
	Version   uint32
	Timestamp int64
	LSN       uint64
	Checksum  uint32
	Flags     uint32
	Reserved  [32]byte
}

// SnapshotWriter writes snapshot data
type SnapshotWriter struct {
	file         *os.File
	gzWriter     *gzip.Writer
	checksum     uint32
	bytesWritten int64
	path         string // Final target path
	tmpPath      string // Temporary write path
}

// NewSnapshotWriter creates a new snapshot writer with atomic write-to-temp pattern
func NewSnapshotWriter(path string, header *SnapshotHeader) (*SnapshotWriter, error) {
	// Write to temporary file first for atomicity
	tmpPath := path + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp snapshot: %w", err)
	}

	// Set magic
	header.Magic = [4]byte{'G', 'R', 'A', 'M'}

	// Write header (uncompressed)
	if err := binary.Write(f, binary.BigEndian, header); err != nil {
		if closeErr := f.Close(); closeErr != nil {
			_ = os.Remove(tmpPath)
			return nil, fmt.Errorf("write snapshot header failed: %v (close failed: %v)", err, closeErr)
		}
		if rmErr := os.Remove(tmpPath); rmErr != nil && !os.IsNotExist(rmErr) {
			return nil, fmt.Errorf("write snapshot header failed: %v (cleanup failed: %v)", err, rmErr)
		}
		return nil, err
	}

	// Create gzip writer for remaining data
	gz := gzip.NewWriter(f)

	return &SnapshotWriter{
		file:     f,
		gzWriter: gz,
		path:     path,
		tmpPath:  tmpPath,
	}, nil
}

// Write writes data to snapshot
func (w *SnapshotWriter) Write(data []byte) (int, error) {
	n, err := w.gzWriter.Write(data)
	if err != nil {
		return n, err
	}

	w.bytesWritten += int64(n)
	w.checksum = crc32.Update(w.checksum, crc32.IEEETable, data)
	return n, nil
}

// WriteSection writes a named section to snapshot
func (w *SnapshotWriter) WriteSection(name string, data []byte) error {
	// Section format: [4 name_len][name][8 data_len][data]
	nameBytes := []byte(name)

	if err := binary.Write(w.gzWriter, binary.BigEndian, uint32(len(nameBytes))); err != nil {
		return err
	}
	if _, err := w.gzWriter.Write(nameBytes); err != nil {
		return err
	}
	if err := binary.Write(w.gzWriter, binary.BigEndian, uint64(len(data))); err != nil {
		return err
	}
	if _, err := w.gzWriter.Write(data); err != nil {
		return err
	}

	w.bytesWritten += int64(4 + len(nameBytes) + 8 + len(data))
	return nil
}

// Close closes the snapshot writer and atomically renames temp file to final path
func (w *SnapshotWriter) Close() error {
	if err := w.gzWriter.Close(); err != nil {
		if closeErr := w.file.Close(); closeErr != nil {
			_ = os.Remove(w.tmpPath)
			return fmt.Errorf("gzip close failed: %v (file close failed: %v)", err, closeErr)
		}
		if rmErr := os.Remove(w.tmpPath); rmErr != nil && !os.IsNotExist(rmErr) {
			return fmt.Errorf("gzip close failed: %v (cleanup failed: %v)", err, rmErr)
		}
		return err
	}
	if err := w.file.Close(); err != nil {
		if rmErr := os.Remove(w.tmpPath); rmErr != nil && !os.IsNotExist(rmErr) {
			return fmt.Errorf("file close failed: %v (cleanup failed: %v)", err, rmErr)
		}
		return err
	}

	// Atomically rename tmp file to final path
	if err := os.Rename(w.tmpPath, w.path); err != nil {
		if rmErr := os.Remove(w.tmpPath); rmErr != nil && !os.IsNotExist(rmErr) {
			return fmt.Errorf("failed to rename snapshot: %v (cleanup failed: %v)", err, rmErr)
		}
		return fmt.Errorf("failed to rename snapshot: %w", err)
	}

	return nil
}

// BytesWritten returns total bytes written
func (w *SnapshotWriter) BytesWritten() int64 {
	return w.bytesWritten
}

// SnapshotReader reads snapshot data
type SnapshotReader struct {
	file     *os.File
	gzReader *gzip.Reader
	header   *SnapshotHeader
}

// NewSnapshotReader opens a snapshot for reading
func NewSnapshotReader(path string) (*SnapshotReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	// Read header
	header := &SnapshotHeader{}
	if err := binary.Read(f, binary.BigEndian, header); err != nil {
		if closeErr := f.Close(); closeErr != nil {
			return nil, fmt.Errorf("read snapshot header failed: %v (close failed: %v)", err, closeErr)
		}
		return nil, err
	}

	// Verify magic
	if header.Magic != [4]byte{'G', 'R', 'A', 'M'} {
		if closeErr := f.Close(); closeErr != nil {
			return nil, fmt.Errorf("invalid snapshot magic (close failed: %v)", closeErr)
		}
		return nil, fmt.Errorf("invalid snapshot magic")
	}

	// Create gzip reader
	gz, err := gzip.NewReader(f)
	if err != nil {
		if closeErr := f.Close(); closeErr != nil {
			return nil, fmt.Errorf("create gzip reader failed: %v (close failed: %v)", err, closeErr)
		}
		return nil, err
	}

	return &SnapshotReader{
		file:     f,
		gzReader: gz,
		header:   header,
	}, nil
}

// Header returns the snapshot header
func (r *SnapshotReader) Header() *SnapshotHeader {
	return r.header
}

// ReadSection reads a section from snapshot
func (r *SnapshotReader) ReadSection() (name string, data []byte, err error) {
	// Read name length
	var nameLen uint32
	if err := binary.Read(r.gzReader, binary.BigEndian, &nameLen); err != nil {
		return "", nil, err
	}

	// Read name
	nameBytes := make([]byte, nameLen)
	if _, err := io.ReadFull(r.gzReader, nameBytes); err != nil {
		return "", nil, err
	}

	// Read data length
	var dataLen uint64
	if err := binary.Read(r.gzReader, binary.BigEndian, &dataLen); err != nil {
		return "", nil, err
	}

	// Read data
	data = make([]byte, dataLen)
	if _, err := io.ReadFull(r.gzReader, data); err != nil {
		return "", nil, err
	}

	return string(nameBytes), data, nil
}

// Close closes the snapshot reader
func (r *SnapshotReader) Close() error {
	var closeErr error
	if r.gzReader != nil {
		if err := r.gzReader.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	if r.file != nil {
		if err := r.file.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}

// CreateSnapshot creates a snapshot file
func CreateSnapshot(path string, lsn uint64, writeFunc func(w *SnapshotWriter) error) error {
	header := &SnapshotHeader{
		Version:   1,
		Timestamp: time.Now().Unix(),
		LSN:       lsn,
	}

	writer, err := NewSnapshotWriter(path, header)
	if err != nil {
		return err
	}

	if err := writeFunc(writer); err != nil {
		if closeErr := writer.Close(); closeErr != nil {
			_ = os.Remove(path)
			return fmt.Errorf("snapshot write failed: %v (close failed: %v)", err, closeErr)
		}
		if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
			return fmt.Errorf("snapshot write failed: %v (cleanup failed: %v)", err, rmErr)
		}
		return err
	}

	return writer.Close()
}

// RestoreSnapshot restores from a snapshot file
func RestoreSnapshot(path string, readFunc func(r *SnapshotReader) error) error {
	reader, err := NewSnapshotReader(path)
	if err != nil {
		return err
	}
	if err := readFunc(reader); err != nil {
		if closeErr := reader.Close(); closeErr != nil {
			return fmt.Errorf("read snapshot failed: %v (close failed: %v)", err, closeErr)
		}
		return err
	}
	if err := reader.Close(); err != nil {
		return err
	}
	return nil
}

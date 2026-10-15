package agentuse

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/klauspost/compress/snappy"
)

// leveldbLookup reads one key from a Chrome leveldb directory (the older
// localStorage store) without opening it: Chrome holds the lock while it runs.
// It reads the write-ahead logs and the sorted tables and returns the newest
// value, or found=false when the key is absent or deleted.
func leveldbLookup(dir string, key []byte) (value []byte, found bool, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, false, err
	}
	var best struct {
		seq     uint64
		deleted bool
		value   []byte
		seen    bool
	}
	consider := func(seq uint64, deleted bool, value []byte) {
		if best.seen && seq < best.seq {
			return
		}
		best.seq, best.deleted, best.value, best.seen = seq, deleted, value, true
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	var firstErr error
	for _, name := range names {
		path := filepath.Join(dir, name)
		var readErr error
		switch {
		case strings.HasSuffix(name, ".log"):
			readErr = scanLevelDBLog(path, key, consider)
		case strings.HasSuffix(name, ".ldb") || strings.HasSuffix(name, ".sst"):
			readErr = scanLevelDBTable(path, key, consider)
		}
		if readErr != nil && firstErr == nil {
			firstErr = fmt.Errorf("%s: %w", name, readErr)
		}
	}
	if !best.seen {
		return nil, false, firstErr
	}
	if best.deleted {
		return nil, false, nil
	}
	return best.value, true, nil
}

const (
	logBlockSize  = 32 << 10
	logHeaderSize = 7
	recordFull    = 1
	recordFirst   = 2
	recordMiddle  = 3
	recordLast    = 4
)

// scanLevelDBLog walks the log's write batches for puts and deletes of key.
func scanLevelDBLog(path string, key []byte, consider func(uint64, bool, []byte)) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var batch []byte
	for block := 0; block < len(raw); block += logBlockSize {
		end := min(block+logBlockSize, len(raw))
		for offset := block; offset+logHeaderSize <= end; {
			length := int(binary.LittleEndian.Uint16(raw[offset+4:]))
			kind := raw[offset+6]
			start := offset + logHeaderSize
			if kind == 0 && length == 0 {
				break // zero padding at the end of a block
			}
			if start+length > end {
				return errors.New("truncated log record")
			}
			payload := raw[start : start+length]
			offset = start + length
			switch kind {
			case recordFull:
				scanWriteBatch(payload, key, consider)
				batch = nil
			case recordFirst:
				batch = append(batch[:0], payload...)
			case recordMiddle:
				batch = append(batch, payload...)
			case recordLast:
				batch = append(batch, payload...)
				scanWriteBatch(batch, key, consider)
				batch = nil
			}
		}
	}
	return nil
}

func scanWriteBatch(batch []byte, key []byte, consider func(uint64, bool, []byte)) {
	if len(batch) < 12 {
		return
	}
	seq := binary.LittleEndian.Uint64(batch)
	count := binary.LittleEndian.Uint32(batch[8:])
	rest := batch[12:]
	for i := uint32(0); i < count && len(rest) > 0; i++ {
		tag := rest[0]
		rest = rest[1:]
		k, n := lengthPrefixed(rest)
		if n <= 0 {
			return
		}
		rest = rest[n:]
		switch tag {
		case 1:
			v, m := lengthPrefixed(rest)
			if m <= 0 {
				return
			}
			rest = rest[m:]
			if bytes.Equal(k, key) {
				consider(seq+uint64(i), false, append([]byte(nil), v...))
			}
		case 0:
			if bytes.Equal(k, key) {
				consider(seq+uint64(i), true, nil)
			}
		default:
			return
		}
	}
}

func lengthPrefixed(raw []byte) ([]byte, int) {
	length, n := binary.Uvarint(raw)
	if n <= 0 || uint64(len(raw)-n) < length {
		return nil, 0
	}
	return raw[n : n+int(length)], n + int(length)
}

const tableMagic = 0xdb4775248b80fb57

// scanLevelDBTable reads every data block of a sorted table and reports
// entries whose user key equals key. Tables are small for localStorage, so
// a full scan is simpler than an index seek and tolerates any comparator.
func scanLevelDBTable(path string, key []byte, consider func(uint64, bool, []byte)) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(raw) < 48 || binary.LittleEndian.Uint64(raw[len(raw)-8:]) != tableMagic {
		return errors.New("not a leveldb table")
	}
	footer := raw[len(raw)-48:]
	_, n1 := blockHandle(footer) // metaindex
	if n1 <= 0 {
		return errors.New("bad table footer")
	}
	index, n2 := blockHandle(footer[n1:])
	if n2 <= 0 {
		return errors.New("bad table footer")
	}
	indexBlock, err := readTableBlock(raw, index)
	if err != nil {
		return err
	}
	return walkBlock(indexBlock, func(_, value []byte) error {
		handle, n := blockHandle(value)
		if n <= 0 {
			return errors.New("bad index entry")
		}
		data, err := readTableBlock(raw, handle)
		if err != nil {
			return err
		}
		return walkBlock(data, func(internalKey, value []byte) error {
			if len(internalKey) < 8 {
				return nil
			}
			userKey := internalKey[:len(internalKey)-8]
			if !bytes.Equal(userKey, key) {
				return nil
			}
			trailer := binary.LittleEndian.Uint64(internalKey[len(internalKey)-8:])
			consider(trailer>>8, trailer&0xff == 0, append([]byte(nil), value...))
			return nil
		})
	})
}

type tableHandle struct{ offset, size uint64 }

func blockHandle(raw []byte) (tableHandle, int) {
	offset, n := binary.Uvarint(raw)
	if n <= 0 {
		return tableHandle{}, 0
	}
	size, m := binary.Uvarint(raw[n:])
	if m <= 0 {
		return tableHandle{}, 0
	}
	return tableHandle{offset, size}, n + m
}

func readTableBlock(raw []byte, handle tableHandle) ([]byte, error) {
	end := handle.offset + handle.size
	if end+5 > uint64(len(raw)) {
		return nil, errors.New("block out of range")
	}
	block := raw[handle.offset:end]
	switch raw[end] {
	case 0:
		return block, nil
	case 1:
		return snappy.Decode(nil, block)
	default:
		return nil, fmt.Errorf("unsupported block compression %d", raw[end])
	}
}

// walkBlock visits each prefix-compressed entry of a block in order.
func walkBlock(block []byte, visit func(key, value []byte) error) error {
	if len(block) < 4 {
		return errors.New("short block")
	}
	restarts := int(binary.LittleEndian.Uint32(block[len(block)-4:]))
	limit := len(block) - 4 - 4*restarts
	if restarts < 0 || limit < 0 {
		return errors.New("bad block restarts")
	}
	var key []byte
	for offset := 0; offset < limit; {
		shared, a := binary.Uvarint(block[offset:])
		unshared, b := binary.Uvarint(block[offset+max(a, 0):])
		valueLength, c := binary.Uvarint(block[offset+max(a, 0)+max(b, 0):])
		if a <= 0 || b <= 0 || c <= 0 || shared > uint64(len(key)) {
			return errors.New("bad block entry")
		}
		offset += a + b + c
		if uint64(limit-offset) < unshared+valueLength {
			return errors.New("bad block entry")
		}
		key = append(key[:shared], block[offset:offset+int(unshared)]...)
		offset += int(unshared)
		value := block[offset : offset+int(valueLength)]
		offset += int(valueLength)
		if err := visit(key, value); err != nil {
			return err
		}
	}
	return nil
}

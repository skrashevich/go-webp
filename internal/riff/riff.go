// Package riff implements RIFF/WEBP container parsing and writing.
//
// WebP files use the RIFF container format:
//   - 4 bytes: "RIFF"
//   - 4 bytes: file size - 8 (little-endian uint32)
//   - 4 bytes: "WEBP" (form type)
//   - followed by chunks
//
// Each chunk:
//   - 4 bytes: FourCC chunk ID
//   - 4 bytes: chunk data size (little-endian uint32)
//   - N bytes: chunk data (padded to even size)
package riff

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// FourCC represents a 4-byte chunk identifier.
type FourCC [4]byte

func (f FourCC) String() string { return string(f[:]) }

// Known FourCC constants.
var (
	FourCCRIFF = FourCC{'R', 'I', 'F', 'F'}
	FourCCWEBP = FourCC{'W', 'E', 'B', 'P'}
	FourCCVP8  = FourCC{'V', 'P', '8', ' '}
	FourCCVP8L = FourCC{'V', 'P', '8', 'L'}
	FourCCVP8X = FourCC{'V', 'P', '8', 'X'}
	FourCCALPH = FourCC{'A', 'L', 'P', 'H'}
	FourCCANIM = FourCC{'A', 'N', 'I', 'M'}
	FourCCANMF = FourCC{'A', 'N', 'M', 'F'}
)

// Chunk represents a single RIFF chunk.
type Chunk struct {
	ID   FourCC
	Data []byte
}

// ChunkType indicates the WebP encoding type found in the container.
type ChunkType int

const (
	ChunkVP8  ChunkType = iota // Lossy VP8
	ChunkVP8L                  // Lossless VP8L
	ChunkVP8X                  // Extended format
)

// Header holds parsed RIFF/WEBP header information.
type Header struct {
	FileSize uint32 // Total RIFF file size (including "WEBP" form type)
}

// ErrInvalidRIFF is returned when the data is not a valid RIFF/WEBP file.
var ErrInvalidRIFF = errors.New("riff: invalid RIFF/WEBP header")

// ReadHeader reads and validates the 12-byte RIFF/WEBP header from r.
func ReadHeader(r io.Reader) (*Header, error) {
	var buf [12]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return nil, fmt.Errorf("riff: reading header: %w", err)
	}
	if FourCC(buf[0:4]) != FourCCRIFF {
		return nil, ErrInvalidRIFF
	}
	if FourCC(buf[8:12]) != FourCCWEBP {
		return nil, ErrInvalidRIFF
	}
	size := binary.LittleEndian.Uint32(buf[4:8])
	return &Header{FileSize: size}, nil
}

// ReadChunk reads the next chunk header and data from r.
func ReadChunk(r io.Reader) (*Chunk, error) {
	var hdr [8]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	id := FourCC(hdr[0:4])
	size := binary.LittleEndian.Uint32(hdr[4:8])

	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, fmt.Errorf("riff: reading chunk %s data: %w", id, err)
	}

	// Chunks are padded to even size; consume the padding byte if needed.
	if size%2 != 0 {
		var pad [1]byte
		if _, err := io.ReadFull(r, pad[:]); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("riff: reading chunk %s padding: %w", id, err)
		}
	}

	return &Chunk{ID: id, Data: data}, nil
}

// ReadAllChunks reads all remaining chunks from r after the RIFF/WEBP header.
func ReadAllChunks(r io.Reader) ([]*Chunk, error) {
	var chunks []*Chunk
	for {
		chunk, err := ReadChunk(r)
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	return chunks, nil
}

// WriteHeader writes the 12-byte RIFF/WEBP header to w.
// fileSize is the total size of the RIFF file minus 8 bytes (the "RIFF" tag and size field).
func WriteHeader(w io.Writer, fileSize uint32) error {
	var buf [12]byte
	copy(buf[0:4], FourCCRIFF[:])
	binary.LittleEndian.PutUint32(buf[4:8], fileSize)
	copy(buf[8:12], FourCCWEBP[:])
	_, err := w.Write(buf[:])
	return err
}

// WriteChunk writes a chunk with the given FourCC and data to w.
// Adds a padding byte if data length is odd.
func WriteChunk(w io.Writer, id FourCC, data []byte) error {
	var hdr [8]byte
	copy(hdr[0:4], id[:])
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(len(data)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	// Pad to even size.
	if len(data)%2 != 0 {
		if _, err := w.Write([]byte{0}); err != nil {
			return err
		}
	}
	return nil
}

// ChunkSize returns the total on-disk size of a chunk with the given data length.
// 8 bytes header + data + optional padding byte.
func ChunkSize(dataLen int) int {
	size := 8 + dataLen
	if dataLen%2 != 0 {
		size++
	}
	return size
}

// VP8XFlags are bit flags for the VP8X chunk.
type VP8XFlags uint32

const (
	VP8XFlagICC       VP8XFlags = 1 << 5
	VP8XFlagAlpha     VP8XFlags = 1 << 4
	VP8XFlagExif      VP8XFlags = 1 << 3
	VP8XFlagXMP       VP8XFlags = 1 << 2
	VP8XFlagAnimation VP8XFlags = 1 << 1
)

// VP8XChunk holds data from the VP8X (extended) chunk.
type VP8XChunk struct {
	Flags  VP8XFlags
	Width  uint32 // canvas width - 1
	Height uint32 // canvas height - 1
}

// ParseVP8X parses the VP8X chunk data (must be 10 bytes).
func ParseVP8X(data []byte) (*VP8XChunk, error) {
	if len(data) < 10 {
		return nil, errors.New("riff: VP8X chunk too short")
	}
	flags := VP8XFlags(binary.LittleEndian.Uint32(data[0:4]))
	// Width and height are stored as 3-byte little-endian values.
	width := uint32(data[4]) | uint32(data[5])<<8 | uint32(data[6])<<16
	height := uint32(data[7]) | uint32(data[8])<<8 | uint32(data[9])<<16
	return &VP8XChunk{Flags: flags, Width: width, Height: height}, nil
}

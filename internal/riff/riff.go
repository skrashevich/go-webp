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
	FourCCICCP = FourCC{'I', 'C', 'C', 'P'}
	FourCCEXIF = FourCC{'E', 'X', 'I', 'F'}
	FourCCXMP  = FourCC{'X', 'M', 'P', ' '}
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
	width := getUint24LE(data[4:7])
	height := getUint24LE(data[7:10])
	return &VP8XChunk{Flags: flags, Width: width, Height: height}, nil
}

// Encode serialises the VP8XChunk into its 10-byte on-disk representation.
func (v *VP8XChunk) Encode() []byte {
	b := make([]byte, 10)
	binary.LittleEndian.PutUint32(b[0:4], uint32(v.Flags))
	putUint24LE(b[4:7], v.Width)
	putUint24LE(b[7:10], v.Height)
	return b
}

// WriteVP8X writes a complete VP8X chunk to w.
// width and height are the canvas dimensions (not minus one).
func WriteVP8X(w io.Writer, flags VP8XFlags, width, height int) error {
	chunk := &VP8XChunk{
		Flags:  flags,
		Width:  uint32(width - 1),
		Height: uint32(height - 1),
	}
	return WriteChunk(w, FourCCVP8X, chunk.Encode())
}

// --- ALPH chunk header ---

// ALPHHeader holds the decoded flags from the first byte of an ALPH chunk.
type ALPHHeader struct {
	// Method: 0=no compression, 1=lossless VP8L
	Method int
	// Filter: 0=none, 1=horizontal, 2=vertical, 3=gradient
	Filter int
	// Preprocessing: 0=none, 1=level reduction
	Preprocessing int
}

// ParseALPHHeader decodes the single flags byte at the start of an ALPH chunk.
func ParseALPHHeader(data byte) ALPHHeader {
	return ALPHHeader{
		Method:        int(data & 0x03),
		Filter:        int((data >> 2) & 0x03),
		Preprocessing: int((data >> 4) & 0x03),
	}
}

// Encode encodes the ALPHHeader back into a single byte.
func (h ALPHHeader) Encode() byte {
	return byte(h.Method&0x03) | byte((h.Filter&0x03)<<2) | byte((h.Preprocessing&0x03)<<4)
}

// --- ANIM chunk ---

// ANIMChunk holds the data from an ANIM chunk.
type ANIMChunk struct {
	// BackgroundColor is the background colour in BGRA byte order packed as uint32 LE.
	BackgroundColor uint32
	// LoopCount is the number of times to loop the animation; 0 means infinite.
	LoopCount uint16
}

// ParseANIM parses a 6-byte ANIM chunk payload.
func ParseANIM(data []byte) (ANIMChunk, error) {
	if len(data) < 6 {
		return ANIMChunk{}, errors.New("riff: ANIM chunk too short")
	}
	return ANIMChunk{
		BackgroundColor: binary.LittleEndian.Uint32(data[0:4]),
		LoopCount:       binary.LittleEndian.Uint16(data[4:6]),
	}, nil
}

// Encode serialises the ANIMChunk into its 6-byte on-disk representation.
func (a ANIMChunk) Encode() []byte {
	b := make([]byte, 6)
	binary.LittleEndian.PutUint32(b[0:4], a.BackgroundColor)
	binary.LittleEndian.PutUint16(b[4:6], a.LoopCount)
	return b
}

// --- ANMF chunk header ---

// ANMFHeader holds the 16-byte header of an ANMF chunk.
type ANMFHeader struct {
	// X and Y are the frame position in pixels (already multiplied by 2).
	X, Y int
	// Width and Height are the frame dimensions in pixels (already +1).
	Width, Height int
	// Duration is the frame display duration in milliseconds.
	Duration int
	// Dispose indicates the frame should be cleared to background colour after display.
	Dispose bool
	// Blend indicates that no alpha blending should be performed (no_blend flag).
	Blend bool
}

// ParseANMF parses an ANMF chunk payload, returning the header and the
// remaining frame bitstream bytes.
func ParseANMF(data []byte) (ANMFHeader, []byte, error) {
	if len(data) < 16 {
		return ANMFHeader{}, nil, errors.New("riff: ANMF chunk too short")
	}
	h := ANMFHeader{
		X:        int(getUint24LE(data[0:3])) * 2,
		Y:        int(getUint24LE(data[3:6])) * 2,
		Width:    int(getUint24LE(data[6:9])) + 1,
		Height:   int(getUint24LE(data[9:12])) + 1,
		Duration: int(getUint24LE(data[12:15])),
		Dispose:  data[15]&0x01 != 0,
		Blend:    data[15]&0x02 != 0,
	}
	return h, data[16:], nil
}

// Encode serialises the ANMFHeader into its 16-byte on-disk representation.
func (h ANMFHeader) Encode() []byte {
	b := make([]byte, 16)
	putUint24LE(b[0:3], uint32(h.X/2))
	putUint24LE(b[3:6], uint32(h.Y/2))
	putUint24LE(b[6:9], uint32(h.Width-1))
	putUint24LE(b[9:12], uint32(h.Height-1))
	putUint24LE(b[12:15], uint32(h.Duration))
	var flags byte
	if h.Dispose {
		flags |= 0x01
	}
	if h.Blend {
		flags |= 0x02
	}
	b[15] = flags
	return b
}

// --- uint24 helpers ---

func getUint24LE(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16
}

func putUint24LE(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
}

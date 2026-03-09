// Command webpconv converts images to and from WebP format.
//
// Usage:
//
//	webpconv -o output.webp input.png
//	webpconv -o output.png input.webp
//	webpconv -lossy -quality 80 -o output.webp input.png
package main

import (
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	// Register WebP format with image package.
	"github.com/skrashevich/go-webp"
	_ "github.com/skrashevich/go-webp"
)

func main() {
	input, output, lossy, quality, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		printUsage(os.Stderr)
		os.Exit(1)
	}
	if err := convert(input, output, lossy, quality); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func parseArgs(args []string) (input, output string, lossy bool, quality float32, err error) {
	quality = 75
	endOfFlags := false

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !endOfFlags && arg == "--" {
			endOfFlags = true
			continue
		}
		if !endOfFlags && strings.HasPrefix(arg, "-") {
			switch {
			case arg == "-h" || arg == "--help":
				return "", "", false, 0, fmt.Errorf("help requested")
			case arg == "-o":
				i++
				if i >= len(args) {
					return "", "", false, 0, fmt.Errorf("flag needs an argument: -o")
				}
				output = args[i]
			case strings.HasPrefix(arg, "-o="):
				output = strings.TrimPrefix(arg, "-o=")
			case arg == "-lossy":
				lossy = true
			case strings.HasPrefix(arg, "-lossy="):
				v := strings.TrimPrefix(arg, "-lossy=")
				parsed, parseErr := strconv.ParseBool(v)
				if parseErr != nil {
					return "", "", false, 0, fmt.Errorf("invalid boolean value for -lossy: %q", v)
				}
				lossy = parsed
			case arg == "-quality":
				i++
				if i >= len(args) {
					return "", "", false, 0, fmt.Errorf("flag needs an argument: -quality")
				}
				parsed, parseErr := strconv.ParseFloat(args[i], 32)
				if parseErr != nil {
					return "", "", false, 0, fmt.Errorf("invalid value for -quality: %q", args[i])
				}
				quality = float32(parsed)
			case strings.HasPrefix(arg, "-quality="):
				v := strings.TrimPrefix(arg, "-quality=")
				parsed, parseErr := strconv.ParseFloat(v, 32)
				if parseErr != nil {
					return "", "", false, 0, fmt.Errorf("invalid value for -quality: %q", v)
				}
				quality = float32(parsed)
			default:
				return "", "", false, 0, fmt.Errorf("unknown flag: %s", arg)
			}
			continue
		}

		if input != "" {
			return "", "", false, 0, fmt.Errorf("expected exactly one input file")
		}
		input = arg
	}

	if input == "" {
		return "", "", false, 0, fmt.Errorf("input file is required")
	}
	if output == "" {
		return "", "", false, 0, fmt.Errorf("-o output file is required")
	}
	return input, output, lossy, quality, nil
}

func printUsage(w *os.File) {
	fmt.Fprintln(w, "usage: webpconv [flags] input")
	fmt.Fprintln(w, "  -o string")
	fmt.Fprintln(w, "        output file path (required)")
	fmt.Fprintln(w, "  -lossy")
	fmt.Fprintln(w, "        use lossy VP8 encoding (default: lossless VP8L)")
	fmt.Fprintln(w, "  -quality float")
	fmt.Fprintln(w, "        encoding quality for lossy mode (0-100) (default 75)")
}

func convert(inputPath, outputPath string, lossy bool, quality float32) error {
	img, err := decodeImage(inputPath)
	if err != nil {
		return fmt.Errorf("decoding input: %w", err)
	}

	outExt := strings.ToLower(filepath.Ext(outputPath))
	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("creating output file: %w", err)
	}
	defer f.Close()

	switch outExt {
	case ".webp":
		opts := &webp.Options{Lossy: lossy, Quality: quality}
		return webp.Encode(f, img, opts)
	case ".png":
		return png.Encode(f, img)
	case ".jpg", ".jpeg":
		return jpeg.Encode(f, img, &jpeg.Options{Quality: int(quality)})
	default:
		return fmt.Errorf("unsupported output format: %s", outExt)
	}
}

func decodeImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	return img, err
}

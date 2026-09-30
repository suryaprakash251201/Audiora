package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	// Imported for its side effect: ffmpeg always emits PNG, and image.Decode
	// can only read a format whose package has been linked in. Without this,
	// every extracted cover fails to decode with "unknown format" and the
	// whole library silently ends up without artwork.
	_ "image/png"
)

// maxCoverDimension caps the stored cover art. A 3000px scan of a booklet is
// several megabytes per album, and no display we ship is wider than ~1000px,
// so anything larger is wasted disk and slower first paint.
const maxCoverDimension = 1000

// jpegQuality for normalised cover art. 85 is visually transparent for album
// art and roughly halves the size versus a lossless re-encode.
const jpegQuality = 85

// Extractor writes cover images and dominant colours out of a media cache
// directory. Both are keyed by a stable hash, so re-scanning a library
// reuses the artwork already on disk instead of re-running ffmpeg.
type Extractor struct {
	cacheDir     string
	extractArt   bool
	extractColor bool
}

func NewExtractor(cacheDir string, extractArt, extractColor bool) *Extractor {
	return &Extractor{cacheDir: cacheDir, extractArt: extractArt, extractColor: extractColor}
}

// CoverResult describes the artwork found for an album.
type CoverResult struct {
	// FileName is relative to the Extractor's cache directory, or empty when
	// no artwork was found.
	FileName string
	// Color is a #rrggbb accent colour, or empty when not computed.
	Color string
}

// FindSidecarCover looks for cover art sitting next to the audio files, which
// is how a lot of tag-ripped libraries are organised: cover.jpg, folder.png
// and AlbumArt variants next to the tracks.
func FindSidecarCover(albumDir string) (string, bool) {
	names := []string{
		"cover.jpg", "cover.jpeg", "cover.png", "cover.webp",
		"folder.jpg", "folder.jpeg", "folder.png",
		"albumart.jpg", "albumart.png", "album.jpg", "front.jpg",
		"Cover.jpg", "Folder.jpg",
	}
	for _, name := range names {
		p := filepath.Join(albumDir, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Size() > 0 {
			return p, true
		}
	}
	return "", false
}

// Extract writes cover art for an audio file (or a sidecar image) into the
// cache and derives an accent colour from it.
//
// key identifies the album; it only needs to be stable across scans. Passing
// an empty key is a programming error and yields no result.
func (e *Extractor) Extract(ctx context.Context, key, audioPath, sidecarPath string) (*CoverResult, error) {
	if key == "" {
		return nil, fmt.Errorf("cover key must not be empty")
	}
	name := hashKey(key) + ".jpg"
	dest := filepath.Join(e.cacheDir, name)

	// Fast path: already extracted on an earlier scan.
	if _, err := os.Stat(dest); err == nil {
		return &CoverResult{FileName: name, Color: e.cachedColor(key, dest)}, nil
	}

	source := audioPath
	if sidecarPath != "" {
		source = sidecarPath
	} else if !e.extractArt {
		return &CoverResult{}, nil
	}

	if err := os.MkdirAll(e.cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("create cover dir: %w", err)
	}

	png, err := extractCoverPNG(ctx, source, sidecarPath == "")
	if err != nil {
		// A file with no artwork at all is the common case and not worth
		// reporting, but a real extraction failure is: it is silently
		// invisible in the UI, which just shows a placeholder.
		if !isNoArtwork(err) {
			slog.Warn("cover art extraction failed", "source", filepath.Base(source), "err", err)
		}
		return &CoverResult{}, nil
	}

	// Normalise to JPEG ourselves rather than trusting the source extension:
	// the same embedded image is a PNG inside a FLAC and a JPEG inside an
	// ID3v2 tag, and storing both would mean guessing the content type.
	//
	// ffmpeg always emits PNG here, so image/png must be imported for its
	// decoder to be registered. Importing image/jpeg below is not enough: it
	// registers JPEG, and an unregistered format makes image.Decode fail.
	img, _, err := image.Decode(bytes.NewReader(png))
	if err != nil {
		slog.Warn("extracted cover art could not be decoded", "source", filepath.Base(source), "err", err)
		return &CoverResult{}, nil
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		slog.Warn("cover art could not be re-encoded as JPEG", "err", err)
		return &CoverResult{}, nil
	}

	// Write to a temp file then rename, so a concurrent request for this
	// cover never observes a half-written image.
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return nil, fmt.Errorf("write cover: %w", err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return nil, fmt.Errorf("commit cover: %w", err)
	}

	result := &CoverResult{FileName: name}
	if e.extractColor {
		if c, err := dominantColor(dest); err == nil {
			result.Color = c
			// Stash it beside the image so the fast path above can return
			// the colour without re-reading and re-analysing the JPEG.
			_ = os.WriteFile(dest+".color", []byte(c), 0o644)
		}
	}
	return result, nil
}

// cachedColor reads a previously computed accent colour.
func (e *Extractor) cachedColor(key, dest string) string {
	b, err := os.ReadFile(dest + ".color")
	if err != nil {
		return ""
	}
	return string(bytes.TrimSpace(b))
}

// extractCoverPNG pulls the attached picture out of a media file, or reads a
// sidecar image, always as PNG and always downscaled to maxCoverDimension.
func extractCoverPNG(ctx context.Context, source string, fromAudio bool) ([]byte, error) {
	args := []string{"-v", "error", "-i", source}
	if fromAudio {
		// -an drops audio streams; -map 0:v:0 picks the attached picture.
		args = append(args, "-an", "-map", "0:v:0")
	} else {
		// A sidecar may be a single image, or a folder.jpg that is really
		// one image; mapping v:0 is wrong when there is no video stream, so
		// only take the first input stream.
		args = append(args, "-map", "0")
	}
	args = append(args,
		"-vf", fmt.Sprintf("scale='min(%d,iw)':-2:flags=lanczos", maxCoverDimension),
		"-frames:v", "1",
		"-f", "image2pipe", "-vcodec", "png", "pipe:1",
	)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg cover: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if stdout.Len() == 0 {
		return nil, fmt.Errorf("no image produced from %s", source)
	}
	return stdout.Bytes(), nil
}

// dominantColor picks an accent colour from an image by downsampling to a
// 4x4 grid and choosing the most colourful pixel.
//
// Averaging all 16 pixels (what "scale to 1x1" does) reliably produces mud,
// because a cover is mostly dark background. Scoring for saturation instead
// finds the vivid part of the artwork, which is what the UI needs for
// highlights and button accents.
func dominantColor(imagePath string) (string, error) {
	cmd := exec.Command("ffmpeg",
		"-v", "error", "-i", imagePath,
		"-vf", "scale=4:4:flags=bilinear",
		"-f", "rawvideo", "-pix_fmt", "rgb24", "pipe:1")

	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ffmpeg colour: %w", err)
	}
	const gridSide = 4
	const bytesPerPixel = 3
	if out.Len() < gridSide*gridSide*bytesPerPixel {
		return "", fmt.Errorf("unexpected sample size %d", out.Len())
	}

	pixels := out.Bytes()[:gridSide*gridSide*bytesPerPixel]
	best := color.RGBA{}
	bestScore := math.Inf(-1)

	for i := 0; i+bytesPerPixel <= len(pixels); i += bytesPerPixel {
		r, g, b := float64(pixels[i]), float64(pixels[i+1]), float64(pixels[i+2])
		maxC := math.Max(r, math.Max(g, b))
		minC := math.Min(r, math.Min(g, b))
		if maxC == 0 {
			continue // pure black carries no hue information
		}
		saturation := (maxC - minC) / maxC
		brightness := maxC / 255

		// Weight saturation heavily but not exclusively, so a near-grey
		// cover still yields a usable tone instead of a random pixel.
		score := saturation*0.75 + brightness*0.25
		if score > bestScore {
			bestScore = score
			best = color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 255}
		}
	}

	if bestScore == math.Inf(-1) {
		return "", fmt.Errorf("image was entirely black")
	}
	return tuneAccent(best), nil
}

// tuneAccent pulls a sampled colour into a range that works as a UI accent.
// Near-black samples would vanish against a dark background and near-white
// ones would glare, so brightness is compressed into a usable band and
// heavily desaturated colours are given a little chroma.
func tuneAccent(c color.RGBA) string {
	r := float64(c.R) / 255
	g := float64(c.G) / 255
	b := float64(c.B) / 255

	maxC := math.Max(r, math.Max(g, b))
	minC := math.Min(r, math.Min(g, b))
	lightness := (maxC + minC) / 2
	if maxC == minC {
		lightness = maxC
	}

	// Keep accents in a mid-tone band: readable on both glass surfaces.
	if lightness < 0.35 {
		lightness = 0.35 + (0.35-lightness)*0.5
	} else if lightness > 0.72 {
		lightness = 0.72 - (lightness-0.72)*0.4
	}

	saturation := 0.0
	if maxC != minC {
		saturation = (maxC - minC) / (1 - math.Abs(2*lightness-1))
	}
	// Nudge a fully grey cover towards something with visible intent.
	if saturation < 0.12 {
		saturation = 0.18
	}
	if saturation > 0.85 {
		saturation = 0.85
	}

	return rgbToHex(hslToRGB(lightness, saturation, 0.5))
}

func hslToRGB(h, s, l float64) (float64, float64, float64) {
	if s == 0 {
		return l, l, l
	}
	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	return hueToRGB(p, q, h+1.0/3.0), hueToRGB(p, q, h), hueToRGB(p, q, h-1.0/3.0)
}

func hueToRGB(p, q, t float64) float64 {
	if t < 0 {
		t++
	}
	if t > 1 {
		t--
	}
	switch {
	case t < 1.0/6.0:
		return p + (q-p)*6*t
	case t < 1.0/2.0:
		return q
	case t < 2.0/3.0:
		return p + (q-p)*(2.0/3.0-t)*6
	default:
		return p
	}
}

func rgbToHex(r, g, b float64) string {
	to255 := func(v float64) int {
		n := int(math.Round(v * 255))
		if n < 0 {
			n = 0
		}
		if n > 255 {
			n = 255
		}
		return n
	}
	return fmt.Sprintf("#%02x%02x%02x", to255(r), to255(g), to255(b))
}

// isNoArtwork reports whether ffmpeg failed simply because the source has no
// picture in it, which is the normal state of most audio files.
func isNoArtwork(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "does not contain any stream") ||
		strings.Contains(msg, "no such file or directory") ||
		strings.Contains(msg, "no video")
}

func hashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:16])
}

// solidColorPlaceholder is used when an album has no artwork, so the UI has
// a deterministic gradient rather than a blank tile.
func solidColorPlaceholder(seed string) color.RGBA {
	sum := sha256.Sum256([]byte(seed))
	return color.RGBA{R: sum[0], G: sum[1], B: sum[2], A: 255}
}

// PlaceholderColor is the exported form of solidColorPlaceholder, used by
// the API to give the client a colour for artwork-less albums.
func PlaceholderColor(seed string) string {
	c := solidColorPlaceholder(seed)
	return rgbToHex(float64(c.R)/255, float64(c.G)/255, float64(c.B)/255)
}

// DrawFlattened composites any image onto an opaque background. Used when
// normalising transparent PNG artwork to JPEG, which has no alpha channel.
func DrawFlattened(img image.Image, bg color.RGBA) *image.RGBA {
	out := image.NewRGBA(img.Bounds())
	draw.Draw(out, out.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)
	draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Over)
	return out
}

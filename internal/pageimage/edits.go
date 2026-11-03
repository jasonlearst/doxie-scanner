package pageimage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"

	"github.com/disintegration/imaging"
)

// ColorMode is how a page is rendered. The scanner always captures color;
// gray and black & white are derived on the host, like Doxie's own app.
type ColorMode string

const (
	ColorColor ColorMode = "color"
	ColorGray  ColorMode = "gray"
	ColorBW    ColorMode = "bw"
)

// Crop is a rectangle in the rotated page's pixel space: exactly what the
// user sees and drags on. WithRotation carries it across rotations.
type Crop struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// Edits are applied on top of the untouched original every time a page
// is rendered, so any of them can be changed or undone later.
type Edits struct {
	Rotation   int       `json:"rotation,omitempty"`   // clockwise: 0, 90, 180, 270
	Crop       *Crop     `json:"crop,omitempty"`       // nil = whole page
	Brightness int       `json:"brightness,omitempty"` // -100..100
	Contrast   int       `json:"contrast,omitempty"`   // -100..100
	Color      ColorMode `json:"color,omitempty"`      // empty = color
}

// Validate checks the edits against a page of the given decoded size.
func (e Edits) Validate(width, height int) error {
	switch e.Rotation {
	case 0, 90, 180, 270:
	default:
		return fmt.Errorf("rotation must be 0, 90, 180 or 270, got %d", e.Rotation)
	}
	if e.Brightness < -100 || e.Brightness > 100 || e.Contrast < -100 || e.Contrast > 100 {
		return fmt.Errorf("brightness and contrast must be between -100 and 100")
	}
	switch e.Color {
	case "", ColorColor, ColorGray, ColorBW:
	default:
		return fmt.Errorf("color must be color, gray or bw, got %q", e.Color)
	}
	if c := e.Crop; c != nil {
		w, h := rotatedSize(width, height, e.Rotation)
		if c.W <= 0 || c.H <= 0 || !c.rect().In(image.Rect(0, 0, w, h)) {
			return fmt.Errorf("crop %+v is outside the %dx%d page", *c, w, h)
		}
	}
	return nil
}

// WithRotation changes the rotation of a page of the given unrotated size,
// moving any crop so it still covers the same part of the page.
func (e Edits) WithRotation(rotation, width, height int) Edits {
	if e.Crop != nil {
		original := rotateCrop(*e.Crop, width, height, e.Rotation, true)
		moved := rotateCrop(original, width, height, rotation, false)
		e.Crop = &moved
	}
	e.Rotation = rotation
	return e
}

func (c Crop) rect() image.Rectangle { return image.Rect(c.X, c.Y, c.X+c.W, c.Y+c.H) }

func rotatedSize(w, h, rotation int) (int, int) {
	if rotation == 90 || rotation == 270 {
		return h, w
	}
	return w, h
}

// rotateCrop maps a crop between the unrotated frame (w x h) and the frame
// rotated clockwise by rotation; inverse maps rotated back to unrotated.
func rotateCrop(c Crop, w, h, rotation int, inverse bool) Crop {
	if inverse {
		rw, rh := rotatedSize(w, h, rotation)
		return rotateCrop(c, rw, rh, (360-rotation)%360, false)
	}
	x0, y0, x1, y1 := c.X, c.Y, c.X+c.W, c.Y+c.H
	switch rotation {
	case 90:
		x0, y0, x1, y1 = h-y1, x0, h-y0, x1
	case 180:
		x0, y0, x1, y1 = w-x1, h-y1, w-x0, h-y0
	case 270:
		x0, y0, x1, y1 = y0, w-x1, y1, w-x0
	}
	return Crop{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// IsIdentity reports whether rendering would reproduce the original.
func (e Edits) IsIdentity() bool {
	return e.Rotation == 0 && e.Crop == nil && e.Brightness == 0 && e.Contrast == 0 &&
		(e.Color == "" || e.Color == ColorColor)
}

// Render applies edits: crop (mapped back to the unrotated frame, so the
// rotation works on fewer pixels), rotate, tone, then color.
func Render(img image.Image, e Edits) image.Image {
	out := img
	if c := e.Crop; c != nil {
		b := img.Bounds()
		o := rotateCrop(*c, b.Dx(), b.Dy(), e.Rotation, true)
		out = imaging.Crop(out, o.rect().Add(b.Min))
	}
	switch e.Rotation {
	case 90:
		out = imaging.Rotate270(out)
	case 180:
		out = imaging.Rotate180(out)
	case 270:
		out = imaging.Rotate90(out)
	}
	if e.Brightness != 0 {
		out = imaging.AdjustBrightness(out, float64(e.Brightness))
	}
	if e.Contrast != 0 {
		out = imaging.AdjustContrast(out, float64(e.Contrast))
	}
	switch e.Color {
	case ColorGray:
		out = toGray(out)
	case ColorBW:
		out = toBW(out)
	}
	return out
}

func toGray(img image.Image) *image.Gray {
	n := imaging.Grayscale(img)
	g := image.NewGray(n.Rect)
	for i := range g.Pix {
		g.Pix[i] = n.Pix[i*4]
	}
	return g
}

// toBW thresholds at Otsu's level, which adapts to the paper and ink of
// each page instead of a fixed cut-off.
func toBW(img image.Image) *image.Gray {
	g := toGray(img)
	t := otsu(g)
	for i, v := range g.Pix {
		if v > t {
			g.Pix[i] = 0xff
		} else {
			g.Pix[i] = 0
		}
	}
	return g
}

func otsu(g *image.Gray) uint8 {
	var hist [256]int
	for _, v := range g.Pix {
		hist[v]++
	}
	total := len(g.Pix)
	sum := 0
	for i, n := range hist {
		sum += i * n
	}
	var sumB, wB int
	var best float64
	threshold := uint8(127)
	for t := 0; t < 256; t++ {
		wB += hist[t]
		if wB == 0 {
			continue
		}
		wF := total - wB
		if wF == 0 {
			break
		}
		sumB += t * hist[t]
		mB := float64(sumB) / float64(wB)
		mF := float64(sum-sumB) / float64(wF)
		between := float64(wB) * float64(wF) * (mB - mF) * (mB - mF)
		if between > best {
			best = between
			threshold = uint8(t)
		}
	}
	return threshold
}

// Fit scales a rendered page to fit within maxW x maxH for thumbnails and
// previews; images already small enough are returned unchanged.
func Fit(img image.Image, maxW, maxH int) image.Image {
	b := img.Bounds()
	if b.Dx() <= maxW && b.Dy() <= maxH {
		return img
	}
	return imaging.Fit(img, maxW, maxH, imaging.Linear)
}

// CacheKey identifies a rendering of a page with these edits at a given
// size, so cached renders are reused until the edits change.
func CacheKey(e Edits, size string) string {
	b, _ := json.Marshal(e)
	sum := sha256.Sum256(b)
	return size + "-" + hex.EncodeToString(sum[:6])
}

// Preset sizes the web UI asks for, as a bounding box in pixels.
var Sizes = map[string]image.Point{
	"thumb":   {X: 360, Y: 480},
	"preview": {X: 1400, Y: 1800},
}

const viewQuality = 82

// RenderView renders a page with its edits, scaled to a preset size, as
// JPEG for display.
func RenderView(img image.Image, e Edits, size string) ([]byte, error) {
	box, ok := Sizes[size]
	if !ok {
		return nil, fmt.Errorf("pageimage: unknown size %q", size)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, Fit(Render(img, e), box.X, box.Y), &jpeg.Options{Quality: viewQuality}); err != nil {
		return nil, fmt.Errorf("pageimage: encode %s view: %w", size, err)
	}
	return buf.Bytes(), nil
}

package pageimage

import (
	"image"
	"math"
)

// Affine maps points, x right and y down: (x, y) → (A·x + C·y + E, B·x + D·y + F).
type Affine struct{ A, B, C, D, E, F float64 }

// Apply maps one point.
func (m Affine) Apply(x, y float64) (float64, float64) {
	return m.A*x + m.C*y + m.E, m.B*x + m.D*y + m.F
}

// Then is m followed by n.
func (m Affine) Then(n Affine) Affine {
	return Affine{
		A: n.A*m.A + n.C*m.B, B: n.B*m.A + n.D*m.B,
		C: n.A*m.C + n.C*m.D, D: n.B*m.C + n.D*m.D,
		E: n.A*m.E + n.C*m.F + n.E, F: n.B*m.E + n.D*m.F + n.F,
	}
}

// Inverse undoes m.
func (m Affine) Inverse() Affine {
	det := m.A*m.D - m.B*m.C
	a, b, c, d := m.D/det, -m.B/det, -m.C/det, m.A/det
	return Affine{A: a, B: b, C: c, D: d, E: -(a*m.E + c*m.F), F: -(b*m.E + d*m.F)}
}

// Translate moves points by (x, y).
func Translate(x, y float64) Affine { return Affine{A: 1, D: 1, E: x, F: y} }

// Scale stretches points by (x, y).
func Scale(x, y float64) Affine { return Affine{A: x, D: y} }

// NeedsPixels reports whether e changes pixel values (tone or color), as
// opposed to only moving them, which a PDF can do with the original.
func (e Edits) NeedsPixels() bool {
	return e.Brightness != 0 || e.Contrast != 0 || e.Color == ColorGray || e.Color == ColorBW
}

// Geometry maps the original's pixels to where Render puts them for e's
// straighten, crop and rotation, and gives the size of the result.
func Geometry(e Edits, width, height int) (m Affine, outW, outH int) {
	m = Affine{A: 1, D: 1}
	if e.Straighten != 0 {
		sin, cos := math.Sincos(e.Straighten * math.Pi / 180)
		cx, cy := float64(width)/2, float64(height)/2
		m = Translate(-cx, -cy).Then(Affine{A: cos, B: -sin, C: sin, D: cos}).Then(Translate(cx, cy))
	}
	box := Crop{W: width, H: height}
	if e.Crop != nil {
		box = rotateCrop(*e.Crop, width, height, e.Rotation, true)
		m = m.Then(Translate(-float64(box.X), -float64(box.Y)))
	}
	bw, bh := float64(box.W), float64(box.H)
	switch e.Rotation {
	case 90:
		m = m.Then(Affine{B: 1, C: -1, E: bh})
	case 180:
		m = m.Then(Affine{A: -1, D: -1, E: bw, F: bh})
	case 270:
		m = m.Then(Affine{B: -1, C: 1, F: bw})
	}
	outW, outH = rotatedSize(box.W, box.H, e.Rotation)
	return m, outW, outH
}

// Visible is the part of a width x height original that ends up inside the
// result of m, which is outW x outH, padded by a pixel for resampling.
func Visible(m Affine, outW, outH, width, height int) image.Rectangle {
	inv := m.Inverse()
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, c := range [4][2]float64{{0, 0}, {float64(outW), 0}, {0, float64(outH)}, {float64(outW), float64(outH)}} {
		x, y := inv.Apply(c[0], c[1])
		minX, minY, maxX, maxY = math.Min(minX, x), math.Min(minY, y), math.Max(maxX, x), math.Max(maxY, y)
	}
	r := image.Rect(int(math.Floor(minX))-1, int(math.Floor(minY))-1, int(math.Ceil(maxX))+1, int(math.Ceil(maxY))+1)
	return r.Intersect(image.Rect(0, 0, width, height))
}

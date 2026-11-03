//go:build hardware

package doxiedx400

import (
	"context"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/andiapps-dev/doxie-scanner/internal/driver"
)

// TestHardwareScan scans one sheet on a real, connected DX400.
//
//	DX400_RES=300 DX400_DUPLEX=1 DX400_OUT=/tmp/dx400 \
//	  go test -tags hardware -run TestHardwareScan -v ./internal/doxiedx400
func TestHardwareScan(t *testing.T) {
	res, _ := strconv.Atoi(os.Getenv("DX400_RES"))
	duplex := os.Getenv("DX400_DUPLEX") == "1"
	out := os.Getenv("DX400_OUT")
	if out == "" {
		t.Fatal("set DX400_OUT to a directory for the scanned pages")
	}

	sess, err := New().Open(context.Background())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer sess.Close()

	t.Log("waiting for paper…")
	deadline := time.Now().Add(3 * time.Minute)
	for {
		ok, err := sess.HasNextPage(context.Background())
		if err != nil {
			t.Fatalf("paper check: %v", err)
		}
		if ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no paper loaded")
		}
		time.Sleep(300 * time.Millisecond)
	}
	time.Sleep(time.Second)

	start := time.Now()
	page, err := sess.ScanPage(context.Background(), driver.ScanOptions{Duplex: duplex, Resolution: res})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	t.Logf("scanned and decoded in %s, back blank: %v", time.Since(start).Round(100*time.Millisecond), page.BackBlank)

	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, side := range []struct {
		name string
		img  image.Image
	}{{"front", page.Front}, {"back", page.Back}} {
		if side.img == nil {
			continue
		}
		path := filepath.Join(out, side.name+".jpg")
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := jpeg.Encode(f, side.img, &jpeg.Options{Quality: 90}); err != nil {
			t.Fatal(err)
		}
		f.Close()
		t.Logf("%s: %v -> %s", side.name, side.img.Bounds().Size(), path)
	}
}

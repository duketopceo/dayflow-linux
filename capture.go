package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"image"
	_ "image/jpeg"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func paused() bool {
	_, err := os.Stat(pausePath())
	return err == nil
}

func setPaused(p bool) {
	if p {
		os.MkdirAll(dataDir(), 0o700)
		os.WriteFile(pausePath(), []byte("paused\n"), 0o600)
	} else {
		os.Remove(pausePath())
	}
}

// ahash computes a 16x16 grayscale average-hash of the image.
func ahash(img image.Image) uint64 {
	const size = 16
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	var px [size * size]uint32
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			sx := b.Min.X + x*w/size
			sy := b.Min.Y + y*h/size
			r, g, bl, _ := img.At(sx, sy).RGBA()
			px[y*size+x] = (r + g + bl) / 3 >> 8
		}
	}
	var sum uint64
	for _, v := range px {
		sum += uint64(v)
	}
	avg := uint32(sum / (size * size))
	var hash uint64
	for i, v := range px {
		if v >= avg {
			hash |= 1 << i
		}
	}
	return hash
}

func hamming(a, b uint64) int {
	d := a ^ b
	n := 0
	for d != 0 {
		n += int(d & 1)
		d >>= 1
	}
	return n
}

// grabFrame captures the screen via grim and returns JPEG bytes.
func grabFrame(quality int) ([]byte, error) {
	cmd := exec.Command("grim", "-t", "jpeg", "-q", fmt.Sprint(quality), "-")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

const dedupThreshold = 5 // hamming distance out of 256 bits

func captureOnce(db *sql.DB, cfg Config, lastHash *uint64) error {
	if paused() {
		return nil
	}
	raw, err := grabFrame(cfg.JPEGQuality)
	if err != nil {
		return fmt.Errorf("grim: %w", err)
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	h := ahash(img)
	if lastHash != nil && hamming(h, *lastHash) <= dedupThreshold {
		return nil // screen unchanged
	}
	*lastHash = h

	now := time.Now()
	dayDir := filepath.Join(framesDir(), now.Format("2006-01-02"))
	if err := os.MkdirAll(dayDir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dayDir, now.Format("150405")+".jpg")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return err
	}
	return insertFrame(db, now, path)
}

func runDaemon(cfg Config) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	var lastHash uint64
	var haveHash bool
	tick := time.NewTicker(time.Duration(cfg.CaptureIntervalSec) * time.Second)
	defer tick.Stop()
	log.Printf("dayflow daemon: capturing every %ds -> %s", cfg.CaptureIntervalSec, framesDir())

	capture := func() {
		var h *uint64
		if haveHash {
			h = &lastHash
		} else {
			h = new(uint64)
			*h = ^uint64(0) // force first frame to be saved
		}
		if err := captureOnce(db, cfg, h); err != nil {
			log.Printf("capture: %v", err)
			return
		}
		lastHash = *h
		haveHash = true
	}
	capture()
	for range tick.C {
		capture()
	}
	return nil
}

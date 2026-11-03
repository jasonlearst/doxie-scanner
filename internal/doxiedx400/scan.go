package doxiedx400

import (
	"fmt"

	"github.com/andiapps-dev/doxie-scanner/internal/scsiusb"
)

// maxChunksPerSheet bounds the read loop (256 MB) so a scanner that never
// reports end-of-side can't spin forever.
const maxChunksPerSheet = 1024

// scanSheet feeds one sheet and returns a complete JPEG per side (front,
// then rear when duplex). The command sequence mirrors Doxie's driver.
func scanSheet(dev scsiusb.BulkDevice, w window, quant []byte) (sides [][]byte, err error) {
	if err := control(dev, opReserve, flagJob); err != nil {
		return nil, err
	}
	defer func() {
		if endErr := control(dev, opRelease, flagJob); err == nil && endErr != nil {
			sides, err = nil, endErr
		}
	}()

	if err := prepare(dev, w, quant); err != nil {
		return nil, err
	}
	if err := control(dev, opReserve, flagScanUnit); err != nil {
		return nil, err
	}
	raw, lines, readErr := readSides(dev, w.duplex)
	relErr := control(dev, opRelease, flagScanUnit)
	if readErr != nil {
		return nil, readErr
	}
	if relErr != nil {
		return nil, relErr
	}

	for i := range raw {
		sides = append(sides, wrapJPEG(w.width, lines[i], quant, raw[i]))
	}
	return sides, nil
}

func prepare(dev scsiusb.BulkDevice, w window, quant []byte) error {
	if _, err := read(dev, dtScanStatus, scanStatusLen); err != nil {
		return err
	}
	if err := setWindow(dev, w); err != nil {
		return err
	}
	if w.duplex {
		if _, err := read(dev, dtDuplexTransfer, duplexTransferLen); err != nil {
			return err
		}
	}
	for _, dt := range []byte{dtTruncateHead, dtTruncateTail} {
		if err := send(dev, dt, truncateNone); err != nil {
			return err
		}
	}
	if err := send(dev, dtJPEGQuant, quantPayload(quant)); err != nil {
		return err
	}
	if _, err := read(dev, dtCalibrationFmt, calibrationFmtLen); err != nil {
		return err
	}
	for channel := byte(0); channel < 3; channel++ {
		if err := send(dev, dtGamma, gammaPayload(channel)); err != nil {
			return err
		}
	}
	return nil
}

// readSides collects each side's scan data. In duplex the scanner
// alternates front/rear chunks until one side ends; every later chunk
// belongs to the side still running.
func readSides(dev scsiusb.BulkDevice, duplex bool) ([][]byte, []int, error) {
	n := 1
	if duplex {
		n = 2
	}
	data, lines, done := make([][]byte, n), make([]int, n), make([]bool, n)
	side, remaining := 0, n

	for reads := 0; remaining > 0; reads++ {
		if reads >= maxChunksPerSheet {
			return nil, nil, fmt.Errorf("doxiedx400: no end of page after %d chunks", reads)
		}
		chunk, s, err := readImageChunk(dev)
		if err != nil {
			return nil, nil, err
		}
		if s == nil {
			data[side] = append(data[side], chunk...)
		} else {
			if !s.endOfSide() {
				return nil, nil, s.err()
			}
			valid := min(max(s.validBytes, 0), len(chunk))
			data[side] = append(data[side], chunk[:valid]...)
			if len(data[side]) != s.sideBytes {
				return nil, nil, fmt.Errorf("doxiedx400: side %d is %d bytes, scanner reported %d", side, len(data[side]), s.sideBytes)
			}
			lines[side], done[side] = s.lines, true
			remaining--
		}
		if duplex && !done[1-side] {
			side = 1 - side
		}
	}
	return data, lines, nil
}

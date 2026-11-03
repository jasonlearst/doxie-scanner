package doxiedx400

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/andiapps-dev/doxie-scanner/internal/scsiusb"
)

// ErrNoPaper means the scanner reported its feeder empty.
var ErrNoPaper = errors.New("doxiedx400: no paper in the feeder")

const (
	senseMinLen    = 31
	offASC         = 7
	offASCQ        = 8
	offValidBytes  = 9
	offSenseLines  = 25
	offSideBytes   = 27
	ascADF         = 0x80
	ascqChuteEmpty = 0x03
	ascqPaperEnd   = 0x04
)

// sense is the vendor sense block (opcode 0xE3). At the end of a side it
// reports how many bytes of the final READ chunk are real, the side's
// line count, and the side's total byte count.
type sense struct {
	asc, ascq  byte
	validBytes int
	lines      int
	sideBytes  int
	raw        []byte
}

func parseSense(b []byte) (sense, error) {
	if len(b) < senseMinLen {
		return sense{}, &scsiusb.ScsiError{Message: fmt.Sprintf("short sense block: %d bytes", len(b))}
	}
	return sense{
		asc:        b[offASC],
		ascq:       b[offASCQ],
		validBytes: int(binary.BigEndian.Uint32(b[offValidBytes:])),
		lines:      int(binary.BigEndian.Uint16(b[offSenseLines:])),
		sideBytes:  int(binary.BigEndian.Uint32(b[offSideBytes:])),
		raw:        b,
	}, nil
}

func (s sense) endOfSide() bool { return s.asc == ascADF && s.ascq == ascqPaperEnd }

func (s sense) err() error {
	if s.asc == ascADF && s.ascq == ascqChuteEmpty {
		return ErrNoPaper
	}
	return &scsiusb.ScsiError{Message: fmt.Sprintf("scanner reported asc=%#02x ascq=%#02x (sense % x)", s.asc, s.ascq, s.raw)}
}

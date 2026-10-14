package opus

import "errors"

// ErrClosed is matched (with errors.Is) by the error every method of an Encoder, Decoder, Repacketizer or
// SoftClipper returns once the object has been closed, or disabled after an internal codec error.
var ErrClosed = errors.New("opus: closed")

// closedError names which kind of object was used after Close, and matches ErrClosed.
type closedError string

func (e closedError) Error() string        { return "opus: " + string(e) + " closed" }
func (e closedError) Is(target error) bool { return target == ErrClosed }

var (
	errDecoderClosed      error = closedError("decoder")
	errEncoderClosed      error = closedError("encoder")
	errRepacketizerClosed error = closedError("repacketizer")
	errSoftClipperClosed  error = closedError("soft clipper")
)

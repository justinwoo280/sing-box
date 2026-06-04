package xhttp

import (
	"github.com/sagernet/sing-box/common/xray/buf"
	"github.com/sagernet/sing-box/common/xray/pipe"
)

// A wrapper around pipe that ensures the size limit is exactly honored.
//
// The MultiBuffer pipe accepts any single WriteMultiBuffer call even if that
// single MultiBuffer exceeds the size limit, and then starts blocking on the
// next WriteMultiBuffer call. This means that ReadMultiBuffer can return more
// bytes than the size limit. We work around this by splitting a potentially
// too large write up into multiple.
type uploadWriter struct {
	*pipe.Writer
	maxLen int32
}

func (w uploadWriter) Write(b []byte) (int, error) {
	buffer := buf.MultiBufferContainer{}
	if _, err := buffer.Write(b); err != nil {
		return 0, err
	}

	var writed int
	for _, buff := range buffer.MultiBuffer {
		err := w.WriteMultiBuffer([]*buf.Buffer{buff})
		if err != nil {
			return writed, err
		}
		writed += int(buff.Len())
	}
	return writed, nil
}

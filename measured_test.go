package measured

import (
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/getlantern/testify/assert"
)

type MockReporter struct {
	s []*Stats
}

func (nr *MockReporter) Submit(s *Stats) error {
	nr.s = append(nr.s, s)
	return nil
}

func TestConnectionError(t *testing.T) {
	nr := MockReporter{}
	AddReporter(&nr)
	Start()
	defer Stop()
	runtime.Gosched()
	d := Dialer(net.Dial, "localhost:9000")
	_, _ = d("tcp", "localhost:9999")
	_, _ = d("tcp", "localhost:9998")
	time.Sleep(50 * time.Millisecond)
	if assert.Equal(t, 2, len(nr.s)) {
		assert.Equal(t, "localhost:9000", nr.s[0].Server, "should report correct server")
		assert.Equal(t, 1, nr.s[0].Errors["connection refused"], "should report connection reset")
		assert.Equal(t, 1, nr.s[1].Errors["connection refused"], "should report connection reset")
	}
}

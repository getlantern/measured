package measured

import (
	"net"
	"testing"

	"github.com/getlantern/testify/assert"
)

type MockReporter struct {
	s []*Stats
}

func (nr *MockReporter) Submit(s Stats) error {
	nr.s = append(nr.s, &s)
	return nil
}

func TestConnectionError(t *testing.T) {
	nr := MockReporter{}
	AddReporter(&nr)
	d := Dialer(net.Dial)
	_, _ = d("tcp", "localhost:9999")
	_, _ = d("tcp", "localhost:9998")
	assert.Equal(t, 1, nr.s[0].Errors["connection refused"], "should report connection reset")
	assert.Equal(t, 1, nr.s[1].Errors["connection refused"], "should report connection reset")
}

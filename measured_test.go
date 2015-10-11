package measured

import (
	"fmt"
	"net"
	"net/http"
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

func TestReportError(t *testing.T) {
	nr := startWithMockReporter()
	defer Stop()
	d := Dialer(net.Dial, "localhost:9000", 10*time.Second)
	_, _ = d("tcp", "localhost:9999")
	_, _ = d("tcp", "localhost:9998")
	time.Sleep(100 * time.Millisecond)
	if assert.Equal(t, 2, len(nr.s)) {
		assert.Equal(t, "errors", nr.s[0].Type, "should report correct remoteAddr")
		assert.Equal(t, "localhost:9000", nr.s[0].Tags["remoteAddr"], "should report correct remoteAddr")
		assert.Equal(t, "connection refused", nr.s[0].Tags["error"], "should report connection reset")
		assert.Equal(t, 1, nr.s[0].Fields["value"], "should report connection reset")

		assert.Equal(t, "errors", nr.s[1].Type, "should report correct remoteAddr")
		assert.Equal(t, "localhost:9000", nr.s[1].Tags["remoteAddr"], "should report correct remoteAddr")
		assert.Equal(t, "connection refused", nr.s[1].Tags["error"], "should report connection reset")
		assert.Equal(t, 1, nr.s[1].Fields["value"], "should report connection reset")
	}
}

func TestDefaultTags(t *testing.T) {
	nr := startWithMockReporter()
	defer Stop()
	SetDefaults(map[string]string{"app": "test-app"})
	reportError("test-remoteAddr", fmt.Errorf("test-error"), "dial-phase")
	time.Sleep(100 * time.Millisecond)
	if assert.Equal(t, 1, len(nr.s)) {
		assert.Equal(t, "test-app", nr.s[0].Tags["app"], "should report default tags")
	}
}

func TestListener(t *testing.T) {
	nr := startWithMockReporter()
	defer Stop()
	var bytesIn, bytesOut uint64
	var remoteAddr string
	l, err := net.Listen("tcp", ":0")
	if assert.NoError(t, err, "Listen should not fail") {
		ml := Listener(l, 10*time.Second)
		s := http.Server{
			Handler: http.NotFoundHandler(),
			ConnState: func(c net.Conn, s http.ConnState) {
				if s == http.StateIdle {
					mc := c.(*measuredConn)
					bytesIn = mc.bytesIn
					bytesOut = mc.bytesOut
					remoteAddr = mc.RemoteAddr().String()
					mc.Close()
				}
			},
		}
		go s.Serve(ml)
	}
	_, _ = http.Get("http://" + l.Addr().String())
	assert.Equal(t, uint64(92), bytesIn, "")
	assert.Equal(t, uint64(143), bytesOut, "")
	time.Sleep(100 * time.Millisecond)
	if assert.Equal(t, 1, len(nr.s)) {
		assert.Equal(t, "stats", nr.s[0].Type, "should report default tags")
		assert.Equal(t, remoteAddr, nr.s[0].Tags["remoteAddr"], "should report default tags")
		assert.Equal(t, bytesIn, nr.s[0].Fields["bytesIn"], "should report default tags")
		assert.Equal(t, bytesOut, nr.s[0].Fields["bytesOut"], "should report default tags")
	}

}

func startWithMockReporter() *MockReporter {
	nr := MockReporter{}
	Reset()
	AddReporter(&nr)
	Start()
	// To make sure it really started
	runtime.Gosched()
	return &nr
}

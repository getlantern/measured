package measured

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMeasuredListener(t *testing.T) {
	l, err := net.Listen("tcp", "localhost:0")
	if !assert.NoError(t, err) {
		return
	}

	var wg sync.WaitGroup
	wg.Add(1)
	rateInterval := 50 * time.Millisecond
	ml := WrapListener(l, rateInterval, func(conn Conn) {
		defer wg.Done()
		assert.Nil(t, conn.FirstError())

		stats := conn.Stats()
		if !assert.NotNil(t, stats) {
			return
		}

		assert.Equal(t, 8, stats.SentTotal)
		assert.True(t, stats.SentMin > 0)
		assert.True(t, stats.SentMax > 0)
		assert.True(t, stats.SentAvg > 0)

		assert.Equal(t, 10, stats.RecvTotal)
		assert.True(t, stats.RecvMin > 0)
		assert.True(t, stats.RecvMax > 0)
		assert.True(t, stats.RecvAvg > 0)

		assert.True(t, stats.Duration > 10*time.Millisecond, "Stats should have some duration")
	})

	go func() {
		_conn, err := ml.Accept()
		if err != nil {
			t.Fatal(err)
		}
		defer _conn.Close()
		conn := &slowConn{_conn}
		n, err := conn.Write([]byte("12345678"))
		if !assert.NoError(t, err) {
			return
		}
		assert.Equal(t, 8, n)
		b := make([]byte, 1000)
		n, err = conn.Read(b)
		if !assert.NoError(t, err) {
			return
		}
		assert.Equal(t, 10, n)
		// Be inactive for a bit
		time.Sleep(3 * rateInterval)
	}()

	conn, err := net.Dial("tcp", ml.Addr().String())
	if !assert.NoError(t, err) {
		return
	}
	conn.Write([]byte("1234567890"))
	conn.Close()
	wg.Wait()
}

package reporter

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getlantern/measured"
	"github.com/getlantern/testify/assert"
)

func TestWriteLineProtocol(t *testing.T) {
	chReq := make(chan []string, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := ioutil.ReadAll(r.Body)
		user, pass, ok := r.BasicAuth()
		assert.True(t, ok, "should send basic auth")
		chReq <- []string{user, pass, string(b)}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()
	ir := NewInfluxDBReporter(ts.URL, "test-user", "test-password", "testdb", nil)
	e := ir.Submit(&measured.Stats{
		Type: "errors",
		Tags: map[string]string{
			"server": "fl-nl-xxx",
			"error":  "test error",
		},
		Fields: map[string]interface{}{"value": 3},
	})
	assert.NoError(t, e, "should send to influxdb without error")
	req := <-chReq
	assert.Equal(t, req[0], "test-user", "")
	assert.Equal(t, req[1], "test-password", "")
	assert.Contains(t, req[2], "errors,", "should send measurement")
	assert.Contains(t, req[2], "error=test\\ error", "should send tag")
	assert.Contains(t, req[2], "server=fl-nl-xxx", "should send tag")
	assert.Contains(t, req[2], " value=3i ", "should send field")
	assert.NotContains(t, req[2], ", value=3i", "should not have trailing comma")
}

func TestCheckContent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	ir := NewInfluxDBReporter(ts.URL, "test-user", "test-password", "testdb", nil)
	e := ir.Submit(&measured.Stats{
		Fields: map[string]interface{}{"value": 3},
		Tags:   map[string]string{"server": "fl-nl-xxx"}})
	assert.Error(t, e, "should error if no type specified")
	e = ir.Submit(&measured.Stats{Type: "bytes"})
	assert.Error(t, e, "should error if no tag or field specified")
	e = ir.Submit(&measured.Stats{Type: "bytes",
		Fields: map[string]interface{}{"value": 3}})
	assert.Error(t, e, "should error if no tag specified")
	e = ir.Submit(&measured.Stats{Type: "bytes",
		Tags: map[string]string{
			"server": "fl-nl-xxx",
		}})
	assert.Error(t, e, "should error if no field specified")
	e = ir.Submit(&measured.Stats{Type: "bytes",
		Fields: map[string]interface{}{"value": 3},
		Tags:   map[string]string{"server": "fl-nl-xxx"}})
	assert.NoError(t, e, "should have no error for valid stat")
	e = ir.Submit(&measured.Stats{Type: "bytes",
		Fields: map[string]interface{}{"value": ""},
		Tags:   map[string]string{"server": "fl-nl-xxx"}})
	assert.Error(t, e, "should have error if field is empty")
	e = ir.Submit(&measured.Stats{Type: "bytes",
		Fields: map[string]interface{}{"value": 3},
		Tags:   map[string]string{"server": ""}})
	assert.Error(t, e, "should have error if tag is empty")
}

func TestRealProxyServer(t *testing.T) {
	ir := NewInfluxDBReporter("https://influx.getiantem.org/", "test", "test", "lantern", nil)
	e := ir.Submit(&measured.Stats{
		Type: "errors",
		Tags: map[string]string{
			"server": "fl-nl-xxx",
			"error":  "test error",
		},
		Fields: map[string]interface{}{"value": 3}})
	assert.NoError(t, e, "should send to influxdb without error")
}

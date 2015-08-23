package reporter

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/influxdb/influxdb/client"

	"github.com/getlantern/measured"
	"github.com/getlantern/testify/assert"
)

func TestConnectionError(t *testing.T) {
	chReq := make(chan string, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := ioutil.ReadAll(r.Body)
		chReq <- string(b)
		data := client.Response{}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(data)
	}))
	defer ts.Close()
	ir := NewInfluxDBReporter(ts.URL, "test", "test", "testdb", nil)
	e := ir.Submit(measured.Stats{
		Instance: "ln001",
		Server:   "fl-nl-xxx",
		Errors:   map[string]int{"test-error": 1}})
	assert.NoError(t, e, "", "")
	req := <-chReq
	assert.Contains(t, req, "errors,error=test-error,instance=ln001,server=fl-nl-xxx value=1i", "")

}

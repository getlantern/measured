package reporter

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/getlantern/golog"
	"github.com/getlantern/measured"
)

var (
	log = golog.LoggerFor("measured.reporter")
)

type influxDBReporter struct {
	httpClient *http.Client
	url        string
	username   string
	password   string
}

func NewInfluxDBReporter(influxURL, username, password, dbName string, httpClient *http.Client) measured.Reporter {
	if httpClient == nil {
		httpClient = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}
	}
	u := fmt.Sprintf("%s/write?db=%s", strings.TrimRight(influxURL, "/"), dbName)
	log.Debugf("Created InfluxDB reporter: %s", u)
	return &influxDBReporter{httpClient, u, username, password}
}

func (ir *influxDBReporter) Submit(s *measured.Stats) error {
	var buf bytes.Buffer

	// https://influxdb.com/docs/v0.9/write_protocols/write_syntax.html
	if s.Type == "" {
		return fmt.Errorf("No measurement type supplied")
	}
	buf.WriteString(s.Type)
	buf.WriteString(",")
	count, i := len(s.Tags), 0
	if count == 0 {
		return fmt.Errorf("No tags supplied")
	}
	for k, v := range s.Tags {
		i++
		if v == "" {
			return fmt.Errorf("Tag %s is empty", k)
		}
		buf.WriteString(fmt.Sprintf("%s=%s", k, escapeStringField(v)))
		if i < count {
			buf.WriteString(",")
		}
	}
	buf.WriteString(" ")

	count, i = len(s.Fields), 0
	if count == 0 {
		return fmt.Errorf("No fields supplied")
	}
	for k, v := range s.Fields {
		i++
		switch v.(type) {
		case string:
			s := v.(string)
			if s == "" {
				return fmt.Errorf("Field %s is empty", k)
			}
			buf.WriteString(fmt.Sprintf("%s=%s", k, s))
		case int:
			buf.WriteString(fmt.Sprintf("%s=%di", k, v))
		case float64:
			buf.WriteString(fmt.Sprintf("%s=%f", k, v))
		default:
			panic("Unsupported field type")
		}
		if i < count {
			buf.WriteString(",")
		}
	}

	buf.WriteString(fmt.Sprintf(" %d\n", time.Now().UnixNano()))
	req, err := http.NewRequest("POST", ir.url, &buf)
	if err != nil {
		log.Errorf("Error make POST request to %s: %s", ir.url, err)
		return err
	}
	req.SetBasicAuth(ir.username, ir.password)
	rsp, err := ir.httpClient.Do(req)
	if err != nil {
		log.Errorf("Error send POST request to %s: %s", ir.url, err)
		return err
	}
	if rsp.StatusCode != 204 {
		err = fmt.Errorf("Error response from %s: %s", ir.url, rsp.Status)
		log.Error(err)
		return err
	}
	return err
}

func escapeStringField(in string) string {
	var out []byte
	i := 0
	for {
		if i >= len(in) {
			break
		}
		if in[i] == ',' || in[i] == '=' || in[i] == ' ' {
			out = append(out, '\\')
			out = append(out, in[i])
			i += 1
			continue
		}
		out = append(out, in[i])
		i += 1

	}
	return string(out)
}

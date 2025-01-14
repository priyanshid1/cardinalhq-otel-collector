// Copyright 2024 CardinalHQ, Inc
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package datadogreceiver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/cespare/xxhash/v2"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"golang.org/x/exp/maps"
)

type DDLog struct {
	DDSource string `json:"ddsource,omitempty"`
	DDTags   string `json:"ddtags,omitempty"`
	Message  string `json:"message,omitempty"`
	Hostname string `json:"hostname,omitempty"`
	Service  string `json:"service,omitempty"`
}

func handleLogsPayload(req *http.Request) (ddLogs []DDLog, err error) {
	ddLogs = make([]DDLog, 0)
	body, err := io.ReadAll(req.Body)
	if err != nil {
		err = fmt.Errorf("failed to read request body: %w", err)
		return nil, err
	}

	err = json.Unmarshal(body, &ddLogs)
	if err != nil {
		// hack: special case '{}' which is not an array, but we get a lot of them...
		if len(body) == 2 && body[0] == 0x7b && body[1] == 0x7d {
			return ddLogs, nil
		}
		if len(body) > 10 {
			body = body[:10]
		}
		err = fmt.Errorf("failed to decode request body: %w (body=%x)", err, body)
		return nil, err
	}
	return ddLogs, nil
}

type groupedLogs struct {
	Messages []string
	Tags     map[string]string
	Service  string
	Hostname string
	DDSource string
}

func (ddr *datadogReceiver) splitLogs(apikey string, logs []DDLog) []groupedLogs {
	cachedTags := newLocalTagCache()

	logkeys := make(map[int64]groupedLogs)
	for _, log := range logs {
		tags := splitTags(log.DDTags)
		hostname := log.Hostname
		if hostname == "" {
			hostname = tags["host"]
		}
		ddr.hostnameTags.Add(context.Background(), 1,
			metric.WithAttributes(attribute.String("hostname", hostname), attribute.String("telemetry_type", "logs")))
		if hostname != "" {
			for _, tag := range cachedTags.FetchCache(ddr.tagcacheExtension, apikey, log.Hostname) {
				tags[tag.Name] = tag.Value
			}
		}
		key := tagKey(tags, []string{log.Service, log.Hostname, log.DDSource})
		if lk, ok := logkeys[key]; !ok {
			logkeys[key] = groupedLogs{
				Messages: []string{log.Message},
				Tags:     tags,
				Service:  log.Service,
				Hostname: log.Hostname,
				DDSource: log.DDSource,
			}
		} else {
			lk.Messages = append(lk.Messages, log.Message)
			logkeys[key] = lk
		}
	}
	return maps.Values(logkeys)
}

func (ddr *datadogReceiver) processLogs(ctx context.Context, apikey string, t pcommon.Timestamp, logs []DDLog) error {
	logparts := ddr.splitLogs(apikey, logs)
	for _, group := range logparts {
		otelLog, err := ddr.convertLogs(t, group)
		if err != nil {
			return err
		}
		if err := ddr.nextLogConsumer.ConsumeLogs(ctx, otelLog); err != nil {
			return err
		}
	}
	return nil
}

func splitTags(tags string) map[string]string {
	tagMap := make(map[string]string)
	if tags == "" {
		return tagMap
	}
	for _, tag := range strings.Split(tags, ",") {
		kv := strings.Split(tag, ":")
		if len(kv) == 2 && kv[1] != "" && kv[0] != "" {
			tagMap[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}
	return tagMap
}

func splitTagSlice(tags []string) map[string]string {
	tagMap := make(map[string]string)
	for _, tag := range tags {
		kv := strings.SplitN(tag, ":", 2)
		if len(kv) == 2 && kv[1] != "" && kv[0] != "" {
			tagMap[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}
	return tagMap
}

func tagKey(tags map[string]string, extra []string) int64 {
	keys := maps.Keys(tags)
	slices.Sort(keys)
	b := strings.Builder{}
	for i, k := range keys {
		if i > 0 {
			b.WriteString("::")
		}
		b.WriteString(k + "=" + tags[k])
	}
	for _, e := range extra {
		b.WriteString("::" + e)
	}
	return int64(xxhash.Sum64String(b.String()))
}

func (ddr *datadogReceiver) convertLogs(t pcommon.Timestamp, group groupedLogs) (plog.Logs, error) {
	lm := plog.NewLogs()
	rl := lm.ResourceLogs().AppendEmpty()
	rAttr := rl.Resource().Attributes()
	rl.SetSchemaUrl(semconv.SchemaURL)
	rAttr.PutStr(string(semconv.ServiceNameKey), group.Service)
	rAttr.PutStr(string(semconv.HostNameKey), group.Hostname)
	scope := rl.ScopeLogs().AppendEmpty()
	sAttr := scope.Scope().Attributes()
	sAttr.PutStr(string(semconv.TelemetrySDKNameKey), "Datadog")

	tags := group.Tags
	severityNumber, severityString := toSeverity(tags["status"])
	delete(tags, "status")

	lAttr := pcommon.NewMap()
	for k, v := range tags {
		decorateItem(k, v, rAttr, sAttr, lAttr)
	}
	if group.DDSource != "" {
		lAttr.PutStr("source", group.DDSource)
	}

	for _, msg := range group.Messages {
		logRecord := scope.LogRecords().AppendEmpty()
		logRecord.SetObservedTimestamp(t)
		logRecord.SetSeverityNumber(severityNumber)
		logRecord.SetSeverityText(severityString)
		logRecord.Body().SetStr(msg)
		lAttr.CopyTo(logRecord.Attributes())
	}

	return lm, nil
}

func toSeverity(s string) (plog.SeverityNumber, string) {
	s = strings.ToLower(s)
	number := plog.SeverityNumberUnspecified
	switch s {
	case "error":
		number = plog.SeverityNumberError
	case "warn":
		number = plog.SeverityNumberWarn
	case "info":
		number = plog.SeverityNumberInfo
	case "debug":
		number = plog.SeverityNumberDebug
	case "trace":
		number = plog.SeverityNumberTrace
	}
	return number, number.String()
}

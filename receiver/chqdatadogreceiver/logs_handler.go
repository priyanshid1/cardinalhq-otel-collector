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
	"net/http"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.uber.org/zap"
)

func (ddr *datadogReceiver) handleLogs(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Invalid method", http.StatusMethodNotAllowed)
		return
	}
	if req.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "Invalid content type", http.StatusUnsupportedMediaType)
		return
	}

	obsCtx := ddr.tReceiver.StartLogsOp(req.Context())
	var err error
	var logCount int
	defer func(logCount *int) {
		ddr.tReceiver.EndLogsOp(obsCtx, "datadog", *logCount, err)
	}(&logCount)

	ddLogs, err := handleLogsPayload(req)
	if err != nil {
		http.Error(w, "Unable to unmarshal reqs", http.StatusBadRequest)
		ddr.params.Logger.Error("Unable to unmarshal reqs", zap.Error(err))
		return
	}
	logCount = len(ddLogs)
	t := pcommon.NewTimestampFromTime(time.Now())
	err = ddr.processLogs(t, ddLogs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		ddr.params.Logger.Error("processLogs", zap.Error(err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(""))
}
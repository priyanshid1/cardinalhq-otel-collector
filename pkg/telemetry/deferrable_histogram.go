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

package telemetry

import "go.opentelemetry.io/otel/metric"

type DeferrableHistogram interface {
	Record(delta int64, options ...metric.RecordOption)
}

func HistogramAdd(histogram DeferrableHistogram, delta int64, options ...metric.RecordOption) {
	if histogram == nil {
		return
	}
	histogram.Record(delta, options...)
}
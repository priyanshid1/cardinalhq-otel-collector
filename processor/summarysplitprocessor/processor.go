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

package summarysplitprocessor

import (
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/processor"
	"go.uber.org/zap"
)

type summarysplit struct {
	logger             *zap.Logger
	id                 component.ID
	nextMetricReceiver consumer.Metrics
}

func newSummarySplitter(_ *Config, set processor.Settings, nextConsumer consumer.Metrics) (*summarysplit, error) {
	ss := &summarysplit{
		id:                 set.ID,
		logger:             set.Logger,
		nextMetricReceiver: nextConsumer,
	}

	ss.logger.Info("SummarySplit processor is enabled. Converting summary metrics to quantile metrics.")

	return ss, nil
}

func (e *summarysplit) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: true}
}
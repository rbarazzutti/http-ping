// Copyright 2021 Raphaël P. Barazzutti
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"fever.ch/http-ping/stats"
	"fmt"
	"net/http"
	"time"
)

// HTTPMeasure is the out of a measurement done as an HTTP ping
type HTTPMeasure struct {
	Proto string

	StatusCode   int
	Bytes        int64
	InBytes      int64
	OutBytes     int64
	SocketReused bool
	Compressed   bool
	RemoteAddr   string
	TLSEnabled   bool
	TLSVersion   string

	TotalTime         stats.Measure
	DNSResolution     stats.Measure
	TCPHandshake      stats.Measure
	TLSDuration       stats.Measure
	ConnEstablishment stats.Measure
	RequestSending    stats.Measure
	ResponseIngesting stats.Measure
	Wait              stats.Measure

	IsFailure    bool
	FailureCause string
	Headers      *http.Header
}

// Pinger does the calls to the actual HTTP/S component
type Pinger interface {
	Ping() <-chan *HTTPMeasure

	URL() string
}

type pingerImpl struct {
	client WebClient
	config *Config
}

// NewPinger builds a new pingerImpl
func NewPinger(config *Config, runtimeConfig *RuntimeConfig) (Pinger, error) {

	pinger := pingerImpl{}

	pinger.config = config

	client, err := NewWebClient(config, runtimeConfig)
	if err != nil {
		return nil, fmt.Errorf("%s (%s)", err, config.IPProtocol)
	}

	pinger.client = client

	return &pinger, nil
}

func (pinger *pingerImpl) URL() string {
	return pinger.client.URL()
}

// Ping actually does the pinging specified in config
func (pinger *pingerImpl) Ping() <-chan *HTTPMeasure {
	measures := make(chan *HTTPMeasure)
	go func() {
		defer close(measures)

		if !pinger.config.DisableKeepAlive || pinger.config.FollowRedirects {
			pinger.client.DoMeasure(pinger.config.FollowRedirects)
			time.Sleep(pinger.config.Interval)
		}

		for a := int64(0); a < pinger.config.Count; a++ {
			measures <- pinger.client.DoMeasure(false)
			time.Sleep(pinger.config.Interval)
		}

	}()
	return measures
}

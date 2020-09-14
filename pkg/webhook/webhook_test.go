// Copyright 2016-2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package webhook_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"text/template"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	h "github.com/aws/aws-node-termination-handler/pkg/test"
	"github.com/aws/aws-node-termination-handler/pkg/webhook"
	"github.com/rs/zerolog/log"
)

const (
	testDateFormat      = "02 Jan 2006 15:04:05 GMT"
	testWebhookHeaders  = `{"Content-type":"application/json"}`
	testWebhookTemplate = `{"text":"[NTH][Instance Interruption] EventID: {{ .EventID }} - Kind: {{ .Kind }} - Description: {{ .Description }} - Start Time: {{ .StartTime }}"}`
)

func parseScheduledEventTime(inputTime string) time.Time {
	scheduledTime, _ := time.Parse(testDateFormat, inputTime)
	return scheduledTime
}

func getExpectedMessage(event *monitor.InterruptionEvent) string {
	webhookTemplate, err := template.New("").Parse(testWebhookTemplate)
	if err != nil {
		log.Log().Err(err).Msg("Webhook Error: Template parsing failed")
		return ""
	}

	var byteBuffer bytes.Buffer
	webhookTemplate.Execute(&byteBuffer, event)

	m := map[string]interface{}{}
	if err := json.Unmarshal(byteBuffer.Bytes(), &m); err != nil {
		return ""
	}

	return fmt.Sprintf("%v", m["text"])
}

func TestPostSuccess(t *testing.T) {
	var requestPath string = "/some/path"

	event := &monitor.InterruptionEvent{
		EventID:     "instance-event-0d59937288b749b32",
		Kind:        "SCHEDULED_EVENT",
		Description: "Scheduled event will occur",
		State:       "active",
		StartTime:   parseScheduledEventTime("21 Jan 2019 09:00:43 GMT"),
		EndTime:     parseScheduledEventTime("21 Jan 2019 09:17:23 GMT"),
	}

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		h.Equals(t, req.Method, "POST")
		h.Equals(t, req.URL.String(), requestPath)

		// Test request headers
		headerMap := make(map[string]interface{})
		if err := json.Unmarshal([]byte(testWebhookHeaders), &headerMap); err != nil {
			t.Error("Unable to parse webhook headers")
		}
		h.Equals(t, req.Header.Get("Content-type"), headerMap["Content-type"])

		// Test request body
		requestBody, err := ioutil.ReadAll(req.Body)
		if err != nil {
			t.Error("Unable to read request body.")
		}
		requestMap := map[string]interface{}{}
		if err := json.Unmarshal([]byte(requestBody), &requestMap); err != nil {
			t.Error("Unable to parse request body to json.")
		}
		h.Equals(t, getExpectedMessage(event), requestMap["text"])

		rw.Write([]byte(`OK`))
	}))
	defer server.Close()

	nthconfig := config.Config{
		WebhookURL:      server.URL + requestPath,
		WebhookHeaders:  testWebhookHeaders,
		WebhookTemplate: testWebhookTemplate,
	}

	nodeMetadata := ec2metadata.NodeMetadata{}

	webhook.Post(nodeMetadata, event, nthconfig)
}

func TestPostTemplateParseError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		t.Error("Request made with invalid webhook")
	}))
	defer server.Close()

	event := &monitor.InterruptionEvent{}
	nthconfig := config.Config{
		WebhookURL:      server.URL,
		WebhookTemplate: "{{ ",
	}

	nodeMetadata := ec2metadata.NodeMetadata{}

	webhook.Post(nodeMetadata, event, nthconfig)
}

func TestPostTemplateExecutionError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		t.Error("Request made with invalid webhook")
	}))
	defer server.Close()

	event := &monitor.InterruptionEvent{}
	nthconfig := config.Config{
		WebhookURL:      server.URL,
		WebhookTemplate: `{{.cat}}`,
	}

	nodeMetadata := ec2metadata.NodeMetadata{}

	webhook.Post(nodeMetadata, event, nthconfig)
}

func TestPostNewHttpRequestError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		t.Error("Request made with invalid webhook")
	}))
	defer server.Close()

	event := &monitor.InterruptionEvent{}
	nthconfig := config.Config{
		WebhookURL:      "\t",
		WebhookTemplate: testWebhookTemplate,
	}
	nodeMetadata := ec2metadata.NodeMetadata{}

	webhook.Post(nodeMetadata, event, nthconfig)
}

func TestPostHeaderParseFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		t.Error("Request made with invalid webhook")
	}))
	defer server.Close()

	event := &monitor.InterruptionEvent{}
	nthconfig := config.Config{
		WebhookURL:      server.URL,
		WebhookTemplate: testWebhookTemplate,
	}
	nodeMetadata := ec2metadata.NodeMetadata{}

	webhook.Post(nodeMetadata, event, nthconfig)
}

func TestPostTimeout(t *testing.T) {
	var requestCount int = 0
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		requestCount++
		time.Sleep(6 * time.Second)
	}))
	defer server.Close()

	event := &monitor.InterruptionEvent{}
	nthconfig := config.Config{
		WebhookURL:      server.URL,
		WebhookTemplate: testWebhookTemplate,
		WebhookHeaders:  testWebhookHeaders,
	}
	nodeMetadata := ec2metadata.NodeMetadata{}

	webhook.Post(nodeMetadata, event, nthconfig)
	h.Equals(t, 1, requestCount)
}

func TestPostBadResponseCode(t *testing.T) {
	var requestCount int = 0
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		requestCount++
		http.Error(rw, "404 page not found", http.StatusNotFound)
	}))
	defer server.Close()

	event := &monitor.InterruptionEvent{}
	nthconfig := config.Config{
		WebhookURL:      server.URL,
		WebhookTemplate: testWebhookTemplate,
		WebhookHeaders:  testWebhookHeaders,
	}
	nodeMetadata := ec2metadata.NodeMetadata{}

	webhook.Post(nodeMetadata, event, nthconfig)
	h.Equals(t, 1, requestCount)
}

func TestValidateWebhookConfig(t *testing.T) {
	var nthConfig = config.Config{}
	err := webhook.ValidateWebhookConfig(nthConfig)
	h.Ok(t, err)

	nthConfig.WebhookURL = "http://123.123.123"
	nthConfig.WebhookTemplate = "{{ "
	err = webhook.ValidateWebhookConfig(nthConfig)
	h.Assert(t, err != nil, "Failed to return error for failing to parse webhook template")

	nthConfig.WebhookTemplate = "{{.cat}}"
	err = webhook.ValidateWebhookConfig(nthConfig)
	h.Assert(t, err != nil, "Failed to return error for failing to execute webhook template")

	nthConfig.WebhookTemplate = testWebhookTemplate
	err = webhook.ValidateWebhookConfig(nthConfig)
	h.Ok(t, err)
}

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

package webhook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/rs/zerolog/log"
)

type combinedDrainData struct {
	ec2metadata.NodeMetadata
	monitor.InterruptionEvent
	InstanceID   string
	InstanceType string
}

// Post makes a http post to send drain event data to webhook url
func Post(additionalInfo ec2metadata.NodeMetadata, event *monitor.InterruptionEvent, nthConfig config.Config) {
	var webhookTemplateContent string

	if nthConfig.WebhookTemplateFile != "" {
		content, err := os.ReadFile(nthConfig.WebhookTemplateFile)
		if err != nil {
			log.Err(err).
				Str("webhook_template_file", nthConfig.WebhookTemplateFile).
				Msg("Webhook Error: Could not read template file")
			return
		}
		webhookTemplateContent = string(content)
		log.Debug().Str("webhook_template_content", webhookTemplateContent)
	} else {
		webhookTemplateContent = nthConfig.WebhookTemplate
	}

	webhookTemplate, err := template.New("message").Funcs(sprig.TxtFuncMap()).Parse(webhookTemplateContent)
	if err != nil {
		log.Err(err).Msg("Webhook Error: Template parsing failed")
		return
	}

	// Need to merge the two data sources manually since both have
	// InstanceID and InstanceType fields
	instanceID := additionalInfo.InstanceID
	if event.InstanceID != "" {
		instanceID = event.InstanceID
	}
	instanceType := additionalInfo.InstanceType
	if event.InstanceType != "" {
		instanceType = event.InstanceType
	}
	var combined = combinedDrainData{NodeMetadata: additionalInfo, InterruptionEvent: *event, InstanceID: instanceID, InstanceType: instanceType}

	var byteBuffer bytes.Buffer
	err = webhookTemplate.Execute(&byteBuffer, combined)
	if err != nil {
		log.Err(err).Msg("Webhook Error: Template execution failed")
		return
	}

	request, err := http.NewRequest("POST", nthConfig.WebhookURL, &byteBuffer)
	if err != nil {
		log.Err(err).Msg("Webhook Error: Http NewRequest failed")
		return
	}

	headerMap := make(map[string]interface{})
	err = json.Unmarshal([]byte(nthConfig.WebhookHeaders), &headerMap)
	if err != nil {
		log.Err(err).Msg("Webhook Error: Header Unmarshal failed")
		return
	}
	for key, value := range headerMap {
		request.Header.Set(key, value.(string))
	}

	client := http.Client{
		Timeout: time.Duration(5 * time.Second),
		Transport: &http.Transport{
			IdleConnTimeout: 1 * time.Second,
			Proxy: func(req *http.Request) (*url.URL, error) {
				if nthConfig.WebhookProxy == "" {
					return nil, nil
				}
				proxy, err := url.Parse(nthConfig.WebhookProxy)
				if err != nil {
					return nil, err
				}
				return proxy, nil
			},
		},
	}
	response, err := client.Do(request)
	if err != nil {
		log.Err(err).Msg("Webhook Error: Client Do failed")
		return
	}

	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode > 299 {
		log.Warn().Int("status_code", response.StatusCode).Msg("Webhook Error: Received Non-Successful Status Code")
		return
	}

	log.Info().Msg("Webhook Success: Notification Sent!")
}

// ValidateWebhookConfig will check if the template provided in nthConfig with parse and execute
func ValidateWebhookConfig(nthConfig config.Config) error {
	if nthConfig.WebhookURL == "" {
		return nil
	}

	var webhookTemplateContent string

	if nthConfig.WebhookTemplateFile != "" {
		content, err := os.ReadFile(nthConfig.WebhookTemplateFile)
		if err != nil {
			return fmt.Errorf("Webhook Error: Could not read template file %w", err)
		}
		webhookTemplateContent = string(content)
	} else {
		webhookTemplateContent = nthConfig.WebhookTemplate
	}

	webhookTemplate, err := template.New("message").Funcs(sprig.TxtFuncMap()).Parse(webhookTemplateContent)
	if err != nil {
		return fmt.Errorf("Unable to parse webhook template: %w", err)
	}

	var byteBuffer bytes.Buffer
	err = webhookTemplate.Execute(&byteBuffer, &combinedDrainData{})
	if err != nil {
		return fmt.Errorf("Unable to execute webhook template: %w", err)
	}
	return nil
}

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
	"io/ioutil"
	"net/http"
	"net/url"
	"text/template"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/rs/zerolog/log"
)

type combinedDrainData struct {
	ec2metadata.NodeMetadata
	monitor.InterruptionEvent
}

// Post makes a http post to send drain event data to webhook url
func Post(additionalInfo ec2metadata.NodeMetadata, event *monitor.InterruptionEvent, nthConfig config.Config) {
	var webhookTemplateContent string

	if nthConfig.WebhookTemplateFile != "" {
		content, err := ioutil.ReadFile(nthConfig.WebhookTemplateFile)
		if err != nil {
			log.Log().
				Str("webhook_template_file", nthConfig.WebhookTemplateFile).
				Err(err).
				Msg("Webhook Error: Could not read template file")
			return
		}
		webhookTemplateContent = string(content)
		log.Debug().Msgf("Template file content - %s", webhookTemplateContent)
	} else {
		webhookTemplateContent = nthConfig.WebhookTemplate
	}

	webhookTemplate, err := template.New("message").Parse(webhookTemplateContent)
	if err != nil {
		log.Log().Err(err).Msg("Webhook Error: Template parsing failed")
		return
	}

	var combined = combinedDrainData{additionalInfo, *event}

	var byteBuffer bytes.Buffer
	err = webhookTemplate.Execute(&byteBuffer, combined)
	if err != nil {
		log.Log().Err(err).Msg("Webhook Error: Template execution failed")
		return
	}

	request, err := http.NewRequest("POST", nthConfig.WebhookURL, &byteBuffer)
	if err != nil {
		log.Log().Err(err).Msg("Webhook Error: Http NewRequest failed")
		return
	}

	headerMap := make(map[string]interface{})
	err = json.Unmarshal([]byte(nthConfig.WebhookHeaders), &headerMap)
	if err != nil {
		log.Log().Err(err).Msg("Webhook Error: Header Unmarshal failed")
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
		log.Log().Err(err).Msg("Webhook Error: Client Do failed")
		return
	}

	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode > 299 {
		log.Log().Int("status_code", response.StatusCode).Msg("Webhook Error: Received Non-Successful Status Code")
		return
	}

	log.Log().Msg("Webhook Success: Notification Sent!")
}

// ValidateWebhookConfig will check if the template provided in nthConfig with parse and execute
func ValidateWebhookConfig(nthConfig config.Config) error {
	if nthConfig.WebhookURL == "" {
		return nil
	}

	var webhookTemplateContent string

	if nthConfig.WebhookTemplateFile != "" {
		content, err := ioutil.ReadFile(nthConfig.WebhookTemplateFile)
		if err != nil {
			return fmt.Errorf("Webhook Error: Could not read template file %w", err)
		}
		webhookTemplateContent = string(content)
	} else {
		webhookTemplateContent = nthConfig.WebhookTemplate
	}

	webhookTemplate, err := template.New("message").Parse(webhookTemplateContent)
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

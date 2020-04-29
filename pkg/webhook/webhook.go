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
	"log"
	"net/http"
	"text/template"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	"github.com/aws/aws-node-termination-handler/pkg/interruptionevent"
)

type combinedDrainData struct {
	ec2metadata.NodeMetadata
	interruptionevent.InterruptionEvent
}

// Post makes a http post to send drain event data to webhook url
func Post(additionalInfo ec2metadata.NodeMetadata, event *interruptionevent.InterruptionEvent, nthconfig config.Config) {

	webhookTemplate, err := template.New("message").Parse(nthconfig.WebhookTemplate)
	if err != nil {
		log.Printf("Webhook Error: Template parsing failed - %s\n", err)
		return
	}

	var combined = combinedDrainData{additionalInfo, *event}

	var byteBuffer bytes.Buffer
	err = webhookTemplate.Execute(&byteBuffer, combined)
	if err != nil {
		log.Printf("Webhook Error: Template execution failed - %s\n", err)
		return
	}

	request, err := http.NewRequest("POST", nthconfig.WebhookURL, &byteBuffer)
	if err != nil {
		log.Printf("Webhook Error: Http NewRequest failed - %s\n", err)
		return
	}

	headerMap := make(map[string]interface{})
	err = json.Unmarshal([]byte(nthconfig.WebhookHeaders), &headerMap)
	if err != nil {
		log.Printf("Webhook Error: Header Unmarshal failed - %s\n", err)
		return
	}
	for key, value := range headerMap {
		request.Header.Set(key, value.(string))
	}

	client := http.Client{
		Timeout: time.Duration(5 * time.Second),
	}
	response, err := client.Do(request)
	if err != nil {
		log.Printf("Webhook Error: Client Do failed - %s\n", err)
		return
	}

	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode > 299 {
		log.Printf("Webhook Error: Received Status Code %d\n", response.StatusCode)
		return
	}

	log.Println("Webhook Success: Notification Sent!")
}

// ValidateWebhookConfig will check if the template provided in nthConfig with parse and execute
func ValidateWebhookConfig(nthConfig config.Config) error {
	if nthConfig.WebhookURL == "" {
		return nil
	}
	webhookTemplate, err := template.New("message").Parse(nthConfig.WebhookTemplate)
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

/*
Copyright 2022 Amazon.com, Inc. or its affiliates. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package adapter

import (
	"net/http"

	"github.com/aws/aws-node-termination-handler/api/v1alpha1"
	"github.com/aws/aws-node-termination-handler/pkg/terminator"
	"github.com/aws/aws-node-termination-handler/pkg/webhook"
)

type WebhookClientBuilder func(url, proxyURL, template string, headers http.Header) (webhook.Client, error)

func (b WebhookClientBuilder) NewWebhookClient(terminator *v1alpha1.Terminator) (terminator.WebhookClient, error) {
	headers := http.Header{}
	for _, h := range terminator.Spec.Webhook.Headers {
		headers.Add(h.Name, h.Value)
	}

	return b(
		terminator.Spec.Webhook.URL,
		terminator.Spec.Webhook.ProxyURL,
		terminator.Spec.Webhook.Template,
		headers,
	)
}

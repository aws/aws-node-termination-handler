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

package webhook

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	templatepkg "text/template"

	"github.com/aws/aws-node-termination-handler/pkg/logging"
)

type Client struct {
	url      string
	headers  http.Header
	sendFunc HttpSendFunc
	template *templatepkg.Template
}

func (c Client) NewRequest() Request {
	return Request{sendFunc: c.send}
}

func (c Client) send(ctx context.Context, n Notification) error {
	ctx = logging.WithLogger(ctx, logging.FromContext(ctx).Named("webhook"))

	if c.url == "" {
		return nil
	}

	var buf bytes.Buffer
	if err := c.template.Execute(&buf, n); err != nil {
		msg := "failed to populate template"
		logging.FromContext(ctx).With("error", err).Error(msg)
		return fmt.Errorf("%s: %w", msg, err)
	}

	req, err := http.NewRequest(http.MethodPost, c.url, &buf)
	if err != nil {
		msg := "failed to create request"
		logging.FromContext(ctx).With("error", err).Error(msg)
		return fmt.Errorf("%s: %w", msg, err)
	}

	req.Header = c.headers

	resp, err := c.sendFunc(req)
	if err != nil {
		msg := "request failed"
		logging.FromContext(ctx).With("error", err).Error(msg)
		return fmt.Errorf("%s: %w", msg, err)
	}
	if resp != nil {
		logging.FromContext(ctx).
			With("status", resp.StatusCode).
			Info("response status")
	}

	return nil
}

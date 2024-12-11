// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package sqsevent_test

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/monitor/sqsevent"
	h "github.com/aws/aws-node-termination-handler/pkg/test"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
)

type temporaryError struct {
	error
	temp bool
}

func TestGetSqsClient(t *testing.T) {
	retryer := getSqsRetryer(t)

	h.Equals(t, client.DefaultRetryerMaxNumRetries, retryer.NumMaxRetries)
	h.Equals(t, time.Duration(1200*time.Millisecond), retryer.MaxRetryDelay)
}

func TestShouldRetry(t *testing.T) {
	retryer := getSqsRetryer(t)

	testCases := []struct {
		name        string
		req         *request.Request
		shouldRetry bool
	}{
		{
			name: "AWS throttling error",
			req: &request.Request{
				Error: awserr.New("ThrottlingException", "Rate exceeded", nil),
			},
			shouldRetry: true,
		},
		{
			name: "AWS validation error",
			req: &request.Request{
				Error: awserr.New("ValidationError", "Invalid parameter", nil),
			},
			shouldRetry: false,
		},
		{
			name: "read connection reset by peer error",
			req: &request.Request{
				Error: &temporaryError{
					error: &net.OpError{
						Op:  "read",
						Err: fmt.Errorf("read: connection reset by peer"),
					},
					temp: false,
				}},
			shouldRetry: true,
		},
		{
			name: "read unknown error",
			req: &request.Request{
				Error: &temporaryError{
					error: &net.OpError{
						Op:  "read",
						Err: fmt.Errorf("read unknown error"),
					},
					temp: false,
				}},
			shouldRetry: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := retryer.ShouldRetry(tc.req)
			h.Equals(t, tc.shouldRetry, result)
		})
	}
}

func getSqsRetryer(t *testing.T) sqsevent.SqsRetryer {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String("us-east-1"),
	})
	h.Ok(t, err)

	sqsClient := sqsevent.GetSqsClient(sess)
	h.Assert(t, sqsClient.Client.Config.Region != nil, "Region should not be nil")
	h.Equals(t, "us-east-1", *sqsClient.Client.Config.Region)

	retryer, ok := sqsClient.Client.Config.Retryer.(sqsevent.SqsRetryer)
	h.Assert(t, ok, "Retryer should be of type SqsRetryer")
	return retryer
}

func (e *temporaryError) Temporary() bool {
	return e.temp
}

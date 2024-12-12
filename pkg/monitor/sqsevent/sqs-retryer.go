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

package sqsevent

import (
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sqs"
)

type SqsRetryer struct {
	client.DefaultRetryer
}

func (r SqsRetryer) ShouldRetry(req *request.Request) bool {
	return r.DefaultRetryer.ShouldRetry(req) ||
		(req.Error != nil && strings.Contains(req.Error.Error(), "connection reset"))
}

func GetSqsClient(sess *session.Session) *sqs.SQS {
	return sqs.New(sess, &aws.Config{
		Retryer: SqsRetryer{
			DefaultRetryer: client.DefaultRetryer{
				// Monitor continuously monitors SQS for events every 2 seconds
				NumMaxRetries:    client.DefaultRetryerMaxNumRetries,
				MinRetryDelay:    client.DefaultRetryerMinRetryDelay,
				MaxRetryDelay:    1200 * time.Millisecond,
				MinThrottleDelay: client.DefaultRetryerMinThrottleDelay,
				MaxThrottleDelay: 1200 * time.Millisecond,
			},
		},
	})
}

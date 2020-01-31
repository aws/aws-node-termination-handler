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

package drainevent

import (
	"testing"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	h "github.com/aws/aws-node-termination-handler/pkg/test"
)

func TestUncordonAfterRebootPreDrainSuccess(t *testing.T) {
	drainEvent := DrainEvent{}
	nthConfig := config.Config{
		DryRun: true,
	}
	tNode, _ := node.New(nthConfig)

	err := uncordonAfterRebootPreDrain(drainEvent, *tNode)
	h.Ok(t, err)
}

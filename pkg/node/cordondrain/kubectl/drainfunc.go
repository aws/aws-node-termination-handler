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

package kubectl

import (
	"context"
	"fmt"

	"github.com/aws/aws-node-termination-handler/pkg/logging"

	v1 "k8s.io/api/core/v1"
	"k8s.io/kubectl/pkg/drain"
)

var DefaultDrainer = DrainFunc(drain.RunNodeDrain)

type DrainFunc func(*drain.Helper, string) error

func (d DrainFunc) Drain(ctx context.Context, node *v1.Node, helper drain.Helper) error {
	ctx = logging.WithLogger(ctx, logging.FromContext(ctx).Named("drain"))

	if node == nil {
		err := fmt.Errorf("argument 'node' is nil")
		logging.FromContext(ctx).
			With("error", err).
			Error("failed to drain node")
		return err
	}

	helper.Ctx = ctx
	helper.Out = logging.Writer{SugaredLogger: logging.FromContext(ctx)}
	helper.ErrOut = helper.Out

	if err := d(&helper, node.Name); err != nil {
		logging.FromContext(ctx).
			With("error", err).
			Error("failed to drain node")
		return err
	}

	return nil
}

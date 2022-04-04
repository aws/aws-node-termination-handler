/*
Copyright 2022.

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

var DefaultCordoner = CordonFunc(drain.RunCordonOrUncordon)

type CordonFunc func(*drain.Helper, *v1.Node, bool) error

func (c CordonFunc) Cordon(ctx context.Context, node *v1.Node, helper drain.Helper) error {
	ctx = logging.WithLogger(ctx, logging.FromContext(ctx).Named("cordon"))

	if node == nil {
		err := fmt.Errorf("argument 'node' is nil")
		logging.FromContext(ctx).
			With("error", err).
			Error("failed to cordon node")
		return err
	}

	helper.Ctx = ctx
	helper.Out = logging.Writer{SugaredLogger: logging.FromContext(ctx)}
	helper.ErrOut = helper.Out

	const updateNodeUnschedulable = true
	if err := c(&helper, node, updateNodeUnschedulable); err != nil {
		logging.FromContext(ctx).
			With("error", err).
			Error("failed to cordon node")
		return err
	}

	return nil
}

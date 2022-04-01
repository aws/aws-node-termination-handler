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

	"github.com/aws/aws-node-termination-handler/pkg/node/cordondrain"
	"go.uber.org/multierr"

	v1 "k8s.io/api/core/v1"
	"k8s.io/kubectl/pkg/drain"
)

type cordonDrainer struct {
	Cordoner
	Drainer
	drain.Helper
}

func NewCordonDrainer(helper drain.Helper, cordoner Cordoner, drainer Drainer) (cordondrain.CordonDrainer, error) {
	var err error
	if cordoner == nil {
		err = multierr.Append(err, fmt.Errorf("argument 'cordoner' is nil"))
	}
	if drainer == nil {
		err = multierr.Append(err, fmt.Errorf("arguemnt 'drainer' is nil"))
	}
	if err != nil {
		return nil, err
	}

	return cordonDrainer{
		Cordoner: cordoner,
		Drainer:  drainer,
		Helper:   helper,
	}, nil
}

func (c cordonDrainer) Cordon(ctx context.Context, node *v1.Node) error {
	return c.Cordoner.Cordon(ctx, node, c.Helper)
}

func (c cordonDrainer) Drain(ctx context.Context, node *v1.Node) error {
	return c.Drainer.Drain(ctx, node, c.Helper)
}

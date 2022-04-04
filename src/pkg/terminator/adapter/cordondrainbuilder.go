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

package adapter

import (
	"fmt"

	"github.com/aws/aws-node-termination-handler/api/v1alpha1"
	"github.com/aws/aws-node-termination-handler/pkg/node/cordondrain"
	kubectlcordondrain "github.com/aws/aws-node-termination-handler/pkg/node/cordondrain/kubectl"
	"github.com/aws/aws-node-termination-handler/pkg/terminator"
)

type CordonDrainerBuilder struct {
	kubectlcordondrain.Builder
}

func (b CordonDrainerBuilder) NewCordonDrainer(terminator *v1alpha1.Terminator) (terminator.CordonDrainer, error) {
	if terminator == nil {
		return nil, fmt.Errorf("argument 'terminator' is nil")
	}

	return b.Build(cordondrain.Config{
		Force:               terminator.Spec.Drain.Force,
		GracePeriodSeconds:  terminator.Spec.Drain.GracePeriodSeconds,
		IgnoreAllDaemonSets: terminator.Spec.Drain.IgnoreAllDaemonSets,
		DeleteEmptyDirData:  terminator.Spec.Drain.DeleteEmptyDirData,
		TimeoutSeconds:      terminator.Spec.Drain.TimeoutSeconds,
	})
}

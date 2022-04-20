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

package terminator

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-node-termination-handler/api/v1alpha1"
	"github.com/aws/aws-node-termination-handler/pkg/logging"

	"github.com/aws/aws-sdk-go/service/sqs"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"go.uber.org/multierr"
	"go.uber.org/zap/zapcore"

	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type (
	CordonDrainer interface {
		Cordon(context.Context, *v1.Node) error
		Drain(context.Context, *v1.Node) error
	}

	CordonDrainerBuilder interface {
		NewCordonDrainer(*v1alpha1.Terminator) (CordonDrainer, error)
	}

	Event interface {
		zapcore.ObjectMarshaler

		Done(context.Context) (tryAgain bool, err error)
		EC2InstanceIDs() []string
		Kind() EventKind
	}

	Getter interface {
		GetTerminator(context.Context, types.NamespacedName) (*v1alpha1.Terminator, error)
	}

	NodeGetter interface {
		GetNode(context.Context, string) (*v1.Node, error)
	}

	NodeGetterBuilder interface {
		NewNodeGetter(*v1alpha1.Terminator) NodeGetter
	}

	NodeNameGetter interface {
		GetNodeName(context.Context, string) (string, error)
	}

	SQSClient interface {
		GetSQSMessages(context.Context) ([]*sqs.Message, error)
		DeleteSQSMessage(context.Context, *sqs.Message) error
	}

	SQSClientBuilder interface {
		NewSQSClient(*v1alpha1.Terminator) (SQSClient, error)
	}

	SQSMessageParser interface {
		Parse(context.Context, *sqs.Message) Event
	}

	Reconciler struct {
		NodeGetterBuilder
		NodeNameGetter
		SQSClientBuilder
		SQSMessageParser
		CordonDrainerBuilder
		Getter

		Name            string
		RequeueInterval time.Duration
	}
)

func (r Reconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	ctx = logging.WithLogger(ctx, logging.FromContext(ctx).Named(r.Name).With("terminator", req.Name))

	terminator, err := r.GetTerminator(ctx, req.NamespacedName)
	if err != nil {
		return reconcile.Result{}, err
	}
	if terminator == nil {
		return reconcile.Result{}, nil
	}

	nodeGetter := r.NewNodeGetter(terminator)

	cordondrainer, err := r.NewCordonDrainer(terminator)
	if err != nil {
		return reconcile.Result{}, err
	}

	sqsClient, err := r.NewSQSClient(terminator)
	if err != nil {
		return reconcile.Result{}, err
	}

	sqsMessages, err := sqsClient.GetSQSMessages(ctx)
	if err != nil {
		return reconcile.Result{}, err
	}

	origCtx := ctx
	for _, msg := range sqsMessages {
		ctx = logging.WithLogger(origCtx, logging.FromContext(origCtx).
			With("sqsMessage", logging.NewMessageMarshaler(msg)),
		)

		evt := r.Parse(ctx, msg)
		ctx = logging.WithLogger(ctx, logging.FromContext(ctx).With("event", evt))

		allInstancesHandled := true
		savedCtx := ctx
		for _, ec2InstanceID := range evt.EC2InstanceIDs() {
			ctx = logging.WithLogger(savedCtx, logging.FromContext(savedCtx).
				With("ec2InstanceID", ec2InstanceID),
			)

			nodeName, e := r.GetNodeName(ctx, ec2InstanceID)
			if e != nil {
				err = multierr.Append(err, e)
				allInstancesHandled = false
				continue
			}

			ctx = logging.WithLogger(ctx, logging.FromContext(ctx).With("node", nodeName))

			node, e := nodeGetter.GetNode(ctx, nodeName)
			if node == nil {
				logger := logging.FromContext(ctx)
				if e != nil {
					logger = logger.With("error", e)
				}
				logger.Warn("no matching node found")
				allInstancesHandled = false
				continue
			}

			if e = cordondrainer.Cordon(ctx, node); e != nil {
				err = multierr.Append(err, e)
				continue
			}

			if e = cordondrainer.Drain(ctx, node); e != nil {
				err = multierr.Append(err, e)
				continue
			}
		}
		ctx = savedCtx

		tryAgain, e := evt.Done(ctx)
		if e != nil {
			err = multierr.Append(err, e)
		}

		if tryAgain || !allInstancesHandled {
			continue
		}

		err = multierr.Append(err, sqsClient.DeleteSQSMessage(ctx, msg))
	}
	ctx = origCtx

	if err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{RequeueAfter: r.RequeueInterval}, nil
}

func (r Reconciler) BuildController(builder *builder.Builder) error {
	if builder == nil {
		return fmt.Errorf("argument 'builder' is nil")
	}

	return builder.
		Named(r.Name).
		For(&v1alpha1.Terminator{}).
		Complete(r)
}

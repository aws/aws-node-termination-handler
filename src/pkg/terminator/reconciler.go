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

package terminator

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-node-termination-handler/api/v1alpha1"
	"github.com/aws/aws-node-termination-handler/pkg/event"
	"github.com/aws/aws-node-termination-handler/pkg/logging"
	"github.com/aws/aws-node-termination-handler/pkg/node/cordondrain"

	"github.com/aws/aws-sdk-go/service/sqs"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"go.uber.org/multierr"

	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type (
	NodeGetter interface {
		GetNode(context.Context, string) (*v1.Node, error)
	}

	NodeNameGetter interface {
		GetNodeName(context.Context, string) (string, error)
	}

	SqsMessageParser interface {
		Parse(context.Context, *sqs.Message) event.Event
	}

	CordonDrainerBuilder interface {
		NewCordonDrainer(*v1alpha1.Terminator) (cordondrain.CordonDrainer, error)
	}

	Getter interface {
		GetTerminator(context.Context, types.NamespacedName) (*v1alpha1.Terminator, error)
	}

	SqsClient interface {
		GetSqsMessages(context.Context) ([]*sqs.Message, error)
		DeleteSqsMessage(context.Context, *sqs.Message) error
	}

	SqsClientBuilder interface {
		NewSqsClient(*v1alpha1.Terminator) (SqsClient, error)
	}

	Reconciler struct {
		NodeGetter
		NodeNameGetter
		SqsClientBuilder
		SqsMessageParser
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

	cordondrainer, err := r.NewCordonDrainer(terminator)
	if err != nil {
		return reconcile.Result{}, err
	}

	sqsClient, err := r.NewSqsClient(terminator)
	if err != nil {
		return reconcile.Result{}, err
	}

	sqsMessages, err := sqsClient.GetSqsMessages(ctx)
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

		savedCtx := ctx
		for _, ec2InstanceId := range evt.Ec2InstanceIds() {
			ctx = logging.WithLogger(savedCtx, logging.FromContext(savedCtx).
				With("ec2InstanceId", ec2InstanceId),
			)

			nodeName, e := r.GetNodeName(ctx, ec2InstanceId)
			if e != nil {
				err = multierr.Append(err, e)
				continue
			}

			ctx = logging.WithLogger(ctx, logging.FromContext(ctx).With("node", nodeName))

			node, e := r.GetNode(ctx, nodeName)
			if e != nil {
				err = multierr.Append(err, e)
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

		if tryAgain {
			continue
		}

		err = multierr.Append(err, sqsClient.DeleteSqsMessage(ctx, msg))
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

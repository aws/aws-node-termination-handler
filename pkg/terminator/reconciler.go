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
	"github.com/aws/aws-node-termination-handler/pkg/webhook"

	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

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
		webhook.Event
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
		GetSQSMessages(context.Context) ([]sqstypes.Message, error)
		DeleteSQSMessage(context.Context, *sqstypes.Message) error
	}

	SQSClientBuilder interface {
		NewSQSClient(*v1alpha1.Terminator) (SQSClient, error)
	}

	SQSMessageParser interface {
		Parse(context.Context, sqstypes.Message) Event
	}

	WebhookClient interface {
		NewRequest() webhook.Request
	}

	WebhookClientBuilder interface {
		NewWebhookClient(*v1alpha1.Terminator) (WebhookClient, error)
	}

	Reconciler struct {
		CordonDrainerBuilder
		Getter
		NodeGetterBuilder
		NodeNameGetter
		SQSClientBuilder
		SQSMessageParser
		WebhookClientBuilder

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

	webhookClient, err := r.NewWebhookClient(terminator)
	if err != nil {
		logging.FromContext(ctx).With("error", err).Warn("failed to initialize webhook client")
	}

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

	for _, msg := range sqsMessages {
		e := r.handleMessage(ctx, msg, terminator, nodeGetter, cordondrainer, sqsClient, webhookClient.NewRequest())
		err = multierr.Append(err, e)
	}

	if err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{RequeueAfter: r.RequeueInterval}, nil
}

func (r Reconciler) handleMessage(ctx context.Context, msg sqstypes.Message, terminator *v1alpha1.Terminator, nodeGetter NodeGetter, cordondrainer CordonDrainer, sqsClient SQSClient, webhookRequest webhook.Request) (err error) {
	ctx = logging.WithLogger(ctx, logging.FromContext(ctx).With("sqsMessage", logging.NewMessageMarshaler(msg)))

	evt := r.Parse(ctx, msg)
	ctx = logging.WithLogger(ctx, logging.FromContext(ctx).With("event", evt))

	webhookRequest.Event = evt

	evtAction := actionForEvent(evt, terminator)
	ctx = logging.WithLogger(ctx, logging.FromContext(ctx).With("action", evtAction))

	allInstancesHandled := true
	if evtAction != v1alpha1.Actions.NoAction {
		for _, ec2InstanceID := range evt.EC2InstanceIDs() {
			webhookRequest.InstanceID = ec2InstanceID
			instanceHandled, e := r.handleInstance(ctx, ec2InstanceID, evtAction, nodeGetter, cordondrainer, webhookRequest)
			err = multierr.Append(err, e)
			allInstancesHandled = allInstancesHandled && instanceHandled
		}
	}

	tryAgain, e := evt.Done(ctx)
	if e != nil {
		err = multierr.Append(err, e)
	}

	if tryAgain || !allInstancesHandled {
		return err
	}

	return multierr.Append(err, sqsClient.DeleteSQSMessage(ctx, &msg))
}

func (r Reconciler) handleInstance(ctx context.Context, ec2InstanceID string, evtAction v1alpha1.Action, nodeGetter NodeGetter, cordondrainer CordonDrainer, webhookRequest webhook.Request) (bool, error) {
	ctx = logging.WithLogger(ctx, logging.FromContext(ctx).With("ec2InstanceID", ec2InstanceID))

	nodeName, err := r.GetNodeName(ctx, ec2InstanceID)
	if err != nil {
		return false, err
	}

	ctx = logging.WithLogger(ctx, logging.FromContext(ctx).With("node", nodeName))

	node, err := nodeGetter.GetNode(ctx, nodeName)
	if node == nil {
		logger := logging.FromContext(ctx)
		if err != nil {
			logger = logger.With("error", err)
		}
		logger.Warn("no matching node found")
		return false, nil
	}

	webhookRequest.NodeName = nodeName
	if err = webhookRequest.Send(ctx); err != nil {
		logging.FromContext(ctx).
			With("error", err).
			Error("webhook notification failed")
	}

	if err = cordondrainer.Cordon(ctx, node); err != nil {
		return true, err
	}

	if evtAction == v1alpha1.Actions.Cordon {
		return true, nil
	}

	if err = cordondrainer.Drain(ctx, node); err != nil {
		return true, err
	}

	return true, nil
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

func actionForEvent(evt Event, terminator *v1alpha1.Terminator) v1alpha1.Action {
	events := terminator.Spec.Events

	switch evt.Kind() {
	case EventKinds.AutoScalingTermination:
		return events.AutoScalingTermination

	case EventKinds.RebalanceRecommendation:
		return events.RebalanceRecommendation

	case EventKinds.ScheduledChange:
		return events.ScheduledChange

	case EventKinds.SpotInterruption:
		return events.SpotInterruption

	case EventKinds.StateChange:
		return events.StateChange

	default:
		return v1alpha1.Actions.NoAction
	}
}

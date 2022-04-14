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

package v1alpha1

import (
	"context"
	"net/url"

	"github.com/aws/aws-sdk-go/service/sqs"

	"k8s.io/apimachinery/pkg/util/sets"

	"knative.dev/pkg/apis"
)

var (
	// https://github.com/aws/aws-sdk-go/blob/v1.38.55/service/sqs/api.go#L3966-L3994
	knownSQSAttributeNames = sets.NewString(sqs.MessageSystemAttributeName_Values()...)
)

func (t *Terminator) Validate(_ context.Context) (errs *apis.FieldError) {
	return errs.Also(
		apis.ValidateObjectMetadata(t).ViaField("metadata"),
		t.Spec.validate().ViaField("spec"),
	)
}

func (t *TerminatorSpec) validate() (errs *apis.FieldError) {
	return t.SQS.validate().ViaField("sqs")
}

func (s *SQSSpec) validate() (errs *apis.FieldError) {
	if _, err := url.Parse(s.QueueURL); err != nil {
		errs = errs.Also(apis.ErrInvalidValue(s.QueueURL, "queueURL", "must be a valid URL"))
	}
	return errs
}

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

package v1alpha1

import (
	"context"
	"net/url"
	"strings"

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
	for _, attrName := range s.AttributeNames {
		if !knownSQSAttributeNames.Has(attrName) {
			errs = errs.Also(apis.ErrInvalidValue(attrName, "attributeNames"))
		}
	}

	// https://github.com/aws/aws-sdk-go/blob/v1.38.55/service/sqs/api.go#L3996-L3999
	if s.MaxNumberOfMessages < 1 || 10 < s.MaxNumberOfMessages {
		errs = errs.Also(apis.ErrInvalidValue(s.MaxNumberOfMessages, "maxNumberOfMessages", "must be in range 1-10"))
	}

	// https://github.com/aws/aws-sdk-go/blob/v1.38.55/service/sqs/api.go#L4001-L4021
	//
	// Simple checks are done below. More indepth checks are left to the SQS client/service.
	for _, attrName := range s.MessageAttributeNames {
		if len(attrName) > 256 {
			errs = errs.Also(apis.ErrInvalidValue(attrName, "messageAttributeNames", "must be 256 characters or less"))
		}

		lcAttrName := strings.ToLower(attrName)
		if strings.HasPrefix(lcAttrName, "aws") || strings.HasPrefix(lcAttrName, "amazon") {
			errs = errs.Also(apis.ErrInvalidValue(attrName, "messageAttributeNames", `must not use reserved prefixes "AWS" or "Amazon"`))
		}

		if strings.HasPrefix(attrName, ".") || strings.HasSuffix(attrName, ".") {
			errs = errs.Also(apis.ErrInvalidValue(attrName, "messageAttributeNames", "must not begin or end with a period (.)"))
		}
	}

	if _, err := url.Parse(s.QueueURL); err != nil {
		errs = errs.Also(apis.ErrInvalidValue(s.QueueURL, "queueURL", "must be a valid URL"))
	}

	if s.VisibilityTimeoutSeconds < 0 {
		errs = errs.Also(apis.ErrInvalidValue(s.VisibilityTimeoutSeconds, "visibilityTimeoutSeconds", "must be zero or greater"))
	}

	if s.WaitTimeSeconds < 0 {
		errs = errs.Also(apis.ErrInvalidValue(s.WaitTimeSeconds, "waitTimeSeconds", "must be zero or greater"))
	}

	return errs
}

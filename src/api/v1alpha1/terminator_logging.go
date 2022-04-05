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
	"go.uber.org/zap/zapcore"
)

func (t *TerminatorSpec) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddObject("sqs", t.SQS)
	enc.AddObject("drain", t.Drain)
	return nil
}

func (s SQSSpec) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("queueURL", s.QueueURL)
	return nil
}

func (d DrainSpec) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddBool("force", d.Force)
	enc.AddInt("gracePeriodSeconds", d.GracePeriodSeconds)
	enc.AddBool("ignoreAllDaemonSets", d.IgnoreAllDaemonSets)
	enc.AddBool("deleteEmptyDirData", d.DeleteEmptyDirData)
	enc.AddInt("timeoutSeconds", d.TimeoutSeconds)
	return nil
}

/*
Copyright 2021 The Kubernetes Authors.

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

package awstasks

import (
	"fmt"

	"github.com/aws/aws-sdk-go/service/autoscaling"
	"k8s.io/kops/upup/pkg/fi"
	"k8s.io/kops/upup/pkg/fi/cloudup/awsup"
	"k8s.io/kops/upup/pkg/fi/cloudup/terraform"
)

// WarmPool provdes the definition for an ASG warm pool in aws.
// +kops:fitask
type WarmPool struct {
	// Name is the name of the task.
	Name *string

	// Lifecycle is the resource lifecycle.
	Lifecycle fi.Lifecycle

	Enabled *bool
	// MaxSize is the max number of nodes in the warm pool.
	MaxSize *int64
	// MinSize is the smallest number of nodes in the warm pool.
	MinSize int64

	AutoscalingGroup *AutoscalingGroup
}

// Find is used to discover the ASG in the cloud provider.
func (e *WarmPool) Find(c *fi.CloudupContext) (*WarmPool, error) {
	cloud := c.T.Cloud.(awsup.AWSCloud)
	svc := cloud.Autoscaling()
	warmPool, err := svc.DescribeWarmPool(&autoscaling.DescribeWarmPoolInput{
		AutoScalingGroupName: e.AutoscalingGroup.Name,
	})
	if err != nil {
		if awsup.AWSErrorCode(err) == "ValidationError" {
			return nil, nil
		}
		return nil, err
	}
	if warmPool.WarmPoolConfiguration == nil {
		return &WarmPool{
			Name:             e.Name,
			Lifecycle:        e.Lifecycle,
			Enabled:          fi.PtrTo(false),
			AutoscalingGroup: &AutoscalingGroup{Name: e.AutoscalingGroup.Name},
		}, nil
	}

	actual := &WarmPool{
		Name:             e.Name,
		Lifecycle:        e.Lifecycle,
		Enabled:          fi.PtrTo(true),
		AutoscalingGroup: &AutoscalingGroup{Name: e.AutoscalingGroup.Name},
		MaxSize:          warmPool.WarmPoolConfiguration.MaxGroupPreparedCapacity,
		MinSize:          fi.ValueOf(warmPool.WarmPoolConfiguration.MinSize),
	}
	return actual, nil
}

func (e *WarmPool) Run(c *fi.CloudupContext) error {
	return fi.CloudupDefaultDeltaRunMethod(e, c)
}

func (*WarmPool) CheckChanges(a, e, changes *WarmPool) error {
	return nil
}

func (*WarmPool) RenderAWS(t *awsup.AWSAPITarget, a, e, changes *WarmPool) error {
	svc := t.Cloud.Autoscaling()
	if changes != nil {
		if fi.ValueOf(e.Enabled) {
			minSize := e.MinSize
			maxSize := e.MaxSize
			if maxSize == nil {
				maxSize = fi.PtrTo(int64(-1))
			}
			request := &autoscaling.PutWarmPoolInput{
				AutoScalingGroupName:     e.AutoscalingGroup.Name,
				MaxGroupPreparedCapacity: maxSize,
				MinSize:                  fi.PtrTo(minSize),
			}

			_, err := svc.PutWarmPool(request)
			if err != nil {
				if awsup.AWSErrorCode(err) == "ValidationError" {
					return fi.NewTryAgainLaterError("waiting for ASG to become ready").WithError(err)
				}
				return fmt.Errorf("error modifying warm pool: %w", err)
			}
		} else if a != nil {
			_, err := svc.DeleteWarmPool(&autoscaling.DeleteWarmPoolInput{
				AutoScalingGroupName: e.AutoscalingGroup.Name,
				// We don't need to do any cleanup so, the faster the better
				ForceDelete: fi.PtrTo(true),
			})
			if err != nil {
				return fmt.Errorf("error deleting warm pool: %w", err)
			}
		}
	}
	return nil
}

// For the terraform target, warmpool config is rendered inside the AutoscalingGroup resource
func (_ *WarmPool) RenderTerraform(t *terraform.TerraformTarget, a, e, changes *WarmPool) error {
	return nil
}

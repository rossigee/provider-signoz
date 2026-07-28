/*
Copyright 2024 The Crossplane Authors.

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

// Package controller contains the controllers for the SigNoz provider.
package controller

import (
	"github.com/crossplane/crossplane-runtime/v2/pkg/controller"
	"github.com/rossigee/provider-signoz/internal/controller/alert"
	"github.com/rossigee/provider-signoz/internal/controller/channel"
	"github.com/rossigee/provider-signoz/internal/controller/dashboard"
	"github.com/rossigee/provider-signoz/internal/controller/providerconfig"
	ctrl "sigs.k8s.io/controller-runtime"
)

// Setup sets up all controllers for the SigNoz provider.
func Setup(mgr ctrl.Manager, o controller.Options) error {
	if err := providerconfig.Setup(mgr); err != nil {
		return err
	}
	if err := dashboard.Setup(mgr, o); err != nil {
		return err
	}
	if err := alert.Setup(mgr, o); err != nil {
		return err
	}
	if err := channel.Setup(mgr, o); err != nil {
		return err
	}
	return nil
}
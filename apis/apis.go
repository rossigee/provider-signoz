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

package apis

import (
	"k8s.io/apimachinery/pkg/runtime"

	alertv1alpha1 "github.com/rossigee/provider-signoz/apis/alert/v1alpha1"
	channelv1alpha1 "github.com/rossigee/provider-signoz/apis/channel/v1alpha1"
	dashboardv1alpha1 "github.com/rossigee/provider-signoz/apis/dashboard/v1alpha1"
	v1beta1 "github.com/rossigee/provider-signoz/apis/v1beta1"
)

func init() {
	// Register all APIs
	AddToSchemes = append(AddToSchemes, 
		v1beta1.SchemeBuilder.AddToScheme,
		alertv1alpha1.SchemeBuilder.AddToScheme,
		channelv1alpha1.SchemeBuilder.AddToScheme,
		dashboardv1alpha1.SchemeBuilder.AddToScheme,
	)
}

// AddToSchemes may be used to add all resources defined in the project to a Scheme
var AddToSchemes runtime.SchemeBuilder

// AddToScheme adds all Resources to the Scheme
func AddToScheme(s *runtime.Scheme) error {
	return AddToSchemes.AddToScheme(s)
}
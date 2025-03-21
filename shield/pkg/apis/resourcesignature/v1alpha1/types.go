//
// Copyright 2020 IBM Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package v1alpha1

import (
	"fmt"

	ishieldyaml "github.com/IBM/integrity-enforcer/shield/pkg/util/yaml"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	MatchByExactMatch   string = "exactMatch"
	MatchByKnownFilter  string = "useKnownFilter"
	MatchByCustomFilter string = "customFilter"

	SignatureTypeResource         string = "resource"
	SignatureTypeApplyingResource string = "applyingResource"
	SignatureTypePatch            string = "patch"
	// SignatureTypeHelm string = "helm"
)

const (
	// StatePending means CRD instance is created; Pod info has been updated into CRD instance;
	// Pod has been accepted by the system, but one or more of the containers has not been started.
	StatePending string = "Pending"
	// StateRunning means Pod has been bound to a node and all of the containers have been started.
	StateRunning string = "Running"
	// StateSucceeded means that all containers in the Pod have voluntarily terminated with a container
	// exit code of 0, and the system is not going to restart any of these containers.
	StateSucceeded string = "Succeeded"
	// StateFailed means that all containers in the Pod have terminated, and at least one container has
	// terminated in a failure (exited with a non-zero exit code or was stopped by the system).
	StateFailed string = "Failed"
)

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +resource:path=resourcesignature,scope=Namespaced

// ResourceSignature is the CRD. Use this command to generate deepcopy for it:
// ./k8s.io/code-generator/generate-groups.sh all github.com/IBM/pas-client-go/pkg/crd/packageadmissionsignature/v1/apis github.com/IBM/pas-client-go/pkg/crd/ "packageadmissionsignature:v1"
// For more details of code-generator, please visit https://github.com/kubernetes/code-generator
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// ResourceSignature is the CRD. Use this command to generate deepcopy for it:
type ResourceSignature struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	metav1.ObjectMeta `json:"metadata"`
	// Specification of the desired behavior of ResourceSignature.
	Spec ResourceSignatureSpec `json:"spec"`
	// Observed status of ResourceSignature.
	Status ResourceSignatureStatus `json:"status"`
}

func (ss *ResourceSignature) FindMessage(apiVersion, kind, name, namespace string) (string, bool) {
	si, _, found := ss.FindSignItem(apiVersion, kind, name, namespace)
	if found {
		return si.Message, true
	}
	return "", false
}

func (ss *ResourceSignature) FindSignature(apiVersion, kind, name, namespace string) (string, bool) {
	si, _, found := ss.FindSignItem(apiVersion, kind, name, namespace)
	if found {
		return si.Signature, true
	}
	return "", false
}

func (ss *ResourceSignature) FindSignItem(apiVersion, kind, name, namespace string) (*SignItem, []byte, bool) {
	signItem := &SignItem{}
	for _, si := range ss.Spec.Data {
		if found, singleYamlBytes := ishieldyaml.FindSingleYaml([]byte(si.Message), apiVersion, kind, name, namespace); found {
			return si, singleYamlBytes, true
		}
	}
	return signItem, nil, false
}

func (ss *ResourceSignature) Validate() (bool, string) {
	if ss == nil {
		return false, "ResourceSignature Validation failed. ss is nil."
	}
	if ss.Spec.Data == nil {
		return false, "ResourceSignature Validation failed. ss.Spec.Data is nil."
	}
	return true, ""
}

// ResourceSignatureSpec is a desired state description of ResourceSignature.
type ResourceSignatureSpec struct {
	Data []*SignItem `json:"data"`
}

// ResourceSignature describes the lifecycle status of ResourceSignature.
type ResourceSignatureStatus struct {
	State   string `json:"state"`
	Message string `json:"message"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ResourceSignatureList is a list of Workflow resources
type ResourceSignatureList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []*ResourceSignature `json:"items"`
}

func (ssl *ResourceSignatureList) FindMessage(apiVersion, kind, name, namespace string) (string, bool) {
	found, si, _, _ := ssl.FindSignItem(apiVersion, kind, name, namespace)
	if found {
		return si.Message, true
	}
	return "", false
}

func (ssl *ResourceSignatureList) FindSignature(apiVersion, kind, name, namespace string) (string, bool) {
	found, si, _, _ := ssl.FindSignItem(apiVersion, kind, name, namespace)
	if found {
		return si.Signature, true
	}
	return "", false
}

func (ssl *ResourceSignatureList) FindSignItem(apiVersion, kind, name, namespace string) (bool, *SignItem, []byte, string) {
	signItem := &SignItem{}
	for _, ss := range ssl.Items {
		if si, yamlBytes, ok := ss.FindSignItem(apiVersion, kind, name, namespace); ok {
			uid := string(ss.GetUID())
			return true, si, yamlBytes, uid
		}
	}
	return false, signItem, nil, ""
}

type SignItem struct {
	Message        string `json:"message,omitempty"`
	MessageScope   string `json:"messageScope,omitempty"`
	MutableAttrs   string `json:"mutableAttrs,omitempty"`
	Signature      string `json:"signature"`
	Certificate    string `json:"certificate"`
	SigStoreBundle string `json:"sigstoreBundle"`
	Type           string `json:"type"`
}

type ResourceInfo struct {
	ApiVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Name       string `json:"name,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	raw        []byte // raw yaml of single resource
}

func convert(m map[interface{}]interface{}) map[string]interface{} {
	res := map[string]interface{}{}
	for k, v := range m {
		switch v2 := v.(type) {
		case map[interface{}]interface{}:
			res[fmt.Sprint(k)] = convert(v2)
		default:
			res[fmt.Sprint(k)] = v
		}
	}
	return res
}

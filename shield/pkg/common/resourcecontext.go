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

package common

import (
	"encoding/json"
	"reflect"
	"strconv"

	log "github.com/sirupsen/logrus"
	gjson "github.com/tidwall/gjson"

	logger "github.com/IBM/integrity-enforcer/shield/pkg/util/logger"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type ResourceContext struct {
	ResourceScope   string          `json:"resourceScope,omitempty"`
	RawObject       []byte          `json:"-"`
	Namespace       string          `json:"namespace"`
	Name            string          `json:"name"`
	ApiGroup        string          `json:"apiGroup"`
	ApiVersion      string          `json:"apiVersion"`
	Kind            string          `json:"kind"`
	ClaimedMetadata *ObjectMetadata `json:"claimedMetadata"`
	ObjLabels       string          `json:"objLabels"`
	ObjMetaName     string          `json:"objMetaName"`
}

func (resc *ResourceContext) ResourceRef() *ResourceRef {
	gv := schema.GroupVersion{
		Group:   resc.ApiGroup,
		Version: resc.ApiVersion,
	}
	return &ResourceRef{
		Name:       resc.Name,
		Namespace:  resc.Namespace,
		Kind:       resc.Kind,
		ApiVersion: gv.String(),
	}
}

func (resc *ResourceContext) Map() map[string]string {
	m := map[string]string{}
	v := reflect.Indirect(reflect.ValueOf(resc))
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := v.Field(i)
		itf := f.Interface()
		if value, ok := itf.(string); ok {
			filedName := t.Field(i).Name
			m[filedName] = value
		} else {
			continue
		}
	}
	return m
}

func (resc *ResourceContext) Info(m map[string]string) string {
	if m == nil {
		m = map[string]string{}
	}
	m["kind"] = resc.Kind
	m["scope"] = resc.ResourceScope
	m["namespace"] = resc.Namespace
	m["name"] = resc.Name
	infoBytes, _ := json.Marshal(m)
	return string(infoBytes)
}

func (resc *ResourceContext) GroupVersion() string {
	return schema.GroupVersion{Group: resc.ApiGroup, Version: resc.ApiVersion}.String()
}

func (rc *ResourceContext) IsSecret() bool {
	return rc.Kind == "Secret" && rc.GroupVersion() == "v1"
}

func (rc *ResourceContext) IsServiceAccount() bool {
	return rc.Kind == "ServiceAccount" && rc.GroupVersion() == "v1"
}

func (rc *ResourceContext) ExcludeDiffValue() bool {
	if rc.Kind == "Secret" {
		return true
	}
	return false
}

type ParsedResource struct {
	JsonStr string
}

func NewParsedResource(resource *unstructured.Unstructured) *ParsedResource {
	var pr = &ParsedResource{}
	if resBytes, err := json.Marshal(resource); err != nil {
		logger.WithFields(log.Fields{
			"err": err,
		}).Warn("Error when unmarshaling resource object ")

	} else {
		pr.JsonStr = string(resBytes)
	}
	return pr
}

func (pr *ParsedResource) getValue(path string) string {
	var v string
	if w := gjson.Get(pr.JsonStr, path); w.Exists() {
		v = w.String()
	}
	return v
}

func (pr *ParsedResource) getArrayValue(path string) []string {
	var v []string
	if w := gjson.Get(pr.JsonStr, path); w.Exists() {
		x := w.Array()
		for _, xi := range x {
			v = append(v, xi.String())
		}
	}
	return v
}

func (pr *ParsedResource) getAnnotations(path string) *ResourceAnnotation {
	var r map[string]string = map[string]string{}
	if w := gjson.Get(pr.JsonStr, path); w.Exists() {
		m := w.Map()
		for k := range m {
			v := m[k]
			r[k] = v.String()
		}
	}
	return &ResourceAnnotation{
		values: r,
	}
}

func (pr *ParsedResource) getLabels(path string) *ResourceLabel {
	var r map[string]string = map[string]string{}
	if w := gjson.Get(pr.JsonStr, path); w.Exists() {
		m := w.Map()
		for k := range m {
			v := m[k]
			r[k] = v.String()
		}
	}
	return &ResourceLabel{
		values: r,
	}
}

func (pr *ParsedResource) getBool(path string, defaultValue bool) bool {
	if w := gjson.Get(pr.JsonStr, path); w.Exists() {
		v := w.String()
		if b, err := strconv.ParseBool(v); err != nil {
			return defaultValue
		} else {
			return b
		}
	}
	return defaultValue
}

func NewResourceContext(res *unstructured.Unstructured) *ResourceContext {

	pr := NewParsedResource(res)

	name := pr.getValue("name")
	if name == "" {
		name = pr.getValue("metadata.name")
	}

	namespace := pr.getValue("namespace")
	if namespace == "" {
		namespace = pr.getValue("metadata.namespace")
	}

	claimedMetadata := &ObjectMetadata{
		Annotations: pr.getAnnotations("metadata.annotations"),
		Labels:      pr.getLabels("metadata.labels"),
	}
	metaLabelObj := claimedMetadata.Labels
	labelsBytes, _ := json.Marshal(metaLabelObj.values)
	labelsStr := ""
	if labelsBytes != nil {
		labelsStr = string(labelsBytes)
	}

	kind := pr.getValue("kind")
	groupVersion := pr.getValue("apiVersion")
	gv, _ := schema.ParseGroupVersion(groupVersion)
	apiGroup := gv.Group
	apiVersion := gv.Version

	resourceScope := "Namespaced"
	if namespace == "" {
		resourceScope = "Cluster"
	}

	resBytes, _ := json.Marshal(res)

	rc := &ResourceContext{
		RawObject:       resBytes,
		ResourceScope:   resourceScope,
		Name:            name,
		ApiGroup:        apiGroup,
		ApiVersion:      apiVersion,
		Kind:            kind,
		Namespace:       namespace,
		ObjLabels:       labelsStr,
		ObjMetaName:     name,
		ClaimedMetadata: claimedMetadata,
	}
	return rc

}

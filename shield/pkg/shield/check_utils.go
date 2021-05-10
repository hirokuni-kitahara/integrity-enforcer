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

package shield

import (
	"context"
	"fmt"
	"strings"
	"time"

	rspapi "github.com/IBM/integrity-enforcer/shield/pkg/apis/resourcesigningprofile/v1alpha1"
	sigconfapi "github.com/IBM/integrity-enforcer/shield/pkg/apis/signerconfig/v1alpha1"
	rspclient "github.com/IBM/integrity-enforcer/shield/pkg/client/resourcesigningprofile/clientset/versioned/typed/resourcesigningprofile/v1alpha1"
	"github.com/IBM/integrity-enforcer/shield/pkg/util/kubeutil"

	common "github.com/IBM/integrity-enforcer/shield/pkg/common"
	config "github.com/IBM/integrity-enforcer/shield/pkg/config"
	admv1 "k8s.io/api/admission/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func createAdmissionResponse(allowed bool, msg string, vreqc *common.VRequestContext, vreqobj *common.VRequestObject, ctx *CheckContext, conf *config.ShieldConfig) *admv1.AdmissionResponse {
	var patchBytes []byte
	if conf.PatchEnabled(vreqc) {
		// `patchBytes` will be nil if no patch
		patchBytes = generatePatchBytes(vreqc, vreqobj, ctx)
	}
	responseMessage := fmt.Sprintf("%s (Request: %s)", msg, vreqc.Info(nil))
	resp := &admv1.AdmissionResponse{
		Allowed: allowed,
		Result: &metav1.Status{
			Message: responseMessage,
		},
	}
	if patchBytes != nil {
		patchType := admv1.PatchTypeJSONPatch
		resp.Patch = patchBytes
		resp.PatchType = &patchType
	}
	return resp
}

func createOrUpdateEvent(vreqc *common.VRequestContext, ctx *CheckContext, sconfig *config.ShieldConfig, denyRSP *rspapi.ResourceSigningProfile) error {
	config, err := kubeutil.GetKubeConfig()
	if err != nil {
		return err
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	resultStr := "deny"
	eventResult := common.EventResultValueDeny
	if ctx.Allow {
		resultStr = "allow"
		eventResult = common.EventResultValueAllow
	}

	sourceName := "IntegrityShield"
	evtName := fmt.Sprintf("ishield-%s-%s-%s-%s", resultStr, strings.ToLower(vreqc.Operation), strings.ToLower(vreqc.Kind), vreqc.Name)

	evtNamespace := vreqc.Namespace
	involvedObject := v1.ObjectReference{
		Namespace:  vreqc.Namespace,
		APIVersion: vreqc.GroupVersion(),
		Kind:       vreqc.Kind,
		Name:       vreqc.Name,
	}
	if vreqc.ResourceScope == "Cluster" {
		evtNamespace = sconfig.Namespace
		involvedObject = v1.ObjectReference{
			Namespace:  sconfig.Namespace,
			APIVersion: common.IShieldCustomResourceAPIVersion,
			Kind:       common.IShieldCustomResourceKind,
			Name:       sconfig.IShieldCRName,
		}
	}

	now := time.Now()
	evt := &v1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name: evtName,
			Annotations: map[string]string{
				common.EventTypeAnnotationKey:   common.EventTypeValueVerifyResult,
				common.EventResultAnnotationKey: eventResult,
			},
		},
		InvolvedObject:      involvedObject,
		Type:                sourceName,
		Source:              v1.EventSource{Component: sourceName},
		ReportingController: sourceName,
		ReportingInstance:   evtName,
		Action:              evtName,
		FirstTimestamp:      metav1.NewTime(now),
	}
	isExistingEvent := false
	current, getErr := client.CoreV1().Events(evtNamespace).Get(context.Background(), evtName, metav1.GetOptions{})
	if current != nil && getErr == nil {
		isExistingEvent = true
		evt = current
	}

	rspInfo := ""
	if denyRSP != nil {
		rspInfo = fmt.Sprintf(" (RSP `namespace: %s, name: %s`)", denyRSP.GetNamespace(), denyRSP.GetName())
	}
	responseMessage := fmt.Sprintf("Result: %s, Reason: \"%s\"%s, Request: %s", resultStr, ctx.Message, rspInfo, vreqc.Info(nil))
	tmpMessage := fmt.Sprintf("[IntegrityShieldEvent] %s", responseMessage)
	// Event.Message can have 1024 chars at most
	if len(tmpMessage) > 1024 {
		tmpMessage = tmpMessage[:950] + " ... Trimmed. `Event.Message` can have 1024 chars at maximum."
	}
	evt.Message = tmpMessage
	evt.Reason = common.ReasonCodeMap[ctx.ReasonCode].Code
	evt.Count = evt.Count + 1
	evt.EventTime = metav1.NewMicroTime(now)
	evt.LastTimestamp = metav1.NewTime(now)

	if isExistingEvent {
		_, err = client.CoreV1().Events(evtNamespace).Update(context.Background(), evt, metav1.UpdateOptions{})
	} else {
		_, err = client.CoreV1().Events(evtNamespace).Create(context.Background(), evt, metav1.CreateOptions{})
	}
	if err != nil {
		return err
	}
	return nil
}

func updateRSPStatus(rsp *rspapi.ResourceSigningProfile, vreqc *common.VRequestContext, errMsg string) error {
	if rsp == nil {
		return nil
	}

	config, err := kubeutil.GetKubeConfig()
	if err != nil {
		return err
	}
	client, err := rspclient.NewForConfig(config)
	if err != nil {
		return err
	}

	rspNamespace := rsp.GetNamespace()
	rspName := rsp.GetName()
	rspOrg, err := client.ResourceSigningProfiles(rspNamespace).Get(context.Background(), rspName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	req := common.NewRequestFromReqContext(vreqc)
	rspNew := rspOrg.UpdateStatus(req, errMsg)

	_, err = client.ResourceSigningProfiles(rspNamespace).Update(context.Background(), rspNew, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	return nil
}

func checkIfProfileTargetNamespace(reqNamespace, shieldNamespace string, data *RunData) bool {
	ruleTable := data.GetRuleTable(shieldNamespace)
	if ruleTable == nil {
		return false
	}
	return ruleTable.CheckIfTargetNamespace(reqNamespace)
}

func checkIfInScopeNamespace(reqNamespace string, config *config.ShieldConfig) bool {
	inScopeNSSelector := config.InScopeNamespaceSelector
	if inScopeNSSelector == nil {
		return false
	}
	return inScopeNSSelector.MatchNamespaceName(reqNamespace)
}

func checkIfDryRunAdmission(vreqc *common.VRequestContext) bool {
	return vreqc.DryRun
}

func checkIfUnprocessedInIShield(reqFeilds map[string]string, config *config.ShieldConfig) bool {
	for _, d := range config.Ignore {
		if d.Match(reqFeilds) {
			return true
		}
	}
	return false
}

func getRequestNamespace(req *admv1.AdmissionRequest) string {
	reqNamespace := ""
	if req.Kind.Kind != "Namespace" && req.Namespace != "" {
		reqNamespace = req.Namespace
	}
	return reqNamespace
}

func getRequestNamespaceFromVRequestContext(vreqc *common.VRequestContext) string {
	reqNamespace := ""
	if vreqc.Kind != "Namespace" && vreqc.Namespace != "" {
		reqNamespace = vreqc.Namespace
	}
	return reqNamespace
}

func getRequestNamespaceFromV2ResourceContext(v2resc *common.V2ResourceContext) string {
	reqNamespace := ""
	if v2resc.Kind != "Namespace" && v2resc.Namespace != "" {
		reqNamespace = v2resc.Namespace
	}
	return reqNamespace
}

func checkIfIShieldAdminRequest(vreqc *common.VRequestContext, config *config.ShieldConfig) bool {
	groupMatched := false
	if config.IShieldAdminUserGroup != "" {
		groupMatched = common.MatchPatternWithArray(config.IShieldAdminUserGroup, vreqc.UserGroups)
	}
	userMatched := false
	if config.IShieldAdminUserName != "" {
		userMatched = common.MatchPattern(config.IShieldAdminUserName, vreqc.UserName)
	}
	// TODO: delete this block after OLM SA will be added to `config.IShieldAdminUserName` in CR
	if common.MatchPattern("system:serviceaccount:openshift-operator-lifecycle-manager:olm-operator-serviceaccount", vreqc.UserName) {
		userMatched = true
	}
	isAdmin := (groupMatched || userMatched)
	return isAdmin
}

func checkIfIShieldServerRequest(vreqc *common.VRequestContext, config *config.ShieldConfig) bool {
	return common.MatchPattern(config.IShieldServerUserName, vreqc.UserName) //"service account for integrity-shield"
}

func checkIfIShieldOperatorRequest(vreqc *common.VRequestContext, config *config.ShieldConfig) bool {
	return common.ExactMatch(config.IShieldResourceCondition.OperatorServiceAccount, vreqc.UserName) //"service account for integrity-shield-operator"
}

func checkIfGarbageCollectorRequest(vreqc *common.VRequestContext) bool {
	// TODO: should be configurable?
	return vreqc.UserName == "system:serviceaccount:kube-system:generic-garbage-collector"
}

func checkIfSpecialServiceAccountRequest(vreqc *common.VRequestContext) bool {
	// TODO: should be configurable?
	if strings.HasPrefix(vreqc.UserName, "system:serviceaccount:kube-") {
		return true
	} else if strings.HasPrefix(vreqc.UserName, "system:serviceaccount:openshift-") {
		return true
	} else if strings.HasPrefix(vreqc.UserName, "system:serviceaccount:openshift:") {
		return true
	} else if strings.HasPrefix(vreqc.UserName, "system:serviceaccount:open-cluster-") {
		return true
	} else if strings.HasPrefix(vreqc.UserName, "system:serviceaccount:olm:") {
		return true
	}

	return false
}

func getBreakGlassConditions(signerConfig *sigconfapi.SignerConfig) []common.BreakGlassCondition {
	conditions := []common.BreakGlassCondition{}
	if signerConfig != nil {
		conditions = append(conditions, signerConfig.Spec.Config.BreakGlass...)
	}
	return conditions
}

func checkIfBreakGlassEnabled(vreqc *common.VRequestContext, signerConfig *sigconfapi.SignerConfig) bool {

	conditions := getBreakGlassConditions(signerConfig)
	breakGlassEnabled := false
	if vreqc.ResourceScope == "Namespaced" {
		reqNs := vreqc.Namespace
		for _, d := range conditions {
			if d.Scope == common.ScopeUndefined || d.Scope == common.ScopeNamespaced {
				for _, ns := range d.Namespaces {
					if reqNs == ns {
						breakGlassEnabled = true
						break
					}
				}
			}
			if breakGlassEnabled {
				break
			}
		}
	} else {
		for _, d := range conditions {
			if d.Scope == common.ScopeCluster {
				breakGlassEnabled = true
				break
			}
		}
	}
	return breakGlassEnabled
}

func checkIfDetectOnly(sconf *config.ShieldConfig) bool {
	return (sconf.Mode == config.DetectMode)
}

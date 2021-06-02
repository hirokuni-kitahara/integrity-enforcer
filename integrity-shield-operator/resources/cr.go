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

package resources

import (
	"fmt"
	"io/ioutil"
	"path/filepath"

	apiv1alpha1 "github.com/IBM/integrity-enforcer/integrity-shield-operator/api/v1alpha1"
	rsp "github.com/IBM/integrity-enforcer/shield/pkg/apis/resourcesigningprofile/v1alpha1"
	ec "github.com/IBM/integrity-enforcer/shield/pkg/apis/shieldconfig/v1alpha1"
	"github.com/IBM/integrity-enforcer/shield/pkg/common"
	econf "github.com/IBM/integrity-enforcer/shield/pkg/config"
	"github.com/ghodss/yaml"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("controller_integrityshield")

// shield config cr
func BuildShieldConfigForIShield(cr *apiv1alpha1.IntegrityShield, scheme *runtime.Scheme, commonProfileYamlPathList []string) *ec.ShieldConfig {

	ecc := &ec.ShieldConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.GetShieldConfigCRName(),
			Namespace: cr.Namespace,
		},
		Spec: ec.ShieldConfigSpec{
			ShieldConfig: cr.Spec.ShieldConfig,
		},
	}
	if ecc.Spec.ShieldConfig.Namespace == "" {
		ecc.Spec.ShieldConfig.Namespace = cr.Namespace
	}
	if ecc.Spec.ShieldConfig.SignatureNamespace == "" {
		ecc.Spec.ShieldConfig.SignatureNamespace = cr.Namespace
	}
	if ecc.Spec.ShieldConfig.ProfileNamespace == "" {
		ecc.Spec.ShieldConfig.ProfileNamespace = cr.Namespace
	}
	if ecc.Spec.ShieldConfig.IShieldCRName == "" {
		ecc.Spec.ShieldConfig.IShieldCRName = cr.Name
	}
	if ecc.Spec.ShieldConfig.IShieldServerUserName == "" {
		ecc.Spec.ShieldConfig.IShieldServerUserName = fmt.Sprintf("system:serviceaccount:%s:%s", cr.Namespace, cr.GetServiceAccountName())
	}
	operatorSA := getOperatorServiceAccount()

	iShieldOperatorResources, iShieldServerResources := cr.GetIShieldResourceList(scheme)

	ecc.Spec.ShieldConfig.IShieldResourceCondition = &econf.IShieldResourceCondition{
		OperatorResources:      iShieldOperatorResources,
		ServerResources:        iShieldServerResources,
		OperatorServiceAccount: operatorSA,
	}
	if ecc.Spec.ShieldConfig.CommonProfile == nil {
		commonProfile := &common.CommonProfile{}

		for _, presetPath := range commonProfileYamlPathList {
			var tmpProfile *common.CommonProfile
			fpath := filepath.Clean(presetPath)
			tmpProfileBytes, _ := ioutil.ReadFile(fpath) // NOSONAR
			err := yaml.Unmarshal(tmpProfileBytes, &tmpProfile)
			if err != nil {
				reqLogger := log.WithValues("BuildShieldConfigForIShield", cr.GetShieldConfigCRName())
				reqLogger.Error(err, fmt.Sprintf("Failed to load preset CommonProfile from file `%s`", fpath))
			}
			commonProfile.IgnoreRules = append(commonProfile.IgnoreRules, tmpProfile.IgnoreRules...)
			commonProfile.IgnoreAttrs = append(commonProfile.IgnoreAttrs, tmpProfile.IgnoreAttrs...)
		}

		if operatorSA != "" {
			// add IShield operator SA to ignoreRules in commonProfile
			operatorSAPattern := common.RulePattern(operatorSA)
			ignoreRules := commonProfile.IgnoreRules

			ignoreRules = append(ignoreRules, &common.Rule{Match: []*common.RequestPatternWithNamespace{{RequestPattern: &common.RequestPattern{UserName: &operatorSAPattern}}}})
			commonProfile.IgnoreRules = ignoreRules
		}

		for _, ir := range cr.Spec.IgnoreRules {
			tmpRule := ir
			commonProfile.IgnoreRules = append(commonProfile.IgnoreRules, &tmpRule)
		}
		for _, ia := range cr.Spec.IgnoreAttrs {
			tmpAttr := ia
			commonProfile.IgnoreAttrs = append(commonProfile.IgnoreAttrs, &tmpAttr)
		}

		ecc.Spec.ShieldConfig.CommonProfile = commonProfile
	}

	return ecc
}

func BuildResourceSigningProfileForIShield(cr *apiv1alpha1.IntegrityShield, prof *apiv1alpha1.ProfileConfig) *rsp.ResourceSigningProfile {
	rspfromcr := &rsp.ResourceSigningProfile{}
	rspfromcr.Spec = *(prof.ResourceSigningProfileSpec)
	rspfromcr.ObjectMeta.Name = prof.Name
	rspfromcr.ObjectMeta.Namespace = cr.Namespace
	return rspfromcr
}

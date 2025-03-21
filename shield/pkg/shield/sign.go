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
	"encoding/json"
	"fmt"

	vrsig "github.com/IBM/integrity-enforcer/shield/pkg/apis/resourcesignature/v1alpha1"
	rspapi "github.com/IBM/integrity-enforcer/shield/pkg/apis/resourcesigningprofile/v1alpha1"
	common "github.com/IBM/integrity-enforcer/shield/pkg/common"
	config "github.com/IBM/integrity-enforcer/shield/pkg/config"
	helm "github.com/IBM/integrity-enforcer/shield/pkg/plugins/helm"
	"github.com/IBM/integrity-enforcer/shield/pkg/util/kubeutil"
	logger "github.com/IBM/integrity-enforcer/shield/pkg/util/logger"
	pgp "github.com/IBM/integrity-enforcer/shield/pkg/util/sign/pgp"
	"github.com/IBM/integrity-enforcer/shield/pkg/util/sign/sigstore"
	x509 "github.com/IBM/integrity-enforcer/shield/pkg/util/sign/x509"
	ishieldyaml "github.com/IBM/integrity-enforcer/shield/pkg/util/yaml"
)

type SignedResourceType string

const (
	SignedResourceTypeUnknown          SignedResourceType = ""
	SignedResourceTypeResource         SignedResourceType = "Resource"
	SignedResourceTypeApplyingResource SignedResourceType = "ApplyingResource"
	SignedResourceTypePatch            SignedResourceType = "Patch"
	SignedResourceTypeHelm             SignedResourceType = "Helm"
)

/**********************************************

                GeneralSignature

***********************************************/

type GeneralSignature struct {
	SignType SignedResourceType
	data     map[string]string
	option   map[string]bool
}

/**********************************************

                Signature

***********************************************/

type SignatureEvaluator interface {
	Eval(resc *common.ResourceContext, resSigList *vrsig.ResourceSignatureList, signingProfile rspapi.ResourceSigningProfile) (*common.SignatureEvalResult, error)
}

type ConcreteSignatureEvaluator struct {
	config       *config.ShieldConfig
	signerConfig *common.SignerConfig
	plugins      map[string]bool
}

func NewSignatureEvaluator(config *config.ShieldConfig, signerConfig *common.SignerConfig, plugins map[string]bool) (SignatureEvaluator, error) {
	return &ConcreteSignatureEvaluator{
		config:       config,
		signerConfig: signerConfig,
		plugins:      plugins,
	}, nil
}

func (self *ConcreteSignatureEvaluator) GetResourceSignature(ref *common.ResourceRef, resc *common.ResourceContext, resSigList *vrsig.ResourceSignatureList) *GeneralSignature {

	sigAnnotations := resc.ClaimedMetadata.Annotations.SignatureAnnotations()

	//1. pick ResourceSignature from metadata.annotation if available
	if sigAnnotations.Signature != "" {
		found, yamlBytes := ishieldyaml.FindSingleYaml([]byte(sigAnnotations.Message), ref.ApiVersion, ref.Kind, ref.Name, ref.Namespace)
		if found {
			message := ishieldyaml.Base64decode(sigAnnotations.Message)
			message = ishieldyaml.Decompress(message)
			messageScope := sigAnnotations.MessageScope
			mutableAttrs := sigAnnotations.MutableAttrs
			matchRequired := true
			scopedSignature := false
			if message == "" && messageScope != "" {
				message = GenerateMessageFromRawObj(resc.RawObject, messageScope, mutableAttrs)
				matchRequired = false  // skip matching because the message is generated from Requested Object
				scopedSignature = true // enable checking if the signature is for patch
			}
			signature := ishieldyaml.Base64decode(sigAnnotations.Signature)
			certificate := ishieldyaml.Base64decode(sigAnnotations.Certificate)
			certificate = ishieldyaml.Decompress(certificate)
			sigstoreBundle := ""
			if sigAnnotations.SigStoreBundle != "" {
				sigstoreBundle = ishieldyaml.Base64decode(sigAnnotations.SigStoreBundle)
				sigstoreBundle = ishieldyaml.Decompress(sigstoreBundle)
			}
			signType := SignedResourceTypeResource
			if sigAnnotations.SignatureType == vrsig.SignatureTypeApplyingResource {
				signType = SignedResourceTypeApplyingResource
			} else if sigAnnotations.SignatureType == vrsig.SignatureTypePatch {
				signType = SignedResourceTypePatch
			}
			return &GeneralSignature{
				SignType: signType,
				data:     map[string]string{"signature": signature, "message": message, "certificate": certificate, "yamlBytes": string(yamlBytes), "scope": messageScope, "sigstoreBundle": sigstoreBundle},
				option:   map[string]bool{"matchRequired": matchRequired, "scopedSignature": scopedSignature},
			}
		}
	}

	//2. pick ResourceSignature from custom resource if available
	if resSigList != nil && len(resSigList.Items) > 0 {
		found, si, yamlBytes, resSigUID := resSigList.FindSignItem(ref.ApiVersion, ref.Kind, ref.Name, ref.Namespace)
		if found {
			signature := ishieldyaml.Base64decode(si.Signature)
			certificate := ishieldyaml.Base64decode(si.Certificate)
			certificate = ishieldyaml.Decompress(certificate)
			sigstoreBundle := ""
			if si.SigStoreBundle != "" {
				sigstoreBundle = ishieldyaml.Base64decode(si.SigStoreBundle)
				sigstoreBundle = ishieldyaml.Decompress(sigstoreBundle)
			}
			message := ishieldyaml.Base64decode(si.Message)
			message = ishieldyaml.Decompress(message)
			mutableAttrs := si.MutableAttrs
			matchRequired := true
			scopedSignature := false
			if si.Message == "" && si.MessageScope != "" {
				message = GenerateMessageFromRawObj(resc.RawObject, si.MessageScope, mutableAttrs)
				matchRequired = false  // skip matching because the message is generated from Requested Object
				scopedSignature = true // enable checking if the signature is for patch
			}
			signType := SignedResourceTypeResource
			if si.Type == vrsig.SignatureTypeApplyingResource {
				signType = SignedResourceTypeApplyingResource
			} else if si.Type == vrsig.SignatureTypePatch {
				signType = SignedResourceTypePatch
			}
			return &GeneralSignature{
				SignType: signType,
				data:     map[string]string{"signature": signature, "message": message, "certificate": certificate, "yamlBytes": string(yamlBytes), "scope": si.MessageScope, "sigstoreBundle": sigstoreBundle, "resourceSignatureUID": resSigUID},
				option:   map[string]bool{"matchRequired": matchRequired, "scopedSignature": scopedSignature},
			}
		}
	}

	//3. pick ResourceSignature from external store if available

	//4. helm resource (release secret, helm cahrt resources)
	if ok := self.plugins["helm"]; ok {
		rsecBytes, err := helm.FindReleaseSecret(resc.Namespace, resc.Kind, resc.Name, resc.RawObject)
		if err != nil {
			logger.Error(fmt.Sprintf("Error occured in finding helm release secret; %s", err.Error()))
			return nil
		}
		if rsecBytes != nil {
			hrmSigs, err := helm.GetHelmReleaseMetadata(rsecBytes)
			if err == nil && len(hrmSigs) == 2 {
				rls := hrmSigs[0]
				hrm := hrmSigs[1]
				eCfg := true

				return &GeneralSignature{
					SignType: SignedResourceTypeHelm,
					data:     map[string]string{"releaseSecret": rls, "helmReleaseMetadata": hrm},
					option:   map[string]bool{"emptyConfig": eCfg, "matchRequired": true},
				}
			} else {
				logger.Error(fmt.Sprintf("Error occured in getting signature from helm release metadata; %s", err.Error()))
				return nil

			}
		}
	}
	return nil

	//5. return nil if no signature found
	// return nil
}

func (self *ConcreteSignatureEvaluator) Eval(resc *common.ResourceContext, resSigList *vrsig.ResourceSignatureList, signingProfile rspapi.ResourceSigningProfile) (*common.SignatureEvalResult, error) {

	// eval sign policy
	ref := resc.ResourceRef()

	// override ref name if there is kustomize pattern for this
	kustPatterns := signingProfile.Kustomize(resc.Map())
	if len(kustPatterns) > 0 {
		ref = kustPatterns[0].Override(ref)
	}

	// find signature
	rsig := self.GetResourceSignature(ref, resc, resSigList)
	if rsig == nil {
		return &common.SignatureEvalResult{
			Allow:   false,
			Checked: true,
			Error: &common.CheckError{
				Reason: common.ReasonCodeMap[common.REASON_NO_SIG].Message,
			},
		}, nil
	}
	rsigUID := rsig.data["resourceSignatureUID"] // this will be empty string if annotation signature

	candidatePubkeys := self.signerConfig.GetCandidatePubkeys(self.config.KeyPathList, resc.Namespace)
	pgpPubkeys := candidatePubkeys[common.SignatureTypePGP]
	x509Certs := candidatePubkeys[common.SignatureTypeX509]
	sigstoreCerts := candidatePubkeys[common.SignatureTypeSigStore]

	keyLoadingError := false
	candidateKeyCount := len(pgpPubkeys) + len(x509Certs)
	if candidateKeyCount > 0 {
		validKeyCount := 0
		for _, keyPath := range pgpPubkeys {
			if loaded, _ := pgp.LoadKeyRing(keyPath); len(loaded) > 0 {
				validKeyCount += 1
			}
		}

		for _, certDir := range x509Certs {
			if loaded, _ := x509.LoadCertDir(certDir); len(loaded) > 0 {
				validKeyCount += 1
			}
		}

		for _, certPath := range sigstoreCerts {
			if loaded, _ := sigstore.LoadCert(certPath); len(loaded) > 0 {
				validKeyCount += 1
			}
		}
		if validKeyCount == 0 {
			keyLoadingError = true
		}
	}

	sigstoreEnabled := self.config.SigStoreEnabled()

	// create verifier
	dryRunNamespace := ""
	if resc.ResourceScope == string(common.ScopeNamespaced) {
		dryRunNamespace = self.config.Namespace
	}
	verifier := NewVerifier(rsig.SignType, dryRunNamespace, pgpPubkeys, x509Certs, sigstoreCerts, self.config.KeyPathList, sigstoreEnabled)

	// if this verification is not executed in a K8s pod (e.g. using ishieldctl command), then try loading secrets for pubkeys
	if !kubeutil.IsInCluster() {
		err := verifier.LoadSecrets(self.config.Namespace)
		if err != nil {
			logger.Warn("Error while loading pubkey secrets;", err.Error())
		}
	}

	// verify signature
	sigVerifyResult, verifiedKeyPathList, err := verifier.Verify(rsig, resc, signingProfile)
	if err != nil {
		reasonFail := fmt.Sprintf("Error during signature verification; %s; %s", sigVerifyResult.Error.Reason, err.Error())
		return &common.SignatureEvalResult{
			Allow:   false,
			Checked: true,
			Error: &common.CheckError{
				Error:  err,
				Reason: reasonFail,
			},
			ResourceSignatureUID: rsigUID,
		}, nil
	}

	if keyLoadingError {
		reasonFail := common.ReasonCodeMap[common.REASON_NO_VALID_KEYRING].Message
		return &common.SignatureEvalResult{
			Allow:   false,
			Checked: true,
			Error: &common.CheckError{
				Reason: reasonFail,
			},
			ResourceSignatureUID: rsigUID,
		}, nil
	}

	if sigVerifyResult == nil || sigVerifyResult.Signer == nil {
		reasonFail := common.ReasonCodeMap[common.REASON_INVALID_SIG].Message
		if sigVerifyResult != nil && sigVerifyResult.Error != nil {
			reasonFail = fmt.Sprintf("%s; %s", reasonFail, sigVerifyResult.Error.Reason)
		}
		return &common.SignatureEvalResult{
			Allow:   false,
			Checked: true,
			Error: &common.CheckError{
				Reason: reasonFail,
			},
			ResourceSignatureUID: rsigUID,
		}, nil
	}

	// signer
	signer := sigVerifyResult.Signer

	// check signer config
	signerMatched, matchedSignerConfig := self.signerConfig.Match(resc.Namespace, signer, verifiedKeyPathList)
	if signerMatched {
		matchedSignerConfigStr := ""
		if matchedSignerConfig != nil {
			tmpMatchedConfig, _ := json.Marshal(matchedSignerConfig)
			matchedSignerConfigStr = string(tmpMatchedConfig)
		}
		return &common.SignatureEvalResult{
			Signer:               signer,
			SignerName:           signer.GetName(),
			Allow:                true,
			Checked:              true,
			MatchedSignerConfig:  matchedSignerConfigStr,
			Error:                nil,
			ResourceSignatureUID: rsigUID,
		}, nil
	} else {
		reasonFail := common.ReasonCodeMap[common.REASON_NO_MATCH_SIGNER_CONFIG].Message
		if signer != nil {
			reasonFail = fmt.Sprintf("%s; This resource is signed by %s", reasonFail, signer.GetNameWithFingerprint())
		}
		return &common.SignatureEvalResult{
			Signer:     signer,
			SignerName: signer.GetName(),
			Allow:      false,
			Checked:    true,
			Error: &common.CheckError{
				Reason: reasonFail,
			},
			ResourceSignatureUID: rsigUID,
		}, nil
	}
}

func findAttrsPattern(reqc *common.RequestContext, resc *common.ResourceContext, attrs []*common.AttrsPattern) []string {
	reqFields := resc.Map()
	masks := []string{}
	for _, attr := range attrs {
		if attr.MatchWith(reqFields) {
			masks = append(masks, attr.Attrs...)
		}
	}
	return masks
}

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

	rspapi "github.com/IBM/integrity-enforcer/shield/pkg/apis/resourcesigningprofile/v1alpha1"

	common "github.com/IBM/integrity-enforcer/shield/pkg/common"
)

/**********************************************

				DecisionResult

***********************************************/

type DecisionResult struct {
	Type       common.DecisionType `json:"type,omitempty"`
	Verified   bool                `json:"verified,omitempty"`
	ReasonCode int                 `json:"reasonCode,omitempty"`
	Message    string              `json:"message,omitempty"`

	denyRSP *rspapi.ResourceSigningProfile
}

func undeterminedDescision() *DecisionResult {
	return &DecisionResult{Type: common.DecisionUndetermined}
}

func (self *DecisionResult) String() string {
	drB, _ := json.Marshal(self)
	return string(drB)
}

func (self *DecisionResult) isAllowed() bool {
	return self.Type == common.DecisionAllow
}

func (self *DecisionResult) isDenied() bool {
	return self.Type == common.DecisionDeny
}

func (self *DecisionResult) isUndetermined() bool {
	return self.Type == common.DecisionUndetermined
}

func (self *DecisionResult) isErrorOccurred() bool {
	return self.Type == common.DecisionError
}

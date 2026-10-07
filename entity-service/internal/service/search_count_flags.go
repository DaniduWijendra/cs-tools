// Copyright (c) 2026 WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package service

import "github.com/wso2-open-operations/cs-tools/entity-service/internal/apierror"

// validateSearchCountFlags rejects a search that sets both skipTotal and
// countOnly. They ask for opposite halves of the work: skipTotal returns the
// page without counting, countOnly returns the count without the page, so a
// request that sets both would run neither query and answer with nothing.
func validateSearchCountFlags(skipTotal, countOnly bool) error {
	if skipTotal && countOnly {
		return &apierror.ValidationError{Msg: "skipTotal and countOnly cannot both be set: skipTotal returns the page without a total, countOnly returns the total without a page"}
	}
	return nil
}

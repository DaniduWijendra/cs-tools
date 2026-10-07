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

import type { BackendApi, BackendApiPostOptions } from "@api/backend/client";
import { createFlaggedPost } from "@api/backend/postFlaggedSearch";

const countOnly = createFlaggedPost("countOnly");

/** For tests: forget what an earlier request learned about the entity service. */
export const resetCountOnlySupport = countOnly.reset;

/**
 * `api.post` for a search whose caller reads nothing but `total` (a dashboard
 * count tile, a pie or bar slice): asks the server to run only the count
 * (`countOnly: true`), so the page query is skipped and only one pool
 * connection is held. The response still has the list field, but it is empty.
 * Falls back to the plain search if the entity service predates the field (see
 * {@link createFlaggedPost}). Only for endpoints that declare the field: the
 * others reject it, which would cost every request a failed attempt first.
 */
export function postCountOnly<TBody extends object, TResponse>(
  api: BackendApi,
  path: string,
  body: TBody,
  options?: BackendApiPostOptions,
): Promise<TResponse> {
  return countOnly.post<TBody, TResponse>(api, path, body, options);
}

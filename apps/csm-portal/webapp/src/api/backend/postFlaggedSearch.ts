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

/** A `BackendApiError` carries the HTTP status; matched by shape rather than
 * `instanceof` so this module needs nothing from the client at runtime. */
function isBadRequest(err: unknown): boolean {
  return err instanceof Error && (err as { status?: unknown }).status === 400;
}

export interface FlaggedPost {
  /**
   * `api.post` with the flag set to `true`, falling back to the same request
   * without it if the entity service predates the field. The fallback only
   * happens on a 400, and only counts as "unsupported" if the plain retry
   * succeeds, so a request that is simply invalid is reported as the error it
   * is and does not switch the flag off. Whatever the caller put in the body
   * under the flag's name is ignored: the helper alone decides.
   */
  post<TBody extends object, TResponse>(
    api: BackendApi,
    path: string,
    body: TBody,
    options?: BackendApiPostOptions,
  ): Promise<TResponse>;
  /** For tests: forget what an earlier request learned about the entity service. */
  reset(): void;
}

/**
 * Builds a `post` for a search request flag that only newer entity services
 * accept. The entity service rejects request fields it does not declare (a
 * 400), so a portal that is deployed before the entity service it talks to
 * must not strand the searches that use the flag. Support is learned once per
 * page load: the first time such a rejection is seen the flag is simply not
 * sent for the rest of the session. One instance per flag, so learning that
 * one flag is missing says nothing about another.
 */
export function createFlaggedPost(flag: string): FlaggedPost {
  let accepted = true;

  // The request options are only passed on when the caller gave some, so a
  // caller with none keeps the exact two-argument `api.post` call it always
  // made.
  const send = <TBody, TResponse>(
    api: BackendApi,
    path: string,
    body: TBody,
    options: BackendApiPostOptions | undefined,
  ): Promise<TResponse> =>
    options === undefined
      ? api.post<TBody, TResponse>(path, body)
      : api.post<TBody, TResponse>(path, body, options);

  return {
    async post<TBody extends object, TResponse>(
      api: BackendApi,
      path: string,
      body: TBody,
      options?: BackendApiPostOptions,
    ): Promise<TResponse> {
      const plain = { ...body } as Record<string, unknown>;
      delete plain[flag];
      if (!accepted) {
        return send<TBody, TResponse>(api, path, plain as TBody, options);
      }
      try {
        return await send<TBody, TResponse>(api, path, { ...plain, [flag]: true } as TBody, options);
      } catch (err) {
        if (!isBadRequest(err)) throw err;
        const response = await send<TBody, TResponse>(api, path, plain as TBody, options);
        accepted = false;
        return response;
      }
    },
    reset() {
      accepted = true;
    },
  };
}

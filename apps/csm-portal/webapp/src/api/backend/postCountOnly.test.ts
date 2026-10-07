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

import { beforeEach, describe, expect, it, vi } from "vitest";
import type { BackendApi } from "@api/backend/client";
import { postCountOnly, resetCountOnlySupport } from "@api/backend/postCountOnly";
import { postSkippingTotal, resetSkipTotalSupport } from "@api/backend/postSkippingTotal";

/** What `BackendApiError` looks like to the helper: an Error with an HTTP status. */
class BackendApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

const post = vi.fn();
const api = { post } as unknown as BackendApi;
const body: { countOnly?: boolean; pagination: { offset: number; limit: number }; filters: { states: string[] } } = {
  pagination: { offset: 0, limit: 1 },
  filters: { states: ["open"] },
};

describe("postCountOnly", () => {
  beforeEach(() => {
    post.mockReset();
    resetCountOnlySupport();
    resetSkipTotalSupport();
  });

  it("asks the server to run only the count and returns its response", async () => {
    post.mockResolvedValue({ cases: [], total: 12 });

    await expect(postCountOnly(api, "/cases/search", body)).resolves.toEqual({ cases: [], total: 12 });
    expect(post).toHaveBeenCalledTimes(1);
    expect(post).toHaveBeenCalledWith("/cases/search", { ...body, countOnly: true });
  });

  it("passes the request options (the abort signal) through, and only when it was given some", async () => {
    post.mockResolvedValue({});
    const signal = new AbortController().signal;

    await postCountOnly(api, "/cases/search", body, { signal });
    expect(post).toHaveBeenLastCalledWith("/cases/search", { ...body, countOnly: true }, { signal });

    await postCountOnly(api, "/cases/search", body);
    expect(post).toHaveBeenLastCalledWith("/cases/search", { ...body, countOnly: true });
  });

  it("always sets the flag itself, whatever the caller passed", async () => {
    post.mockResolvedValue({});
    await postCountOnly(api, "/cases/search", { ...body, countOnly: false });
    expect(post).toHaveBeenCalledWith("/cases/search", { ...body, countOnly: true });
  });

  it("falls back to the plain search, with the same options, when the entity service rejects the field, and remembers", async () => {
    const signal = new AbortController().signal;
    post
      .mockRejectedValueOnce(new BackendApiError(400, "unknown field countOnly"))
      .mockResolvedValue({ cases: [], total: 4 });

    await expect(postCountOnly(api, "/cases/search", body, { signal })).resolves.toEqual({ cases: [], total: 4 });
    expect(post).toHaveBeenCalledTimes(2);
    expect(post).toHaveBeenNthCalledWith(1, "/cases/search", { ...body, countOnly: true }, { signal });
    expect(post).toHaveBeenNthCalledWith(2, "/cases/search", body, { signal });

    // Learned: the next search does not try the flag first.
    await postCountOnly(api, "/cases/search", body, { signal });
    expect(post).toHaveBeenCalledTimes(3);
    expect(post).toHaveBeenLastCalledWith("/cases/search", body, { signal });
  });

  it("reports a request that is invalid either way as the error it is, and keeps asking", async () => {
    post.mockRejectedValue(new BackendApiError(400, "filters are invalid"));

    await expect(postCountOnly(api, "/cases/search", body)).rejects.toThrow("filters are invalid");
    expect(post).toHaveBeenCalledTimes(2);

    // The plain retry failed too, so the flag is not blamed: it is still tried.
    post.mockClear();
    post.mockResolvedValue({ total: 1 });
    await postCountOnly(api, "/cases/search", body);
    expect(post).toHaveBeenCalledTimes(1);
    expect(post).toHaveBeenCalledWith("/cases/search", { ...body, countOnly: true });
  });

  it("does not retry any other failure", async () => {
    post.mockRejectedValue(new BackendApiError(503, "unavailable"));
    await expect(postCountOnly(api, "/cases/search", body)).rejects.toThrow("unavailable");
    expect(post).toHaveBeenCalledTimes(1);
  });

  it("learning that countOnly is missing says nothing about skipTotal, and the other way round", async () => {
    post
      .mockRejectedValueOnce(new BackendApiError(400, "unknown field countOnly"))
      .mockResolvedValue({});
    await postCountOnly(api, "/cases/search", body);

    post.mockClear();
    const search: { skipTotal?: boolean; pagination: { offset: number; limit: number } } = {
      pagination: { offset: 0, limit: 5 },
    };
    await postSkippingTotal(api, "/cases/search", search);
    expect(post).toHaveBeenCalledTimes(1);
    expect(post).toHaveBeenCalledWith("/cases/search", { ...search, skipTotal: true });
  });
});

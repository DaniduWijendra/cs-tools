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

import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import "@testing-library/jest-dom/vitest";
import type * as React from "react";
import SavedViewsMenu from "@features/csm-operations/components/SavedViewsMenu";
import type {
  BeReorderSavedFilterViewPayload,
  BeSaveSavedFilterViewPayload,
  BeSavedFilterView,
} from "@api/backend/types";

type View = BeSavedFilterView;

let views: View[] = [];
const getMock = vi.fn();
const patchMock = vi.fn();
const delMock = vi.fn();
const postMock = vi.fn();

vi.mock("@api/backend/client", () => ({
  useBackendApi: () => ({
    get: getMock,
    patch: patchMock,
    del: delMock,
    post: postMock,
  }),
}));

function wireApi(): void {
  getMock.mockImplementation(async () => ({ views: [...views] }));
  patchMock.mockImplementation(async (_path: string, body: BeSaveSavedFilterViewPayload) => {
    const name = body.name.trim();
    views = [{ name, qs: body.qs }, ...views.filter((v) => v.name.toLowerCase() !== name.toLowerCase())];
    return { views: [...views] };
  });
  delMock.mockImplementation(async (path: string) => {
    const name = new URL(path, "http://local").searchParams.get("name") ?? "";
    views = views.filter((v) => v.name.toLowerCase() !== name.toLowerCase());
    return { views: [...views] };
  });
  postMock.mockImplementation(async (_path: string, body: BeReorderSavedFilterViewPayload) => {
    const i = views.findIndex((v) => v.name.toLowerCase() === body.name.toLowerCase());
    const t = body.direction === "up" ? i - 1 : i + 1;
    if (i >= 0 && t >= 0 && t < views.length) {
      const next = [...views];
      [next[i], next[t]] = [next[t], next[i]];
      views = next;
    }
    return { views: [...views] };
  });
}

function renderMenu(overrides: Partial<React.ComponentProps<typeof SavedViewsMenu>> = {}) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const onApply = vi.fn();
  const canonicalizeQs = (qs: string) => qs;
  const utils = render(
    <QueryClientProvider client={queryClient}>
      <SavedViewsMenu
        currentQs="q=hello"
        canonicalizeQs={canonicalizeQs}
        activeCount={1}
        hasSearch={false}
        onApply={onApply}
        listKey="incidents"
        {...overrides}
      />
    </QueryClientProvider>,
  );
  return { ...utils, onApply };
}

describe("SavedViewsMenu", () => {
  beforeEach(() => {
    views = [];
    getMock.mockReset();
    patchMock.mockReset();
    delMock.mockReset();
    postMock.mockReset();
    wireApi();
  });

  it("saves the current view via the dialog", async () => {
    renderMenu();

    fireEvent.click(screen.getByRole("button", { name: /saved views/i }));
    fireEvent.click(screen.getByRole("menuitem", { name: /save current view/i }));

    const nameField = screen.getByLabelText(/view name/i);
    fireEvent.change(nameField, { target: { value: "My open S1s" } });
    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));

    await waitFor(() =>
      expect(patchMock).toHaveBeenCalledWith("/users/me/saved-filter-views", {
        listKey: "incidents",
        name: "My open S1s",
        qs: "q=hello",
      }),
    );
  });

  it("keeps the save dialog open when PATCH fails", async () => {
    patchMock.mockRejectedValueOnce(new Error("save failed"));
    renderMenu();

    fireEvent.click(screen.getByRole("button", { name: /saved views/i }));
    fireEvent.click(screen.getByRole("menuitem", { name: /save current view/i }));

    const nameField = screen.getByLabelText(/view name/i);
    fireEvent.change(nameField, { target: { value: "My open S1s" } });
    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));

    await waitFor(() => expect(screen.getByText(/couldn't save this view/i)).toBeInTheDocument());
    expect(screen.getByLabelText(/view name/i)).toHaveValue("My open S1s");
  });

  it("applying a saved view calls onApply with its stored qs", async () => {
    views = [{ name: "Critical only", qs: "severities=S1" }];
    const onApply = vi.fn();
    renderMenu({ onApply });

    fireEvent.click(screen.getByRole("button", { name: /saved views/i }));
    await waitFor(() => screen.getByRole("menuitem", { name: /critical only/i }));
    fireEvent.click(screen.getByRole("menuitem", { name: /critical only/i }));

    expect(onApply).toHaveBeenCalledWith("severities=S1");
  });

  it("highlights the saved view matching the current qs as active", async () => {
    views = [
      { name: "Different", qs: "q=other" },
      { name: "Matches current", qs: "q=hello" },
    ];
    renderMenu({ currentQs: "q=hello" });

    fireEvent.click(screen.getByRole("button", { name: /saved views/i }));
    await waitFor(() => screen.getByText("Matches current"));
    expect(screen.getByText("Matches current").closest('[role="menuitem"]')).toHaveClass(
      "Mui-selected",
    );
    expect(screen.getByText("Different").closest('[role="menuitem"]')).not.toHaveClass(
      "Mui-selected",
    );
  });

  it("reorders saved views with the up/down icon buttons", async () => {
    views = [
      { name: "Second", qs: "q=2" },
      { name: "First", qs: "q=1" },
    ];
    renderMenu();

    fireEvent.click(screen.getByRole("button", { name: /saved views/i }));
    await waitFor(() => screen.getByRole("button", { name: /move saved view second down/i }));
    fireEvent.click(screen.getByRole("button", { name: /move saved view second down/i }));

    await waitFor(() => {
      const items = screen
        .getAllByRole("menuitem")
        .filter((el) => el.textContent?.match(/First|Second/));
      expect(items[0]).toHaveTextContent("First");
      expect(items[1]).toHaveTextContent("Second");
    });
  });

  it("does not claim 'all records' in the save dialog when only a search term is active", () => {
    renderMenu({ activeCount: 0, hasSearch: true });

    fireEvent.click(screen.getByRole("button", { name: /saved views/i }));
    fireEvent.click(screen.getByRole("menuitem", { name: /save current view/i }));

    expect(screen.queryByText(/will show all records/i)).not.toBeInTheDocument();
    expect(screen.getByText(/captures the 0 active filters and the current search/i)).toBeInTheDocument();
  });

  it("shows the 'all records' tip only when there is neither an active filter nor a search", () => {
    renderMenu({ activeCount: 0, hasSearch: false });

    fireEvent.click(screen.getByRole("button", { name: /saved views/i }));
    fireEvent.click(screen.getByRole("menuitem", { name: /save current view/i }));

    expect(screen.getByText(/will show all records/i)).toBeInTheDocument();
  });

  it("deletes a saved view via its delete icon button", async () => {
    views = [{ name: "Temp view", qs: "q=temp" }];
    renderMenu();

    fireEvent.click(screen.getByRole("button", { name: /saved views/i }));
    await waitFor(() => screen.getByRole("button", { name: /delete saved view temp view/i }));
    fireEvent.click(screen.getByRole("button", { name: /delete saved view temp view/i }));

    await waitFor(() => {
      expect(screen.queryByText("Temp view")).not.toBeInTheDocument();
      expect(screen.getByText(/no saved views yet/i)).toBeInTheDocument();
    });
  });
});

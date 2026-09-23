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

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/wso2-open-operations/cs-tools/entity-service/internal/domain"
)

type recordingSNWritebackFailures struct {
	mu    sync.Mutex
	calls []domain.CreateSNWritebackFailureRequest
}

func (r *recordingSNWritebackFailures) Create(_ context.Context, req domain.CreateSNWritebackFailureRequest) (domain.SNWritebackFailure, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, req)
	return domain.SNWritebackFailure{ID: "f1"}, nil
}

func (r *recordingSNWritebackFailures) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// waitFor polls until cond returns true or the timeout elapses, failing the
// test on timeout — Dispatch is fire-and-forget, so tests observe its
// background worker's effect rather than a return value.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func TestSNWritebackDispatcher_SuccessDoesNotRecordFailure(t *testing.T) {
	failures := &recordingSNWritebackFailures{}
	d := NewSNWritebackDispatcher(failures)

	done := make(chan struct{})
	d.Dispatch(context.Background(), "account", "acc-1", "update", map[string]string{"name": "Example Corp"}, func(context.Context) error {
		close(done)
		return nil
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("writeFn was never invoked")
	}
	// Give the worker a moment past writeFn returning to make sure a failure
	// row is never inserted for a successful write.
	time.Sleep(50 * time.Millisecond)
	if got := failures.count(); got != 0 {
		t.Fatalf("expected 0 failure records for a successful write, got %d", got)
	}
}

func TestSNWritebackDispatcher_FailureIsRecorded(t *testing.T) {
	failures := &recordingSNWritebackFailures{}
	d := NewSNWritebackDispatcher(failures)

	wantErr := errors.New("sn downstream unreachable")
	d.Dispatch(context.Background(), "account", "acc-2", "create", map[string]string{"name": "Example Corp"}, func(context.Context) error {
		return wantErr
	})

	waitFor(t, func() bool { return failures.count() == 1 })

	req := failures.calls[0]
	if req.EntityType != "account" || req.EntityID != "acc-2" || req.Operation != "create" {
		t.Fatalf("unexpected failure record: %+v", req)
	}
	if req.Error != wantErr.Error() {
		t.Fatalf("expected error %q, got %q", wantErr.Error(), req.Error)
	}
	if len(req.Payload) == 0 {
		t.Fatal("expected a non-empty marshaled payload")
	}
}

func TestSNWritebackDispatcher_DoesNotBlockCallerOnSlowWrite(t *testing.T) {
	failures := &recordingSNWritebackFailures{}
	d := NewSNWritebackDispatcher(failures)

	release := make(chan struct{})
	started := make(chan struct{})
	dispatchReturned := make(chan struct{})

	go func() {
		d.Dispatch(context.Background(), "account", "acc-3", "update", map[string]string{}, func(context.Context) error {
			close(started)
			<-release
			return nil
		})
		close(dispatchReturned)
	}()

	select {
	case <-dispatchReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("Dispatch blocked on writeFn instead of returning immediately")
	}

	<-started
	close(release)
}

func TestSNWritebackDispatcher_DetachesFromCallerContext(t *testing.T) {
	failures := &recordingSNWritebackFailures{}
	d := NewSNWritebackDispatcher(failures)

	callerCtx, cancel := context.WithCancel(context.Background())
	ran := make(chan error, 1)

	d.Dispatch(callerCtx, "account", "acc-4", "update", map[string]string{}, func(ctx context.Context) error {
		// Simulate the request completing (and its context being canceled)
		// before the background write finishes.
		<-time.After(30 * time.Millisecond)
		ran <- ctx.Err()
		return nil
	})

	cancel() // caller's HTTP request "returns" here

	select {
	case err := <-ran:
		if err != nil {
			t.Fatalf("expected writeFn's context to survive caller cancellation, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("writeFn was never invoked")
	}
}

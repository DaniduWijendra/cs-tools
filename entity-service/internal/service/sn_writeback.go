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
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/wso2-open-operations/cs-tools/entity-service/internal/domain"
	"github.com/wso2-open-operations/cs-tools/entity-service/internal/repository"
)

const (
	// snWritebackTimeout bounds a single ServiceNow mirror write. It runs on
	// a context.WithoutCancel copy of the triggering request's context, so
	// without its own bound it could hang indefinitely on a slow downstream
	// call, now that it's no longer tied to the request's own deadline.
	snWritebackTimeout = 10 * time.Second
	// snWritebackQueueSize is the dispatcher's buffered job channel size.
	// Dispatch drops (logs and records as a failure) rather than blocking
	// the caller's request when the queue is full — see Dispatch's own doc
	// comment.
	snWritebackQueueSize = 256
	// snWritebackWorkers is the fixed number of worker goroutines the
	// dispatcher starts at construction — a small bounded pool, not one
	// goroutine per call.
	snWritebackWorkers = 4
)

// snWritebackJob is one queued best-effort ServiceNow mirror write.
type snWritebackJob struct {
	ctx        context.Context
	entityType string
	entityID   string
	operation  string
	payload    any
	writeFn    func(context.Context) error
}

// SNWritebackDispatcher runs best-effort, one-way ServiceNow mirror writes
// for DATA_SOURCE=postgres-servicenow-dual-write (see
// config.DataSourcePostgresServiceNowDualWrite). Postgres is always written
// synchronously, in the request path, before Dispatch is ever called —
// Dispatch only ever fires after that commit already succeeded. The
// ServiceNow write it queues runs on a fixed background worker pool, fully
// detached from the triggering request: the request returns to its caller
// without waiting for it, and its own context is never cancelled by the
// request completing (see snWritebackTimeout).
//
// No retry logic: a single attempt, and on failure a row is inserted into
// sn_writeback_failures (SNWritebackFailureRepository) and a WARN is logged.
// Nothing currently reads that table back automatically — it exists so an
// operator can see, and manually replay, exactly what ServiceNow is missing
// before treating it as a live rollback target.
type SNWritebackDispatcher struct {
	failures repository.SNWritebackFailureRepository
	jobs     chan snWritebackJob
}

// NewSNWritebackDispatcher constructs an SNWritebackDispatcher and starts
// its fixed pool of background workers. failures must not be nil — the
// dispatcher only makes sense where a Postgres pool (and therefore this
// table) is available, which DATA_SOURCE=postgres-servicenow-dual-write
// guarantees (see config.Config.Validate's dbRequired check).
func NewSNWritebackDispatcher(failures repository.SNWritebackFailureRepository) *SNWritebackDispatcher {
	d := &SNWritebackDispatcher{
		failures: failures,
		jobs:     make(chan snWritebackJob, snWritebackQueueSize),
	}
	for i := 0; i < snWritebackWorkers; i++ {
		go d.worker()
	}
	return d
}

func (d *SNWritebackDispatcher) worker() {
	for job := range d.jobs {
		d.run(job)
	}
}

func (d *SNWritebackDispatcher) run(job snWritebackJob) {
	writeCtx, cancel := context.WithTimeout(job.ctx, snWritebackTimeout)
	defer cancel()

	err := job.writeFn(writeCtx)
	if err == nil {
		return
	}

	slog.WarnContext(writeCtx, "sn writeback: best-effort ServiceNow mirror write failed",
		"entityType", job.entityType, "entityId", job.entityID, "operation", job.operation, "error", err)

	payload, marshalErr := json.Marshal(job.payload)
	if marshalErr != nil {
		// The payload itself couldn't be recorded — still record the
		// failure, with the marshal error folded into the message, rather
		// than silently dropping it. An empty JSON object keeps the column
		// NOT NULL-valid.
		payload = json.RawMessage(`{}`)
		err = fmt.Errorf("%w (payload could not be marshaled for the failure record: %v)", err, marshalErr)
	}

	if _, recErr := d.failures.Create(writeCtx, domain.CreateSNWritebackFailureRequest{
		EntityType: job.entityType,
		EntityID:   job.entityID,
		Operation:  job.operation,
		Payload:    payload,
		Error:      err.Error(),
	}); recErr != nil {
		slog.ErrorContext(writeCtx, "sn writeback: ServiceNow mirror write failed and recording the failure also failed",
			"entityType", job.entityType, "entityId", job.entityID, "operation", job.operation, "writeErr", err, "recordErr", recErr)
	}
}

// Dispatch queues writeFn to run on the background worker pool and returns
// immediately — it never blocks the caller on the ServiceNow write itself
// (see the queue-full case below for the one situation where it still adds
// a small amount of local, non-ServiceNow latency).
// ctx is only used to derive the detached background context (via
// context.WithoutCancel); it is not otherwise consulted, so Dispatch always
// enqueues regardless of ctx's own state.
//
// If the queue is full (snWritebackQueueSize jobs already pending — meaning
// ServiceNow mirror writes are backing up faster than the pool can drain
// them), Dispatch still never touches ServiceNow itself: it logs and
// records the drop as a failure via a synchronous local Postgres insert
// instead (the same outcome a queued attempt would have on failure). That
// insert briefly blocks the caller — deliberately: firing it into yet
// another goroutine would just let failure records pile up unbounded
// against a queue that's already full, the same problem this branch exists
// to avoid. What Dispatch guarantees is no blocking on ServiceNow network
// I/O, never zero added latency.
func (d *SNWritebackDispatcher) Dispatch(ctx context.Context, entityType, entityID, operation string, payload any, writeFn func(context.Context) error) {
	job := snWritebackJob{
		ctx:        context.WithoutCancel(ctx),
		entityType: entityType,
		entityID:   entityID,
		operation:  operation,
		payload:    payload,
		writeFn:    writeFn,
	}

	select {
	case d.jobs <- job:
	default:
		slog.WarnContext(ctx, "sn writeback: queue full, dropping ServiceNow mirror write without attempting it",
			"entityType", entityType, "entityId", entityID, "operation", operation)
		d.run(snWritebackJob{
			ctx:        job.ctx,
			entityType: entityType,
			entityID:   entityID,
			operation:  operation,
			payload:    payload,
			writeFn: func(context.Context) error {
				return errQueueFull
			},
		})
	}
}

// errQueueFull is the synthetic error recorded when Dispatch's queue is full
// — see Dispatch's own doc comment. It never reaches ServiceNow: run's
// writeFn call fails immediately with this instead of attempting the write.
var errQueueFull = fmt.Errorf("sn writeback queue full (%d jobs pending); write not attempted", snWritebackQueueSize)

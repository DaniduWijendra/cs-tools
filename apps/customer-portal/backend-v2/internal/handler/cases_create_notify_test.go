// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com). All Rights Reserved.
//
// This software is the property of WSO2 LLC. and its suppliers, if any.
// Dissemination of any information or reproduction of any material contained
// herein in any form is strictly forbidden, unless permitted by WSO2 expressly.
// You may not alter or remove any copyright or other notice from copies of this content.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wso2-open-operations/cs-tools/apps/customer-portal/backend-v2/internal/entity"
)

const (
	notifyProjectID = "11111111-1111-1111-1111-111111111111"
	notifyConvID    = "33333333-3333-3333-3333-333333333333"
)

type notifyRecorder struct {
	calls     int
	sessionID string
	payload   string
	err       error
}

func (n *notifyRecorder) NotifyAgent(_ context.Context, sessionID, payload string) error {
	n.calls++
	n.sessionID = sessionID
	n.payload = payload
	return n.err
}

// updatingMockClient records the conversation state write so a test can tell the
// two side effects of a chat-raised case apart.
type updatingMockClient struct {
	mockCreateCaseClient
	updatedConversationID string
	updateErr             error
}

func (m *updatingMockClient) UpdateConversation(_ context.Context, id string, _ entity.UpdateConversationRequest) (entity.UpdateConversationResponse, error) {
	m.updatedConversationID = id
	if m.updateErr != nil {
		return entity.UpdateConversationResponse{}, m.updateErr
	}
	return entity.UpdateConversationResponse{}, nil
}

func newNotifyMock() *updatingMockClient {
	return &updatingMockClient{
		mockCreateCaseClient: mockCreateCaseClient{
			project: entity.ProjectDetailsView{ID: notifyProjectID},
			createdCase: entity.CreateCaseResponse{
				Case: entity.CreateCaseDetails{
					ID:     "22222222-2222-2222-2222-222222222222",
					Number: "CS12345",
				},
			},
		},
	}
}

func createCaseBody(conversationID string) string {
	body := `{"projectId":"` + notifyProjectID + `","title":"Test Case","description":"Details"`
	if conversationID != "" {
		body += `,"conversationId":"` + conversationID + `"`
	}
	return body + `}`
}

func postCase(t *testing.T, h *CaseHandler, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.CreateCase(rec, authedRequest(http.MethodPost, "/cases", body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}
	return rec
}

// The agent marks a conversation escalated only when it ran a case tool itself,
// so a case raised from the portal left no trace and its effectiveness
// reporting read 0% escalation while cases were being opened.
func TestCreateCase_FromConversation_NotifiesAgent(t *testing.T) {
	mock := newNotifyMock()
	notifier := &notifyRecorder{}
	h := NewCaseHandlerWithAgent(mock, notifier)

	postCase(t, h, createCaseBody(notifyConvID))

	if notifier.calls != 1 {
		t.Fatalf("expected exactly 1 notification, got %d", notifier.calls)
	}
	// The agent keys a session on "{project}:{conversation}".
	if want := notifyProjectID + ":" + notifyConvID; notifier.sessionID != want {
		t.Errorf("session id = %q, want %q", notifier.sessionID, want)
	}

	var got map[string]string
	if err := json.Unmarshal([]byte(notifier.payload), &got); err != nil {
		t.Fatalf("payload is not JSON: %v (%s)", err, notifier.payload)
	}
	for field, want := range map[string]string{
		"type":           "case_created",
		"accountId":      notifyProjectID,
		"conversationId": notifyConvID,
	} {
		if got[field] != want {
			t.Errorf("payload[%q] = %q, want %q", field, got[field], want)
		}
	}
}

// A case raised outside a chat has no conversation to escalate.
func TestCreateCase_WithoutConversation_DoesNotNotify(t *testing.T) {
	mock := newNotifyMock()
	notifier := &notifyRecorder{}
	h := NewCaseHandlerWithAgent(mock, notifier)

	postCase(t, h, createCaseBody(""))

	if notifier.calls != 0 {
		t.Fatalf("expected no notification, got %d", notifier.calls)
	}
}

// The case already exists by the time the agent is told, so a notification
// failure must not turn a successful creation into an error.
func TestCreateCase_NotifyFailure_StillSucceeds(t *testing.T) {
	mock := newNotifyMock()
	notifier := &notifyRecorder{err: errors.New("agent unreachable")}
	h := NewCaseHandlerWithAgent(mock, notifier)

	postCase(t, h, createCaseBody(notifyConvID))

	if notifier.calls != 1 {
		t.Fatalf("expected the notification to be attempted, got %d calls", notifier.calls)
	}
	// And the authoritative record was still written.
	if mock.updatedConversationID != notifyConvID {
		t.Errorf("conversation %q marked converted, want %q", mock.updatedConversationID, notifyConvID)
	}
}

// The conversation state is the authoritative record; the notification is only
// what makes it visible to the agent. Neither should depend on the other.
func TestCreateCase_ConversionFailure_StillNotifies(t *testing.T) {
	mock := newNotifyMock()
	mock.updateErr = errors.New("entity unavailable")
	notifier := &notifyRecorder{}
	h := NewCaseHandlerWithAgent(mock, notifier)

	postCase(t, h, createCaseBody(notifyConvID))

	if notifier.calls != 1 {
		t.Fatalf("expected the agent to be told even though the state write failed, got %d", notifier.calls)
	}
}

// A handler built without an agent must behave exactly as before — the two
// dozen existing tests construct one that way.
func TestCreateCase_NoAgentConfigured_DoesNotPanic(t *testing.T) {
	mock := newNotifyMock()
	h := NewCaseHandler(mock)

	postCase(t, h, createCaseBody(notifyConvID))

	if mock.updatedConversationID != notifyConvID {
		t.Errorf("conversation %q marked converted, want %q", mock.updatedConversationID, notifyConvID)
	}
}

// An id that is not a UUID is not forwarded: the agent keys its session on it.
func TestCreateCase_MalformedConversationID_DoesNotNotify(t *testing.T) {
	mock := newNotifyMock()
	notifier := &notifyRecorder{}
	h := NewCaseHandlerWithAgent(mock, notifier)

	postCase(t, h, createCaseBody("not-a-uuid"))

	if notifier.calls != 0 {
		t.Fatalf("expected no notification for a malformed id, got %d", notifier.calls)
	}
}

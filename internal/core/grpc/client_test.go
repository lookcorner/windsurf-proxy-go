package grpc

import (
	"errors"
	"testing"
)

func TestIsCascadeExecutorNotIdle(t *testing.T) {
	if !isCascadeExecutorNotIdle(errors.New("rpc error: code = Unknown desc = executor is not idle: CASCADE_RUN_STATUS_RUNNING")) {
		t.Fatal("expected executor-not-idle error to be detected")
	}
	if isCascadeExecutorNotIdle(errors.New("rpc error: code = Unknown desc = internal error")) {
		t.Fatal("unexpected executor-not-idle detection")
	}
	if isCascadeExecutorNotIdle(nil) {
		t.Fatal("nil error should not be detected")
	}
}

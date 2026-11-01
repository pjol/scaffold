package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/aws/smithy-go"
)

func TestBucketPolicy(t *testing.T) {
	RegisterPurpose(Purpose{Name: "policy-public", Public: true, Types: ImageTypes})
	RegisterPurpose(Purpose{Name: "policy-private", Types: DocumentTypes})
	var p struct {
		Statement []struct {
			Effect    string
			Principal map[string][]string
			Action    []string
			Resource  []string
		}
	}
	if err := json.Unmarshal(BucketPolicy("b"), &p); err != nil || len(p.Statement) != 1 {
		t.Fatalf("policy: %v %+v", err, p)
	}
	st := p.Statement[0]
	if st.Effect != "Allow" || !slices.Equal(st.Action, []string{"s3:GetObject"}) || !slices.Equal(st.Principal["AWS"], []string{"*"}) {
		t.Fatalf("statement %+v", st)
	}
	if !slices.Contains(st.Resource, "arn:aws:s3:::b/avatar/*") || !slices.Contains(st.Resource, "arn:aws:s3:::b/policy-public/*") {
		t.Fatalf("public prefixes missing: %v", st.Resource)
	}
	if slices.Contains(st.Resource, "arn:aws:s3:::b/policy-private/*") || slices.Contains(st.Resource, "arn:aws:s3:::b/*") {
		t.Fatalf("private objects readable: %v", st.Resource)
	}
}

func TestNotFoundErr(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{&smithy.GenericAPIError{Code: "NoSuchKey"}, true}, // CopyObject's untyped error
		{fmt.Errorf("wrapped: %w", &smithy.GenericAPIError{Code: "NotFound"}), true},
		{&smithy.GenericAPIError{Code: "AccessDenied"}, false},
		{errors.New("NoSuchKey in a message"), false},
	} {
		if got := errors.Is(notFoundErr(tc.err), ErrObjectNotFound); got != tc.want {
			t.Errorf("%v: not found = %v", tc.err, got)
		}
	}
	if apiErrorCode(&smithy.GenericAPIError{Code: "InvalidRange"}) != "InvalidRange" || apiErrorCode(errors.New("InvalidRange")) != "" {
		t.Error("apiErrorCode")
	}
}

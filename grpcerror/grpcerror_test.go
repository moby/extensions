// SPDX-FileCopyrightText: Copyright The Moby Authors
// SPDX-License-Identifier: Apache-2.0

package grpcerror_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/moby/extensions/grpcerror"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"gotest.tools/v3/assert"
	is "gotest.tools/v3/assert/cmp"
)

type alreadyExists struct{ error }

func (alreadyExists) AlreadyExists() {}

type invalidParameter struct{ error }

func (invalidParameter) InvalidParameter() {}

type unauthorized struct{ error }

func (unauthorized) Unauthorized() {}

type unavailable struct{ error }

func (unavailable) Unavailable()    {}
func (e unavailable) Unwrap() error { return e.error }

func TestToStatusAcceptsJoinedErrorWithNilCause(t *testing.T) {
	t.Parallel()

	err := errors.Join(errdefs.ErrUnavailable, &net.DNSError{Err: "no such host", Name: "replica"})
	st := status.Convert(grpcerror.ToStatus(err))
	assert.Equal(t, st.Code(), codes.Unavailable)
	assert.Equal(t, st.Message(), err.Error())
}

func TestToStatusPreservesMatchingStatusDetails(t *testing.T) {
	t.Parallel()

	st, err := status.New(codes.Unavailable, "offline").WithDetails(wrapperspb.String("replica"))
	assert.NilError(t, err)

	wrapped := unavailable{fmt.Errorf("lookup: %w", st.Err())}
	want := st.Proto()
	want.Message = wrapped.Error()
	got := status.Convert(grpcerror.ToStatus(wrapped)).Proto()
	assert.Check(t, proto.Equal(got, want), "got %v, want %v", got, want)
}

func TestFromStatusDecodesFirstJoinedStatus(t *testing.T) {
	t.Parallel()

	primary := status.Error(codes.NotFound, "missing")
	cleanup := grpcerror.FromStatus(status.Error(codes.Unavailable, "cleanup failed"))
	err := grpcerror.FromStatus(errors.Join(primary, cleanup))
	assert.Equal(t, status.Code(err), codes.NotFound)
	assert.Check(t, errdefs.IsNotFound(err))
}

func TestErrorClassRoundTrip(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		err   error
		code  codes.Code
		check func(error) bool
	}{
		{invalidParameter{errors.New("bad key")}, codes.InvalidArgument, errdefs.IsInvalidArgument},
		{unauthorized{errors.New("token expired")}, codes.Unauthenticated, errdefs.IsUnauthorized},
		{fmt.Errorf("stale revision: %w", errdefs.ErrConflict), codes.FailedPrecondition,
			func(err error) bool {
				return errdefs.IsConflict(err) && !errdefs.IsAlreadyExists(err)
			},
		},
		{alreadyExists{errors.New("exists")}, codes.AlreadyExists,
			func(err error) bool {
				return errdefs.IsAlreadyExists(err) && !errdefs.IsConflict(err)
			},
		},
		{fmt.Errorf("watch: %w", context.Canceled), codes.Canceled,
			func(err error) bool { return errors.Is(err, context.Canceled) },
		},
		{fmt.Errorf("lookup: %w", context.DeadlineExceeded), codes.DeadlineExceeded,
			func(err error) bool { return errors.Is(err, context.DeadlineExceeded) },
		},
		{errors.New("boom"), codes.Unknown, errdefs.IsUnknown},
	} {
		t.Run(tc.code.String(), func(t *testing.T) {
			t.Parallel()

			st := status.Convert(grpcerror.ToStatus(tc.err))
			err := grpcerror.FromStatus(st.Err())
			assert.Check(t, is.Error(err, tc.err.Error()))
			assert.Check(t, is.Equal(status.Code(err), tc.code))
			assert.Check(t, tc.check(err), "class lost: %#v", err)
		})
	}
}

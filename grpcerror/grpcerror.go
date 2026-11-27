// SPDX-FileCopyrightText: Copyright The Moby Authors
// SPDX-License-Identifier: Apache-2.0

// Package grpcerror carries error classes across the gRPC transport.
//
// Callers branch on error classes using containerd's errdefs helpers, such as
// errdefs.IsNotFound, which recognize both errdefs classes and marker methods
// like NotFound().
// In process, a provider's error reaches its caller unchanged.
// Over gRPC, ToStatus encodes the class as a status code on the server, and
// FromStatus restores it on the client, so callers see the same class either
// way.
// Both use containerd's errgrpc mapping.
// As in containerd, Conflict() is sent as codes.FailedPrecondition and restored
// only when the message ends in "conflict".
//
// Generated point wiring applies both conversions.
// Hand-written gRPC servers and clients, including streaming ones, should apply
// ToStatus to errors returned from handlers and FromStatus to errors other than
// io.EOF from Invoke, NewStream, SendMsg, and RecvMsg.
package grpcerror

import (
	"github.com/containerd/errdefs"
	"github.com/containerd/errdefs/pkg/errgrpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ToStatus returns a gRPC status error with err's class and message.
// An error that already contains a gRPC status is returned unchanged.
func ToStatus(err error) error {
	if _, ok := status.FromError(err); ok {
		return err
	}

	class := errdefs.Resolve(err)

	return status.Error(status.Code(errgrpc.ToGRPC(class)), err.Error())
}

// FromStatus returns a gRPC status error with its class restored by
// errgrpc.ToNative.
// Unlike ToNative's result, the error's message is the status message, and it
// also unwraps to the status error, so status.FromError and status.Code still
// work.
// Errors without a status are returned unchanged.
func FromStatus(err error) error {
	if err == nil {
		return nil
	}

	// Check only err itself: a decoded error on a later branch of a join must
	// not hide an undecoded status on an earlier one.
	if _, decoded := err.(interface{ fromStatus() }); decoded {
		return err
	}

	s, ok := status.FromError(err)
	if !ok || s.Code() == codes.OK {
		return err
	}

	return &remoteError{status: s, native: errgrpc.ToNative(err), cause: err}
}

type remoteError struct {
	status *status.Status
	native error
	cause  error
}

func (e *remoteError) Error() string              { return e.status.Message() }
func (e *remoteError) Unwrap() []error            { return []error{e.native, e.cause} }
func (e *remoteError) GRPCStatus() *status.Status { return e.status }
func (*remoteError) fromStatus()                  {}

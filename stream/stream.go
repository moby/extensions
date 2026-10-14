// SPDX-FileCopyrightText: Copyright The Moby Authors
// SPDX-License-Identifier: Apache-2.0

// Package stream defines typed, method-scoped streaming contracts.
//
// The owning method controls stream lifetime and its context controls
// cancellation.
package stream

// Sender sends messages to the other side of a method-scoped stream.
//
// Calls to one Sender must be serialized. The caller must not mutate the
// message, or data reachable from it, until the owning method returns.
// Send must treat that data as read-only and may retain it until then.
// A nil message is invalid.
//
// A successful Send does not report final method success or durable storage.
// Implementations that block must cooperate with the context of the method
// that owns the stream.
type Sender[T any] interface {
	Send(*T) error
}

// Receiver receives messages from the other side of a method-scoped stream.
//
// Calls to one Receiver must be serialized. Recv returns [io.EOF] only for
// normal input completion. A successful call must return a non-nil message.
// Messages returned by Recv are read-only to the caller and must not be used
// after the method that owns the stream returns.
//
// Implementations that block must cooperate with the context of the method
// that owns the stream.
// The method cannot interrupt a blocked Recv in any other way.
// If the other side finishes early, for example with an error, the method
// sees that result only after Recv returns.
type Receiver[T any] interface {
	Recv() (*T, error)
}

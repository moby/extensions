// SPDX-FileCopyrightText: Copyright The Moby Authors
// SPDX-License-Identifier: Apache-2.0

package stream

import (
	"errors"
	"fmt"
	"io"
	"iter"
	"testing"

	"gotest.tools/v3/assert"
	is "gotest.tools/v3/assert/cmp"
)

type testMessage struct {
	Value int
}

type testReceiver struct {
	messages    []*testMessage
	terminalErr error
	calls       int
}

func (r *testReceiver) Recv() (*testMessage, error) {
	r.calls++
	if len(r.messages) == 0 {
		return nil, r.terminalErr
	}
	message := r.messages[0]
	r.messages = r.messages[1:]
	return message, nil
}

type testSender struct {
	messages []*testMessage
	err      error
	calls    int
}

func (s *testSender) Send(message *testMessage) error {
	s.calls++
	if s.err != nil {
		return s.err
	}
	s.messages = append(s.messages, message)
	return nil
}

var (
	_ Sender[testMessage]   = (*testSender)(nil)
	_ Receiver[testMessage] = (*testReceiver)(nil)
)

func TestAllStopsAtEOF(t *testing.T) {
	t.Parallel()
	receiver := &testReceiver{
		messages:    []*testMessage{{Value: 1}},
		terminalErr: fmt.Errorf("input complete: %w", io.EOF),
	}

	var got []*testMessage
	var gotErrs []error
	for message, err := range All(receiver) {
		if err != nil {
			assert.Assert(t, is.Nil(message))
			gotErrs = append(gotErrs, err)
			continue
		}
		got = append(got, message)
	}

	assert.DeepEqual(t, got, []*testMessage{{Value: 1}})
	assert.Equal(t, len(gotErrs), 0)
	assert.Equal(t, receiver.calls, 2)
}

func TestAllPropagatesSourceError(t *testing.T) {
	t.Parallel()
	sourceErr := errors.New("source failed")
	receiver := &testReceiver{
		messages:    []*testMessage{{Value: 1}},
		terminalErr: sourceErr,
	}

	var got []*testMessage
	var gotErrs []error
	for message, err := range All(receiver) {
		if err != nil {
			assert.Assert(t, is.Nil(message))
			gotErrs = append(gotErrs, err)
			continue
		}
		got = append(got, message)
	}

	assert.DeepEqual(t, got, []*testMessage{{Value: 1}})
	assert.Equal(t, len(gotErrs), 1)
	assert.Assert(t, is.ErrorIs(gotErrs[0], sourceErr))
	assert.Equal(t, receiver.calls, 2)
}

func TestAllStopsWhenIterationStops(t *testing.T) {
	t.Parallel()
	receiver := &testReceiver{
		messages: []*testMessage{{Value: 1}, {Value: 2}},
	}

	for message, err := range All(receiver) {
		assert.Assert(t, is.Nil(err))
		assert.Equal(t, message.Value, 1)
		break
	}

	assert.Equal(t, receiver.calls, 1)
}

func TestCopy(t *testing.T) {
	t.Parallel()
	t.Run("EOF is successful completion", func(t *testing.T) {
		receiver := &testReceiver{
			messages:    []*testMessage{{Value: 1}},
			terminalErr: fmt.Errorf("input complete: %w", io.EOF),
		}
		sender := &testSender{}

		err := Copy(sender, All(receiver))

		assert.Assert(t, is.Nil(err))
		assert.DeepEqual(t, sender.messages, []*testMessage{{Value: 1}})
		assert.Equal(t, sender.calls, 1)
		assert.Equal(t, receiver.calls, 2)
	})

	t.Run("source error is returned", func(t *testing.T) {
		sourceErr := errors.New("source failed")
		receiver := &testReceiver{
			messages:    []*testMessage{{Value: 1}},
			terminalErr: sourceErr,
		}
		sender := &testSender{}

		err := Copy(sender, All(receiver))

		assert.Assert(t, is.ErrorIs(err, sourceErr))
		assert.DeepEqual(t, sender.messages, []*testMessage{{Value: 1}})
		assert.Equal(t, sender.calls, 1)
		assert.Equal(t, receiver.calls, 2)
	})

	t.Run("destination error is returned", func(t *testing.T) {
		destinationErr := errors.New("destination failed")
		receiver := &testReceiver{
			messages:    []*testMessage{{Value: 1}, {Value: 2}},
			terminalErr: io.EOF,
		}
		sender := &testSender{err: destinationErr}

		err := Copy(sender, All(receiver))

		assert.Assert(t, is.ErrorIs(err, destinationErr))
		assert.Equal(t, sender.calls, 1)
		assert.Equal(t, receiver.calls, 1)
	})
}

func TestCopyStopsSourceAfterDestinationError(t *testing.T) {
	t.Parallel()
	destinationErr := errors.New("destination failed")
	sender := &testSender{err: destinationErr}
	pulls := 0
	source := iter.Seq2[*testMessage, error](func(yield func(*testMessage, error) bool) {
		for value := 1; value <= 3; value++ {
			pulls++
			if !yield(&testMessage{Value: value}, nil) {
				return
			}
		}
	})

	err := Copy(sender, source)

	assert.Assert(t, is.ErrorIs(err, destinationErr))
	assert.Equal(t, pulls, 1)
	assert.Equal(t, sender.calls, 1)
}

func TestHelpersRejectNilMessages(t *testing.T) {
	t.Parallel()
	t.Run("All", func(t *testing.T) {
		receiver := &testReceiver{
			messages:    []*testMessage{nil},
			terminalErr: io.EOF,
		}

		var gotErrs []error
		for message, err := range All(receiver) {
			assert.Assert(t, is.Nil(message))
			gotErrs = append(gotErrs, err)
		}

		assert.Equal(t, len(gotErrs), 1)
		assert.Assert(t, is.ErrorContains(gotErrs[0], "nil message"))
		assert.Equal(t, receiver.calls, 1)
	})

	t.Run("Copy", func(t *testing.T) {
		sender := &testSender{}
		source := iter.Seq2[*testMessage, error](func(yield func(*testMessage, error) bool) {
			yield(nil, nil)
		})

		err := Copy(sender, source)

		assert.Assert(t, is.ErrorContains(err, "nil message"))
		assert.Equal(t, sender.calls, 0)
	})
}

func TestCopyPreservesIteratorErrors(t *testing.T) {
	t.Parallel()
	for _, sourceErr := range []error{io.EOF, errors.New("source failed")} {
		sender := &testSender{}
		stopped := false
		source := iter.Seq2[*testMessage, error](func(yield func(*testMessage, error) bool) {
			stopped = !yield(nil, sourceErr)
		})
		assert.ErrorIs(t, Copy(sender, source), sourceErr)
		assert.Assert(t, stopped)
		assert.Equal(t, sender.calls, 0)
	}
}

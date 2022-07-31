package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jpillora/backoff"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type tailState int

const (
	tailStateNormal tailState = iota
	tailStateRecover
)

type LogEvent struct {
	Pod       *v1.Pod
	Container *v1.Container
	Timestamp *time.Time
	Message   string
}

type LogEventFunc func(LogEvent)

func NewContainerTailer(
	client kubernetes.Interface,
	pod v1.Pod,
	container v1.Container,
	eventFunc LogEventFunc,
	fromTimestamp *time.Time) *ContainerTailer {
	return &ContainerTailer{
		client:        client,
		pod:           pod,
		container:     container,
		eventFunc:     eventFunc,
		fromTimestamp: fromTimestamp,
		errorBackoff:  &backoff.Backoff{},
		state:         tailStateNormal,
	}
}

type ContainerTailer struct {
	client           kubernetes.Interface
	pod              v1.Pod
	container        v1.Container
	stop             bool
	eventFunc        LogEventFunc
	fromTimestamp    *time.Time
	errorBackoff     *backoff.Backoff
	lastLineChecksum []byte
	state            tailState
}

func (ct *ContainerTailer) Stop() {
	ct.stop = true
}

func (ct *ContainerTailer) Run(ctx context.Context, onError func(err error)) {
	ct.errorBackoff.Reset()
	for !ct.stop {
		stream, err := ct.getStream(ctx)
		if err != nil {
			time.Sleep(ct.errorBackoff.Duration())
			onError(err)
			continue
		}
		if stream == nil {
			break
		}
		if err := ct.runStream(stream); err != nil {
			onError(err)
			time.Sleep(ct.errorBackoff.Duration())
		}
		ct.state = tailStateRecover
	}
}

func (ct *ContainerTailer) runStream(stream io.ReadCloser) error {
	defer func() {
		_ = stream.Close()
	}()

	r := bufio.NewReader(stream)
	for !ct.stop {
		line, err := r.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				return err
			}
			return nil
		}
		ct.errorBackoff.Reset()
		ct.receiveLine(line)
	}
	return nil
}

func (ct *ContainerTailer) receiveLine(s string) {
	if len(s) > 0 && s[len(s)-1] == '\n' {
		s = s[0 : len(s)-1]
	}
	for len(s) > 0 && s[len(s)-1] == '\r' {
		s = s[0 : len(s)-1]
	}

	parts := strings.SplitN(s, " ", 2)
	if len(parts) < 2 {
		// TODO: Warn
		return
	}

	timeString, message := parts[0], parts[1]

	var timestamp time.Time
	if t, err := time.Parse(time.RFC3339Nano, timeString); err == nil {
		timestamp = t
	} else {
		// TODO: Warn
		return
	}

	checksum := checksumLine(message)

	if ct.state == tailStateRecover {
		if ct.lastLineChecksum != nil && bytes.Equal(ct.lastLineChecksum, checksum) {
			// If just restarted, we might be continuing off a timestamp that results in dupes,
			// so discard the dupes.
			return
		}
		if ct.fromTimestamp != nil && timestamp.Before(*ct.fromTimestamp) {
			// We are receiving an old line, skip it
			return
		}
	}

	ct.lastLineChecksum = checksum
	ct.state = tailStateNormal

	// On restart, start from this timestamp. This isn't exact, however.
	nextTimestamp := timestamp.Add(time.Millisecond * 1)
	ct.fromTimestamp = &nextTimestamp

	ct.eventFunc(LogEvent{
		Pod:       &ct.pod,
		Container: &ct.container,
		Timestamp: &timestamp,
		Message:   parts[1],
	})
}

func (ct *ContainerTailer) getStream(ctx context.Context) (io.ReadCloser, error) {
	var sinceTime *metav1.Time
	if ct.fromTimestamp != nil {
		sinceTime = &metav1.Time{
			Time: ct.fromTimestamp.UTC(),
		}
	}

	boff := &backoff.Backoff{}
	for {
		stream, err := ct.client.CoreV1().Pods(ct.pod.Namespace).GetLogs(ct.pod.Name, &v1.PodLogOptions{
			Container:  ct.container.Name,
			Follow:     true,
			Timestamps: true,
			SinceTime:  sinceTime,
		}).Stream(ctx)
		if err == nil {
			return stream, nil
		}
		if status, ok := err.(errors.APIStatus); ok {
			// This will happen if the pod isn't ready for log-reading yet
			switch status.Status().Code {
			case http.StatusBadRequest:
				time.Sleep(boff.Duration())
				continue
			case http.StatusNotFound:
				return nil, nil
			}
		}
		return nil, err
	}
}

func checksumLine(s string) []byte {
	digest := sha256.New()
	digest.Write([]byte(s))
	return digest.Sum(nil)
}

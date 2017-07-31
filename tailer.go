package main

import (
	"bufio"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jpillora/backoff"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
)

type LogEvent struct {
	Pod       *v1.Pod
	Container *v1.Container
	Timestamp *time.Time
	Message   string
}

type LogEventFunc func(LogEvent)

func NewContainerTailer(
	clientset *kubernetes.Clientset,
	pod v1.Pod,
	container v1.Container,
	eventFunc LogEventFunc) *ContainerTailer {
	return &ContainerTailer{
		clientset: clientset,
		pod:       pod,
		container: container,
		eventFunc: eventFunc,
	}
}

type ContainerTailer struct {
	clientset *kubernetes.Clientset
	pod       v1.Pod
	container v1.Container
	stop      bool
	eventFunc LogEventFunc
}

func (ct *ContainerTailer) Stop() {
	ct.stop = true
}

func (ct *ContainerTailer) Run() error {
	for !ct.stop {
		stream, err := ct.getStream()
		if err != nil {
			return err
		}
		if stream == nil {
			break
		}
		if err := ct.runStream(stream); err != nil {
			return err
		}
	}
	return nil
}

func (ct *ContainerTailer) runStream(stream io.ReadCloser) error {
	defer func() {
		_ = stream.Close()
	}()

	r := bufio.NewReader(stream)
	for {
		line, err := r.ReadString('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
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
		// TODO: Warn?
		return
	}

	var timestamp *time.Time
	if t, err := time.Parse(time.RFC3339Nano, parts[0]); err == nil {
		timestamp = &t
	}

	ct.eventFunc(LogEvent{
		Pod:       &ct.pod,
		Container: &ct.container,
		Timestamp: timestamp,
		Message:   parts[1],
	})
}

func (ct *ContainerTailer) getStream() (io.ReadCloser, error) {
	sinceSeconds := int64(1)

	boff := &backoff.Backoff{}
	for {
		stream, err := ct.clientset.Core().Pods(ct.pod.Namespace).GetLogs(ct.pod.Name, &v1.PodLogOptions{
			Container:    ct.container.Name,
			Follow:       true,
			Timestamps:   true,
			SinceSeconds: &sinceSeconds,
		}).Stream()
		if err != nil {
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
		return stream, nil
	}
}

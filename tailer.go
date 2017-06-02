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
	pod *v1.Pod,
	container *v1.Container,
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
	pod       *v1.Pod
	container *v1.Container
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

		sc := bufio.NewScanner(stream)
		for sc.Scan() {
			ct.receiveLine(sc.Text())
		}
		_ = stream.Close()

		if err := sc.Err(); err != nil {
			return err
		}
	}
	return nil
}

func (ct *ContainerTailer) receiveLine(s string) {
	parts := strings.SplitN(s, " ", 2)

	var timestamp *time.Time
	if t, err := time.Parse(time.RFC3339Nano, parts[0]); err == nil {
		timestamp = &t
	}

	ct.eventFunc(LogEvent{
		Pod:       ct.pod,
		Container: ct.container,
		Timestamp: timestamp,
		Message:   parts[1],
	})
}

func (ct *ContainerTailer) getStream() (io.ReadCloser, error) {
	boff := &backoff.Backoff{}
	for {
		stream, err := ct.clientset.Core().Pods(ct.pod.Namespace).GetLogs(ct.pod.Name, &v1.PodLogOptions{
			Container:  ct.container.Name,
			Follow:     true,
			Timestamps: true,
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

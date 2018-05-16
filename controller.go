package main

import (
	"fmt"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/tools/cache"
)

type ControllerOptions struct {
	Namespace  string
	Matcher    Matcher
	SinceStart bool
}

type (
	ContainerEnterFunc func(
		pod *v1.Pod,
		container *v1.Container,
		initialAddPhase bool) bool

	ContainerExitFunc func(pod *v1.Pod,
		container *v1.Container)

	ContainerErrorFunc func(pod *v1.Pod,
		container *v1.Container, err error)
)

type Callbacks struct {
	OnEvent LogEventFunc
	OnEnter ContainerEnterFunc
	OnExit  ContainerExitFunc
	OnError ContainerErrorFunc
}

type Controller struct {
	ControllerOptions
	clientset *kubernetes.Clientset
	tailers   map[string]*ContainerTailer
	callbacks Callbacks
	sync.Mutex
}

func NewController(
	clientset *kubernetes.Clientset,
	options ControllerOptions,
	callbacks Callbacks) *Controller {
	return &Controller{
		ControllerOptions: options,
		clientset:         clientset,
		tailers:           map[string]*ContainerTailer{},
		callbacks:         callbacks,
	}
}

func (ctl *Controller) Run() {
	podListWatcher := cache.NewListWatchFromClient(
		ctl.clientset.CoreV1Client.RESTClient(), "pods", ctl.Namespace, fields.Everything())

	obj, err := podListWatcher.List(metav1.ListOptions{})
	if err != nil {
		panic(err)
	}
	if podList, ok := obj.(*v1.PodList); ok {
		for _, pod := range podList.Items {
			ctl.onInitialAdd(&pod)
		}
	}

	_, informer := cache.NewIndexerInformer(
		podListWatcher, &v1.Pod{}, 0, cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				if pod, ok := obj.(*v1.Pod); ok {
					ctl.onAdd(pod)
				}
			},
			UpdateFunc: func(old interface{}, new interface{}) {
				if pod, ok := new.(*v1.Pod); ok {
					ctl.onUpdate(pod)
				}
			},
			DeleteFunc: func(obj interface{}) {
				if pod, ok := obj.(*v1.Pod); ok {
					ctl.onDelete(pod)
				}
			},
		}, cache.Indexers{})

	stopCh := make(chan struct{}, 1)
	go informer.Run(stopCh)
	<-stopCh
}

func (ctl *Controller) onInitialAdd(pod *v1.Pod) {
	for _, container := range pod.Spec.InitContainers {
		if ctl.shouldIncludeContainer(pod, &container) {
			ctl.addContainer(pod, &container, true)
		}
	}
	for _, container := range pod.Spec.Containers {
		if ctl.shouldIncludeContainer(pod, &container) {
			ctl.addContainer(pod, &container, true)
		}
	}
}

func (ctl *Controller) onAdd(pod *v1.Pod) {
	for _, container := range pod.Spec.InitContainers {
		if ctl.shouldIncludeContainer(pod, &container) {
			ctl.addContainer(pod, &container, false)
		}
	}
	for _, container := range pod.Spec.Containers {
		if ctl.shouldIncludeContainer(pod, &container) {
			ctl.addContainer(pod, &container, false)
		}
	}
}

func (ctl *Controller) onUpdate(pod *v1.Pod) {
	containers := pod.Spec.Containers
	containerStatuses := allContainerStatusesForPod(pod)
	for _, containerStatus := range containerStatuses {
		var container *v1.Container
		for i, c := range containers {
			if c.Name == containerStatus.Name {
				container = &containers[i]
				break
			}
		}
		if container == nil {
			// Should be impossible; means there's a status for a container that isn't
			// part of the spec
			continue
		}

		if ctl.shouldIncludeContainer(pod, container) {
			ctl.addContainer(pod, container, false)
		} else {
			ctl.deleteContainer(pod, container)
		}
	}
}

func (ctl *Controller) onDelete(pod *v1.Pod) {
	for _, container := range pod.Spec.Containers {
		ctl.deleteContainer(pod, &container)
	}
}

func (ctl *Controller) shouldIncludePod(pod *v1.Pod) bool {
	if !ctl.Matcher.Match(pod) {
		return false
	}
	if pod.Status.Phase != v1.PodRunning &&
		pod.Status.Phase != v1.PodPending {
		return false
	}
	return true
}

func (ctl *Controller) shouldIncludeContainer(pod *v1.Pod, container *v1.Container) bool {
	if !ctl.shouldIncludePod(pod) {
		return false
	}
	var status *v1.ContainerStatus
	for _, s := range allContainerStatusesForPod(pod) {
		if s.Name == container.Name {
			status = &s
			break
		}
	}
	if status == nil || status.State.Waiting != nil || status.State.Terminated != nil ||
		status.State.Running == nil {
		return false
	}
	return true
}

func (ctl *Controller) addContainer(pod *v1.Pod, container *v1.Container, initialAdd bool) {
	ctl.Lock()
	defer ctl.Unlock()

	key := buildKey(pod, container)
	if _, ok := ctl.tailers[key]; ok {
		return
	}

	if !ctl.callbacks.OnEnter(pod, container, initialAdd) {
		return
	}

	timestamp, ok := ctl.getStartTimestamp(pod, container, initialAdd)
	if !ok {
		return
	}

	targetPod, targetContainer := *pod, *container // Copy to avoid mutation

	tailer := NewContainerTailer(ctl.clientset, targetPod, targetContainer,
		ctl.callbacks.OnEvent, timestamp)
	ctl.tailers[key] = tailer

	go func() {
		tailer.Run(func(err error) {
			ctl.callbacks.OnError(&targetPod, &targetContainer, err)
		})
	}()
}

func (ctl *Controller) deleteContainer(pod *v1.Pod, container *v1.Container) {
	ctl.Lock()
	defer ctl.Unlock()

	key := buildKey(pod, container)
	if tailer, ok := ctl.tailers[key]; ok {
		delete(ctl.tailers, key)
		tailer.Stop()
		ctl.callbacks.OnExit(pod, container)
	}
}

func (ctl *Controller) getStartTimestamp(
	pod *v1.Pod,
	container *v1.Container,
	initialAdd bool) (*time.Time, bool) {
	if ctl.SinceStart {
		return nil, true
	}

	if initialAdd && !ctl.SinceStart {
		// Don't show any history, but add a small amount of buffer to
		// account for clock skew
		now := time.Now().Add(time.Second * -5)
		return &now, true
	}

	var t *time.Time
	for _, status := range allContainerStatusesForPod(pod) {
		if status.Name == container.Name && status.State.Running != nil {
			startTime := status.State.Running.StartedAt.Time
			if t == nil || startTime.Before(*t) {
				t = &startTime
			}
		}
	}
	if t == nil {
		return nil, false
	}
	return t, true
}

func buildKey(pod *v1.Pod, container *v1.Container) string {
	return fmt.Sprintf("%s/%s/%s", pod.Namespace, pod.Name, findContainerID(pod, container))
}

func findContainerID(pod *v1.Pod, container *v1.Container) string {
	for _, c := range allContainerStatusesForPod(pod) {
		if c.Name == container.Name {
			return c.ContainerID
		}
	}
	return container.Name // Fallback, should never happen
}

func allContainerStatusesForPod(pod *v1.Pod) []v1.ContainerStatus {
	statuses := make([]v1.ContainerStatus, len(pod.Status.ContainerStatuses)+len(pod.Status.InitContainerStatuses))
	return append(
		append(statuses, pod.Status.InitContainerStatuses...),
		pod.Status.ContainerStatuses...)
}

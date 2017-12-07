package main

import (
	"fmt"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/tools/cache"
)

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
	clientset     *kubernetes.Clientset
	tailers       map[string]*ContainerTailer
	namespace     string
	labelSelector labels.Selector
	callbacks     Callbacks
	sync.Mutex
}

func NewController(
	clientset *kubernetes.Clientset,
	namespace string,
	labelSelector labels.Selector,
	callbacks Callbacks) *Controller {
	return &Controller{
		clientset:     clientset,
		tailers:       map[string]*ContainerTailer{},
		namespace:     namespace,
		labelSelector: labelSelector,
		callbacks:     callbacks,
	}
}

func (ctl *Controller) Run() {
	podListWatcher := cache.NewListWatchFromClient(
		ctl.clientset.CoreV1Client.RESTClient(), "pods", ctl.namespace, fields.Everything())

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
	ctl.onUpdateWithContainers(pod, pod.Spec.Containers,
		pod.Status.ContainerStatuses)
	ctl.onUpdateWithContainers(pod, pod.Spec.InitContainers,
		pod.Status.InitContainerStatuses)
}

func (ctl *Controller) onUpdateWithContainers(pod *v1.Pod,
	containers []v1.Container,
	containerStatuses []v1.ContainerStatus) {
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
	if !ctl.labelSelector.Matches(labels.Set(pod.Labels)) {
		return false
	}
	if pod.Status.Phase != v1.PodRunning &&
		pod.Status.Phase != v1.PodPending {
		return false
	}
	return true
}

func (ctl *Controller) shouldIncludeContainer(
	pod *v1.Pod, container *v1.Container) bool {
	if !ctl.shouldIncludePod(pod) {
		return false
	}
	var status *v1.ContainerStatus
	for _, s := range pod.Status.ContainerStatuses {
		if s.Name == container.Name {
			status = &s
			break
		}
	}
	if status == nil {
		for _, s := range pod.Status.InitContainerStatuses {
			if s.Name == container.Name {
				status = &s
				break
			}
		}
		if status == nil {
			return false
		}
	}
	if status.State.Waiting != nil || status.State.Terminated != nil ||
		status.State.Running == nil {
		return false
	}
	return true
}

func (ctl *Controller) addContainer(
	pod *v1.Pod,
	container *v1.Container,
	initialAdd bool) {
	ctl.Lock()
	defer ctl.Unlock()

	key := buildKey(pod, container)
	if _, ok := ctl.tailers[key]; ok {
		return
	}

	if !ctl.callbacks.OnEnter(pod, container, initialAdd) {
		return
	}

	targetPod, targetContainer := *pod, *container // Copy to avoid mutation

	var fromTimestamp *time.Time
	if initialAdd {
		// Don't show any history, but add a small amount of buffer to
		// account for clock skew
		now := time.Now().Add(time.Second * -5)
		fromTimestamp = &now
	} else {
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name == container.Name && status.State.Running != nil {
				startTime := status.State.Running.StartedAt.Time
				fromTimestamp = &startTime
				break
			}
		}
	}

	tailer := NewContainerTailer(ctl.clientset, targetPod, targetContainer,
		ctl.callbacks.OnEvent, fromTimestamp)
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

func buildKey(pod *v1.Pod, container *v1.Container) string {
	return fmt.Sprintf("%s/%s/%s", pod.Namespace, pod.Name, container.Name)
}

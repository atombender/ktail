package main

import (
	"fmt"
	"os"
	"regexp"
	"sync"
	"text/template"

	"github.com/fatih/color"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
    _ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	var (
		contextName       string
		kubeconfigPath    string
		labelSelectorExpr string
		namespace         string
		allNamespaces     bool
		quiet             bool
		timestamps        bool
		tmplString        string
		containerPatterns []*regexp.Regexp
	)

	flags := pflag.NewFlagSet("ktail", pflag.ExitOnError)
	flags.Usage = func() {
		flags.PrintDefaults()
	}

	flags.StringVar(&contextName, "context", "", "Kubernetes context name")
	flags.StringVar(&kubeconfigPath, "kubeconfig", "", "Path to kubeconfig (only required out-of-cluster)")
	flags.StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace")
	flags.StringVarP(&labelSelectorExpr, "selector", "l", "", "Match pods by label (see 'kubectl get -h' for syntax)")
	flags.StringVarP(&tmplString, "template", "t", "", "Template to format each line. For example, for"+
		" just the message, use --template '{{ .Message }}'.")
	flags.BoolVar(&allNamespaces, "all-namespaces", false, "Apply to all Kubernetes namespaces")
	flags.BoolVar(&timestamps, "timestamps", false, "Include timestamps on each line")
	flags.BoolVarP(&quiet, "quiet", "q", false, "Don't print events about new/deleted pods")

	if err := flags.Parse(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	for _, arg := range flags.Args() {
		r, err := regexp.Compile(arg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid regexp: %q: %s\n", arg, err)
			os.Exit(1)
		}
		containerPatterns = append(containerPatterns, r)
	}

	if tmplString == "" {
		tmplString = "{{.Pod.Name}}:{{.Container.Name}} {{.Message}}"
		if allNamespaces {
			tmplString = "{{.Pod.Namespace}}/" + tmplString
		}
		if timestamps {
			tmplString = "{{.Timestamp}} " + tmplString
		}
	}
	tmplString += "\n"

	if kubeconfigPath == "" {
		if os.Getenv("KUBECONFIG") != "" {
			kubeconfigPath = os.Getenv("KUBECONFIG")
		} else {
			kubeconfigPath = clientcmd.RecommendedHomeFile
		}
	}

	labelSelector := labels.Everything()
	if labelSelectorExpr != "" {
		if sel, err := labels.Parse(labelSelectorExpr); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		} else {
			labelSelector = sel
		}
	}

	tmpl, err := template.New("line").Parse(tmplString)
	if err != nil {
		fmt.Fprintln(os.Stderr, fmt.Sprintf("Invalid template: %s", err))
		os.Exit(1)
	}

	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{
			ExplicitPath: kubeconfigPath,
		},
		&clientcmd.ConfigOverrides{
			CurrentContext: contextName,
		})

	config, err := clientConfig.ClientConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	rawConfig, err := clientConfig.RawConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if allNamespaces {
		namespace = v1.NamespaceAll
	} else if namespace == "" {
		if rawConfig.Contexts[rawConfig.CurrentContext].Namespace == "" {
			namespace = v1.NamespaceDefault
		} else {
			namespace = rawConfig.Contexts[rawConfig.CurrentContext].Namespace
		}
	}

	yellow := color.New(color.FgYellow)
	red := color.New(color.FgRed)

	formatPod := func(pod *v1.Pod) string {
		if allNamespaces {
			return fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
		}
		return pod.Name
	}

	formatPodAndContainer := func(pod *v1.Pod, container *v1.Container) string {
		return fmt.Sprintf("%s:%s", formatPod(pod), container.Name)
	}

	var stdoutMutex sync.Mutex
	controller := NewController(clientset, namespace, labelSelector,
		Callbacks{
			OnEvent: func(event LogEvent) {
				stdoutMutex.Lock()
				defer stdoutMutex.Unlock()
				_ = tmpl.Execute(os.Stdout, event)
			},
			OnEnter: func(
				pod *v1.Pod,
				container *v1.Container,
				initialAddPhase bool) bool {
				if len(containerPatterns) > 0 {
					match := false
					for _, r := range containerPatterns {
						if r.MatchString(pod.Name) || r.MatchString(container.Name) {
							match = true
							break
						}
					}
					if !match {
						return false
					}
				}
				if !quiet {
					if initialAddPhase {
						_, _ = yellow.Fprintf(os.Stderr,
							"==> Detected running container [%s]\n", formatPodAndContainer(pod, container))
					} else {
						_, _ = yellow.Fprintf(os.Stderr,
							"==> New container [%s]\n", formatPodAndContainer(pod, container))
					}
				}
				return true
			},
			OnExit: func(pod *v1.Pod, container *v1.Container) {
				if !quiet {
					var status = "unknown"
					for _, containerStatus := range pod.Status.ContainerStatuses {
						if containerStatus.Name == container.Name {
							if containerStatus.State.Running != nil {
								status = "running"
							} else if containerStatus.State.Waiting != nil {
								status = "waiting"
							} else if containerStatus.State.Terminated != nil {
								status = "terminated"
							}
							break
						}
					}
					_, _ = yellow.Fprintf(os.Stderr,
						"==> Container left (%s) [%s]\n", status,
						formatPodAndContainer(pod, container))
				}
			},
			OnError: func(pod *v1.Pod, container *v1.Container, err error) {
				_, _ = red.Fprintf(os.Stderr,
					"==> Warning: Error while tailing container [%s]: %s\n",
					formatPodAndContainer(pod, container), err)
			},
		})
	controller.Run()
}

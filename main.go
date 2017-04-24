package main

import (
	"fmt"
	"os"
	"text/template"

	"github.com/fatih/color"
	"github.com/spf13/pflag"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/labels"
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

	if tmplString == "" {
		tmplString = "{{.Pod.Name}}:{{.Container.Name}} {{.Message}}"
		if allNamespaces {
			tmplString = "{{.Pod.Namespace}}/" + tmplString
		}
		if timestamps {
			tmplString = "{{.Timestamp}}" + tmplString
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

	controller := NewController(clientset, namespace, labelSelector,
		func(event LogEvent) {
			tmpl.Execute(os.Stdout, event)
		},
		func(pod *v1.Pod, container *v1.Container) {
			if !quiet {
				yellow.Fprintf(os.Stderr, "==> Detected container %s\n", formatPodAndContainer(pod, container))
			}
		},
		func(pod *v1.Pod, container *v1.Container) {
			if !quiet {
				yellow.Fprintf(os.Stderr, "==> Leaving container %s\n", formatPodAndContainer(pod, container))
			}
		},
		func(pod *v1.Pod, container *v1.Container, err error) {
			red.Fprintf(os.Stderr, "==> Warning: Error while container %s: %s\n",
				formatPodAndContainer(pod, container), err)
		})
	controller.Run()
}

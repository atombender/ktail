package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sync"
	"text/template"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/pflag"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	var (
		contextName           string
		kubeconfigPath        string
		labelSelectorExpr     string
		namespace             string
		allNamespaces         bool
		quiet                 bool
		timestamps            bool
		raw                   bool
		tmplString            string
		sinceStart            bool
		showVersion           bool
		includePatterns       []*regexp.Regexp
		excludePatternStrings []string
		noColor               bool
	)

	flags := pflag.NewFlagSet("ktail", pflag.ExitOnError)
	flags.Usage = func() {
		flags.PrintDefaults()
	}
	flags.StringVar(&contextName, "context", "", "Kubernetes context name")
	flags.StringVar(&kubeconfigPath, "kubeconfig", "",
		"Path to kubeconfig (only required out-of-cluster)")
	flags.StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace")
	flags.StringArrayVarP(&excludePatternStrings, "exclude", "x", []string{},
		"Exclude using a regular expression. Pattern can be repeated. Takes priority over"+
			" include patterns and labels.")
	flags.StringVarP(&labelSelectorExpr, "selector", "l", "",
		"Match pods by label (see 'kubectl get -h' for syntax).")
	flags.StringVarP(&tmplString, "template", "t", "",
		"Template to format each line. For example, for"+
			" just the message, use --template '{{ .Message }}'.")
	flags.BoolVar(&allNamespaces, "all-namespaces", false, "Apply to all Kubernetes namespaces")
	flags.BoolVarP(&raw, "raw", "r", false, "Don't format output; output messages only (unless --timestamps)")
	flags.BoolVarP(&timestamps, "timestamps", "T", false, "Include timestamps on each line")
	flags.BoolVarP(&quiet, "quiet", "q", false, "Don't print events about new/deleted pods")
	flags.BoolVarP(&sinceStart, "since-start", "s", false,
		"Start reading log from the beginning of the container's lifetime.")
	flags.BoolVarP(&showVersion, "version", "", false, "Show version.")
	flags.BoolVarP(&noColor, "no-color", "", false, "Disable color.")

	if err := flags.Parse(os.Args[1:]); err != nil {
		fail(err.Error())
		os.Exit(1)
	}

	color.NoColor = noColor

	if showVersion {
		fmt.Printf("ktail %s\n", version)
		os.Exit(0)
	}

	var excludePatterns []*regexp.Regexp
	for _, p := range excludePatternStrings {
		r, err := regexp.Compile(p)
		if err != nil {
			fail("Invalid regexp: %q: %s\n", p, err)
		}
		excludePatterns = append(excludePatterns, r)
	}

	for _, arg := range flags.Args() {
		r, err := regexp.Compile(arg)
		if err != nil {
			fail("Invalid regexp: %q: %s\n", arg, err)
		}
		includePatterns = append(includePatterns, r)
	}

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
			fail(err.Error())
		} else {
			labelSelector = sel
		}
	}

	inclusionMatcher := buildMatcher(includePatterns, labelSelector, true)
	exclusionMatcher := buildMatcher(excludePatterns, nil, false)

	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{
			ExplicitPath: kubeconfigPath,
		},
		&clientcmd.ConfigOverrides{
			CurrentContext: contextName,
		})

	config, err := clientConfig.ClientConfig()
	if err != nil {
		fail(err.Error())
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		fail(err.Error())
	}

	rawConfig, err := clientConfig.RawConfig()
	if err != nil {
		fail(err.Error())
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

	var tmpl *template.Template
	if tmplString != "" {
		var err error
		tmpl, err = template.New("line").Parse(tmplString)
		if err != nil {
			fail("invalid template: %s", err)
		}
	}

	formatPod := func(pod *v1.Pod) string {
		if allNamespaces {
			return fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
		}
		return pod.Name
	}

	formatPodAndContainer := func(pod *v1.Pod, container *v1.Container) string {
		return fmt.Sprintf("%s:%s", formatPod(pod), container.Name)
	}

	var printEvent func(*LogEvent) error

	if tmpl != nil {
		printEvent = func(event *LogEvent) error {
			type templateEvent struct {
				Pod       *v1.Pod
				Container *v1.Container
				Timestamp string
				Message   string
			}

			var buf bytes.Buffer
			if err := tmpl.Execute(&buf, &templateEvent{
				Pod:       event.Pod,
				Container: event.Container,
				Message:   event.Message,
				Timestamp: formatTimestamp(event.Timestamp),
			}); err != nil {
				return err
			}

			_, err := fmt.Fprintln(os.Stdout, string(buf.Bytes()))
			return err
		}
	} else {
		printEvent = func(event *LogEvent) error {
			col := getColorConfig(event.Pod.Name, event.Container.Name)

			var line string
			if !raw {
				if timestamps {
					line = col.metadata.Sprint(formatTimestamp(event.Timestamp))
				}
				line += " "
				if allNamespaces {
					line += col.labels.Sprint(fmt.Sprintf("%s/%s:%s",
						event.Pod.Namespace, event.Pod.Name, event.Container.Name))
				} else {
					line += col.labels.Sprint(fmt.Sprintf("%s:%s", event.Pod.Name, event.Container.Name))
				}
				line += " "
			}

			line += event.Message

			_, err := fmt.Fprintln(os.Stdout, line)
			return err
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var stdoutMutex sync.Mutex
	controller := NewController(clientset, ControllerOptions{
		Namespace:        namespace,
		InclusionMatcher: inclusionMatcher,
		ExclusionMatcher: exclusionMatcher,
		SinceStart:       sinceStart,
	},
		Callbacks{
			OnEvent: func(event LogEvent) {
				stdoutMutex.Lock()
				defer stdoutMutex.Unlock()
				if err := printEvent(&event); err != nil {
					printError(fmt.Sprintf("Could not write event: %s", err))
					cancel()
				}
			},
			OnEnter: func(pod *v1.Pod, container *v1.Container, initialAddPhase bool) bool {
				if !quiet {
					if initialAddPhase {
						printInfo("Attached to container [%s]", formatPodAndContainer(pod, container))
					} else {
						printInfo("New container [%s]", formatPodAndContainer(pod, container))
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
					printInfo(fmt.Sprintf("Container left (%s) [%s]\n", status,
						formatPodAndContainer(pod, container)))
				}
			},
			OnError: func(pod *v1.Pod, container *v1.Container, err error) {
				printError(fmt.Sprintf("Error while tailing container [%s]: %s",
					formatPodAndContainer(pod, container), err))
			},
		})

	if err := controller.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		printError(err.Error())
	}
}

func printInfo(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	_, _ = fmt.Fprintf(os.Stderr, colorInfo(fmt.Sprintf("==> %s\n", message)))
}

func printError(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	_, _ = fmt.Fprintf(os.Stderr, colorError(fmt.Sprintf("==> %s\n", message)))
}

func formatTimestamp(t *time.Time) string {
	s := t.Local().Format("2006-01-02T15:04:05.999")
	for len(s) < 23 {
		s += "0"
	}
	return s
}

func fail(format string, args ...interface{}) {
	_, _ = fmt.Fprintf(os.Stderr, format, args...)
	os.Exit(1)
}

var (
	colorInfo  = color.New(color.FgYellow).SprintFunc()
	colorError = color.New(color.FgRed).SprintFunc()
)

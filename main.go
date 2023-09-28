package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sync"
	"text/template"
	"time"

	_ "github.com/alecthomas/chroma/formatters"
	"github.com/alecthomas/chroma/quick"
	"github.com/fatih/color"
	"github.com/go-logr/logr"
	"github.com/spf13/pflag"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

func main() {
	klog.SetLogger(logr.New(&kubeLogger{}))

	cfg := Config{
		ColorMode:   "auto",
		ColorScheme: "bw",
	}

	var (
		contextName       string
		labelSelectorExpr string
		namespaces        []string
		allNamespaces     bool

		kubeconfigPath        string
		quiet                 bool
		timestamps            bool
		raw                   bool
		tmplString            string
		sinceStart            bool
		sinceExpr             string
		showVersion           bool
		includePatterns       []*regexp.Regexp
		excludePatternStrings []string
		noColor               bool
		colorMode             string
		colorScheme           string
	)

	if err := cfg.LoadDefault(); err != nil {
		fail(err.Error())
	}

	flags := pflag.NewFlagSet("ktail", pflag.ContinueOnError)
	flags.SortFlags = false
	flags.Usage = func() {
		fmt.Printf("Usage: ktail [OPTION ...] PATTERN [PATTERN ...]\n")
		flags.PrintDefaults()
	}
	flags.StringVar(&contextName, "context", "", "Kubernetes context name")
	flags.StringArrayVarP(&namespaces, "namespace", "n", []string{}, "Kubernetes namespace")
	flags.BoolVar(&allNamespaces, "all-namespaces", false, "Apply to all Kubernetes namespaces")
	flags.StringArrayVarP(&excludePatternStrings, "exclude", "x", []string{},
		"Exclude using a regular expression. Pattern can be repeated. Takes priority over"+
			" include patterns and labels.")
	flags.StringVarP(&labelSelectorExpr, "selector", "l", "",
		"Match pods by label (see 'kubectl get -h' for syntax).")
	flags.BoolVarP(&sinceStart, "since-start", "s", false,
		"Start reading log from the beginning of the container's lifetime.")
	flags.BoolVarP(&showVersion, "version", "", false, "Show version.")
	flags.StringVarP(&sinceExpr, "since", "S", "", "Get logs since a given time (e.g. 2023-03-30) or duration (e.g. 1h).")

	flags.StringVar(&kubeconfigPath, "kubeconfig", cfg.KubeConfigPath,
		"Path to kubeconfig (only required out-of-cluster)")
	flags.StringVarP(&tmplString, "template", "t", cfg.TemplateString,
		"Template to format each line. For example, for"+
			" just the message, use --template '{{ .Message }}'.")
	flags.BoolVarP(&raw, "raw", "r", cfg.Raw, "Don't format output; output messages only (unless --timestamps)")
	flags.BoolVarP(&timestamps, "timestamps", "T", cfg.Timestamps, "Include timestamps on each line")
	flags.BoolVarP(&quiet, "quiet", "q", cfg.Quiet, "Don't print events about new/deleted pods")
	flags.BoolVar(&noColor, "no-color", cfg.NoColor, "Alias for --color=never.")
	flags.StringVar(&colorMode, "color", cfg.ColorMode, "Set color mode: one of 'auto' (default), 'never', or 'always'. (Aliased as --colour.)")
	flags.StringVar(&colorMode, "colour", cfg.ColorMode, "Set color mode: one of 'auto' (default), 'never', or 'always'.")
	flags.StringVar(&colorScheme, "color-scheme", cfg.ColorScheme, "Set color scheme (see https://github.com/alecthomas/chroma/tree/master/styles). (Aliased as --colour-scheme.)")
	flags.StringVar(&colorScheme, "colour-scheme", cfg.ColorScheme, "Set color scheme (see https://github.com/alecthomas/chroma/tree/master/styles).")
	_ = flags.MarkHidden("colour")
	_ = flags.MarkHidden("colour-scheme")

	if err := flags.Parse(os.Args[1:]); err != nil {
		if err == pflag.ErrHelp {
			os.Exit(2)
		}
		fail(err.Error())
	}

	if noColor {
		colorMode = "never"
	}
	var colorEnabled bool
	switch colorMode {
	case "always":
		colorEnabled = true
	case "auto":
		colorEnabled = isTerminal(os.Stdout)
	case "never":
	}

	color.NoColor = !colorEnabled

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

	var loadingRules *clientcmd.ClientConfigLoadingRules
	if kubeconfigPath != "" {
		loadingRules = &clientcmd.ClientConfigLoadingRules{
			ExplicitPath: kubeconfigPath,
		}
	} else {
		loadingRules = clientcmd.NewDefaultClientConfigLoadingRules()
	}

	clientConfig := clientcmd.NewInteractiveDeferredLoadingClientConfig(loadingRules,
		&clientcmd.ConfigOverrides{
			CurrentContext: contextName,
		},
		nil)

	config, err := clientConfig.ClientConfig()
	if err != nil {
		fail(err.Error())
	}

	// Set higher rate limits
	config.QPS = 100
	config.Burst = 100

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		fail(err.Error())
	}

	rawConfig, err := clientConfig.RawConfig()
	if err != nil {
		fail(err.Error())
	}

	if allNamespaces {
		namespaces = []string{v1.NamespaceAll}
	} else if len(namespaces) == 0 {
		if rawConfig.Contexts[rawConfig.CurrentContext].Namespace == "" {
			namespaces = []string{v1.NamespaceDefault}
		} else {
			namespaces = []string{rawConfig.Contexts[rawConfig.CurrentContext].Namespace}
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

	since, err := parseSinceExpr(sinceExpr)
	if err != nil {
		fail("invalid --since flag: %s", err)
	}

	formatPod := func(pod *v1.Pod) string {
		if allNamespaces || len(namespaces) > 1 {
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

			_, err := fmt.Fprintln(os.Stdout, buf.String())
			return err
		}
	} else {
		printEvent = func(event *LogEvent) error {
			col := getColorConfig(event.Pod.Name, event.Container.Name)

			var line string
			if !raw {
				if timestamps {
					line = col.metadata.Sprint(formatTimestamp(event.Timestamp))
					line += " "
				}
				if allNamespaces {
					line += col.labels.Sprint(fmt.Sprintf("%s/%s:%s",
						event.Pod.Namespace, event.Pod.Name, event.Container.Name))
				} else {
					line += col.labels.Sprint(fmt.Sprintf("%s:%s", event.Pod.Name, event.Container.Name))
				}
				line += " "
			}

			payload := event.Message
			if colorEnabled && len(payload) >= 2 && payload[0] == '{' && payload[len(payload)-1] == '}' {
				var dest interface{}
				if err := json.Unmarshal([]byte(payload), &dest); err == nil {
					var buf bytes.Buffer
					if err := quick.Highlight(&buf, payload, "json", "terminal256", colorScheme); err == nil {
						payload = buf.String()
					}
				}
			}

			line += payload

			_, err := fmt.Fprintln(os.Stdout, line)
			return err
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var stdoutMutex sync.Mutex
	controller := NewController(clientset,
		ControllerOptions{
			Namespaces:       namespaces,
			InclusionMatcher: inclusionMatcher,
			ExclusionMatcher: exclusionMatcher,
			Since:            since,
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
					printInfo(fmt.Sprintf("Container left (%s) [%s]", status,
						formatPodAndContainer(pod, container)))
				}
			},
			OnNothingDiscovered: func() {
				printInfo("No matching pods running yet")
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

func fail(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	_, _ = fmt.Fprintf(os.Stderr, fmt.Sprintf("fatal: %s\n", msg))
	os.Exit(1)
}

func parseSinceExpr(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	for _, layout := range []string{
		time.ANSIC, time.UnixDate, time.RubyDate, time.RFC822, time.RFC822Z, time.RFC850, time.RFC1123,
		time.RFC1123Z, time.RFC3339, time.RFC3339Nano, time.Kitchen, time.Stamp, time.StampMilli,
		time.StampMicro, time.StampNano, time.DateTime, time.DateOnly, time.TimeOnly,
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return &t, nil
		}
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return nil, fmt.Errorf("parsing as duration: %w", err)
	}
	t := time.Now().Add(-dur)
	return &t, nil
}

var (
	colorInfo  = color.New(color.FgYellow).SprintFunc()
	colorError = color.New(color.FgRed).SprintFunc()
)

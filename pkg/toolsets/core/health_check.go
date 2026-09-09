package core

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/containers/kubernetes-mcp-server/pkg/api"
	"github.com/containers/kubernetes-mcp-server/pkg/klogutil"
)

const (
	podHighRestartThreshold = 5

	healthCollectionTimeout        = 15 * time.Second
	healthSectionTimeout           = 10 * time.Second
	healthMaxAPICalls              = 6
	healthEventWorkers             = 4
	healthListLimit          int64 = 500
	healthNamespaceLimit           = 2000
	healthEventNamespaces          = 20
	healthEventsPerNamespace int64 = 100
	healthEventAge                 = time.Hour
	healthIssueRows                = 20
	healthEventRows                = 25
	healthMessageRunes             = 160
	healthPromptRunes              = 24000
)

type healthScope struct {
	namespace string
}

func (s healthScope) isNamespaceScoped() bool {
	return s.namespace != ""
}

type healthSection struct {
	itemCount int
	err       error
	truncated bool
	skipped   string
	details   string
}

type healthDiagnostics struct {
	mu sync.Mutex

	collectionTime time.Time
	scope          healthScope
	sections       map[string]healthSection

	nodes        *v1.NodeList
	namespaces   *v1.NamespaceList
	pods         *v1.PodList
	deployments  *appsv1.DeploymentList
	statefulSets *appsv1.StatefulSetList
	daemonSets   *appsv1.DaemonSetList
	pvcs         *v1.PersistentVolumeClaimList
	events       []v1.Event
}

func newHealthDiagnostics(scope healthScope) *healthDiagnostics {
	return &healthDiagnostics{
		collectionTime: time.Now().UTC(),
		scope:          scope,
		sections:       make(map[string]healthSection),
	}
}

func (d *healthDiagnostics) setSection(name string, section healthSection) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sections[name] = section
}

func (d *healthDiagnostics) section(name string) healthSection {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sections[name]
}

func (d *healthDiagnostics) setSkipped(names []string, reason string) {
	for _, name := range names {
		d.setSection(name, healthSection{skipped: reason})
	}
}

// initHealthChecks initializes bounded cluster-wide and namespace-scoped health prompts.
func initHealthChecks() []api.ServerPrompt {
	return []api.ServerPrompt{
		{
			Prompt: api.Prompt{
				Name:        "cluster-health-check",
				Title:       "Cluster Health Check",
				Description: "Assess Kubernetes cluster health with bounded diagnostic collection",
				Arguments: []api.PromptArgument{
					{
						Name:        "check_events",
						Description: "Include recent warning/error events (true/false, default: true)",
						Required:    false,
					},
				},
			},
			Handler: clusterHealthCheckHandler,
		},
		{
			Prompt: api.Prompt{
				Name:        "namespace-health-check",
				Title:       "Namespace Health Check",
				Description: "Assess Kubernetes workload health within one namespace",
				Arguments: []api.PromptArgument{
					{
						Name:        "namespace",
						Description: "Namespace to assess",
						Required:    true,
					},
					{
						Name:        "check_events",
						Description: "Include recent warning/error events (true/false, default: true)",
						Required:    false,
					},
				},
			},
			Handler: namespaceHealthCheckHandler,
		},
	}
}

func clusterHealthCheckHandler(params api.PromptHandlerParams) (*api.PromptCallResult, error) {
	return runHealthCheck(params, healthScope{})
}

func namespaceHealthCheckHandler(params api.PromptHandlerParams) (*api.PromptCallResult, error) {
	namespace := strings.TrimSpace(params.GetArguments()["namespace"])
	if namespace == "" {
		return nil, errors.New("namespace parameter is required")
	}
	return runHealthCheck(params, healthScope{namespace: namespace})
}

func runHealthCheck(params api.PromptHandlerParams, scope healthScope) (*api.PromptCallResult, error) {
	if err := params.Err(); err != nil {
		return nil, err
	}

	checkEvents := params.GetArguments()["check_events"] != "false"
	logger := klogutil.FromContext(params.Context)
	logger.Info("Starting health check", "kubernetes.namespace.name", scope.namespace, "events_enabled", checkEvents)

	diagnostics := gatherHealthDiagnostics(params, scope, checkEvents)
	return api.NewPromptCallResult(
		"Bounded Kubernetes health diagnostic data gathered",
		[]api.PromptMessage{
			{
				Role: "user",
				Content: api.PromptContent{
					Type: "text",
					Text: formatHealthCheckPrompt(diagnostics),
				},
			},
			{
				Role: "assistant",
				Content: api.PromptContent{
					Type: "text",
					Text: "I'll analyze the health diagnostic data, including any collection limitations, and provide an assessment.",
				},
			},
		},
		nil,
	), nil
}

func gatherHealthDiagnostics(params api.PromptHandlerParams, scope healthScope, checkEvents bool) *healthDiagnostics {
	ctx, cancel := context.WithTimeout(params.Context, healthCollectionTimeout)
	defer cancel()

	diagnostics := newHealthDiagnostics(scope)
	logger := klogutil.FromContext(ctx)
	slots := make(chan struct{}, healthMaxAPICalls)

	if scope.isNamespaceScoped() {
		var err error
		sectionCtx, sectionCancel := context.WithTimeout(ctx, healthSectionTimeout)
		err = withHealthAPISlot(sectionCtx, slots, func() error {
			_, getErr := params.CoreV1().Namespaces().Get(sectionCtx, scope.namespace, metav1.GetOptions{})
			return getErr
		})
		sectionCancel()
		namespaceItems := 1
		if err != nil {
			namespaceItems = 0
		}
		diagnostics.setSection("Namespace", healthSection{itemCount: namespaceItems, err: err})
		if err != nil {
			klogutil.LogWarn(logger, "Unable to verify namespace for health check", klogutil.Err(err), klogutil.Field("kubernetes.namespace.name", scope.namespace))
			diagnostics.setSkipped([]string{"Pods", "Deployments", "StatefulSets", "DaemonSets", "PersistentVolumeClaims", "Events"}, "namespace could not be verified")
			return diagnostics
		}
		diagnostics.setSkipped([]string{"Nodes", "Namespaces"}, "not applicable to a namespace-scoped health check")
	}

	var group errgroup.Group
	launch := func(name string, collect func(context.Context) healthSection) {
		group.Go(func() error {
			sectionCtx, sectionCancel := context.WithTimeout(ctx, healthSectionTimeout)
			defer sectionCancel()
			section := collect(sectionCtx)
			if section.err == nil && sectionCtx.Err() != nil {
				section.err = sectionCtx.Err()
			}
			diagnostics.setSection(name, section)
			if section.err != nil {
				klogutil.LogWarn(logger, "Health check section could not be collected", klogutil.Err(section.err), klogutil.Field("section", name))
			}
			return nil
		})
	}

	if !scope.isNamespaceScoped() {
		launch("Nodes", func(sectionCtx context.Context) healthSection {
			return collectHealthList(sectionCtx, slots,
				func(ctx context.Context, options metav1.ListOptions) (*v1.NodeList, error) {
					return params.CoreV1().Nodes().List(ctx, options)
				},
				func(list *v1.NodeList) {
					diagnostics.mu.Lock()
					diagnostics.nodes = list
					diagnostics.mu.Unlock()
				},
				func(list *v1.NodeList) int { return len(list.Items) },
			)
		})
		launch("Namespaces", func(sectionCtx context.Context) healthSection {
			return collectNamespaces(sectionCtx, slots, params, diagnostics)
		})
	}

	launch("Pods", func(sectionCtx context.Context) healthSection {
		return collectHealthList(sectionCtx, slots,
			func(ctx context.Context, options metav1.ListOptions) (*v1.PodList, error) {
				return params.CoreV1().Pods(scope.namespace).List(ctx, options)
			},
			func(list *v1.PodList) {
				diagnostics.mu.Lock()
				diagnostics.pods = list
				diagnostics.mu.Unlock()
			},
			func(list *v1.PodList) int { return len(list.Items) },
		)
	})
	launch("Deployments", func(sectionCtx context.Context) healthSection {
		return collectHealthList(sectionCtx, slots,
			func(ctx context.Context, options metav1.ListOptions) (*appsv1.DeploymentList, error) {
				return params.AppsV1().Deployments(scope.namespace).List(ctx, options)
			},
			func(list *appsv1.DeploymentList) {
				diagnostics.mu.Lock()
				diagnostics.deployments = list
				diagnostics.mu.Unlock()
			},
			func(list *appsv1.DeploymentList) int { return len(list.Items) },
		)
	})
	launch("StatefulSets", func(sectionCtx context.Context) healthSection {
		return collectHealthList(sectionCtx, slots,
			func(ctx context.Context, options metav1.ListOptions) (*appsv1.StatefulSetList, error) {
				return params.AppsV1().StatefulSets(scope.namespace).List(ctx, options)
			},
			func(list *appsv1.StatefulSetList) {
				diagnostics.mu.Lock()
				diagnostics.statefulSets = list
				diagnostics.mu.Unlock()
			},
			func(list *appsv1.StatefulSetList) int { return len(list.Items) },
		)
	})
	launch("DaemonSets", func(sectionCtx context.Context) healthSection {
		return collectHealthList(sectionCtx, slots,
			func(ctx context.Context, options metav1.ListOptions) (*appsv1.DaemonSetList, error) {
				return params.AppsV1().DaemonSets(scope.namespace).List(ctx, options)
			},
			func(list *appsv1.DaemonSetList) {
				diagnostics.mu.Lock()
				diagnostics.daemonSets = list
				diagnostics.mu.Unlock()
			},
			func(list *appsv1.DaemonSetList) int { return len(list.Items) },
		)
	})
	launch("PersistentVolumeClaims", func(sectionCtx context.Context) healthSection {
		return collectHealthList(sectionCtx, slots,
			func(ctx context.Context, options metav1.ListOptions) (*v1.PersistentVolumeClaimList, error) {
				return params.CoreV1().PersistentVolumeClaims(scope.namespace).List(ctx, options)
			},
			func(list *v1.PersistentVolumeClaimList) {
				diagnostics.mu.Lock()
				diagnostics.pvcs = list
				diagnostics.mu.Unlock()
			},
			func(list *v1.PersistentVolumeClaimList) int { return len(list.Items) },
		)
	})
	_ = group.Wait()

	if checkEvents {
		diagnostics.setSection("Events", collectEvents(ctx, slots, params, diagnostics, logger))
	} else {
		diagnostics.setSection("Events", healthSection{skipped: "disabled by check_events=false"})
	}

	logger.Info("Health check data collection completed", "kubernetes.namespace.name", scope.namespace)
	return diagnostics
}

func collectHealthList[T interface{ GetContinue() string }](
	ctx context.Context,
	slots chan struct{},
	list func(context.Context, metav1.ListOptions) (T, error),
	store func(T),
	count func(T) int,
) healthSection {
	var result T
	err := withHealthAPISlot(ctx, slots, func() error {
		var err error
		result, err = list(ctx, metav1.ListOptions{Limit: healthListLimit})
		return err
	})
	if err != nil {
		return healthSection{err: err}
	}
	store(result)
	return healthSection{itemCount: count(result), truncated: result.GetContinue() != ""}
}

func collectNamespaces(ctx context.Context, slots chan struct{}, params api.PromptHandlerParams, diagnostics *healthDiagnostics) healthSection {
	result := &v1.NamespaceList{}
	continueToken := ""
	truncated := false

	for len(result.Items) < healthNamespaceLimit {
		remaining := healthNamespaceLimit - len(result.Items)
		limit := healthListLimit
		if int64(remaining) < limit {
			limit = int64(remaining)
		}
		var page *v1.NamespaceList
		err := withHealthAPISlot(ctx, slots, func() error {
			var err error
			page, err = params.CoreV1().Namespaces().List(ctx, metav1.ListOptions{Limit: limit, Continue: continueToken})
			return err
		})
		if err != nil {
			diagnostics.mu.Lock()
			diagnostics.namespaces = result
			diagnostics.mu.Unlock()
			return healthSection{itemCount: len(result.Items), err: err, truncated: len(result.Items) > 0}
		}

		if len(page.Items) > remaining {
			result.Items = append(result.Items, page.Items[:remaining]...)
			truncated = true
			break
		}
		result.Items = append(result.Items, page.Items...)
		continueToken = page.GetContinue()
		if continueToken == "" {
			break
		}
		if len(result.Items) == healthNamespaceLimit {
			truncated = true
		}
	}

	diagnostics.mu.Lock()
	diagnostics.namespaces = result
	diagnostics.mu.Unlock()
	return healthSection{itemCount: len(result.Items), truncated: truncated}
}

func withHealthAPISlot(ctx context.Context, slots chan struct{}, call func() error) error {
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	return call()
}

func collectEvents(ctx context.Context, slots chan struct{}, params api.PromptHandlerParams, diagnostics *healthDiagnostics, logger klog.Logger) healthSection {
	namespaces, details, namespacesTruncated := eventNamespaces(diagnostics)
	sectionCtx, cancel := context.WithTimeout(ctx, healthSectionTimeout)
	defer cancel()

	var (
		mu        sync.Mutex
		events    []v1.Event
		failures  int
		truncated = namespacesTruncated
	)
	jobs := make(chan string)
	var group errgroup.Group
	workers := healthEventWorkers
	if len(namespaces) < workers {
		workers = len(namespaces)
	}
	for range workers {
		group.Go(func() error {
			for namespace := range jobs {
				var list *v1.EventList
				err := withHealthAPISlot(sectionCtx, slots, func() error {
					var err error
					list, err = params.CoreV1().Events(namespace).List(sectionCtx, metav1.ListOptions{
						FieldSelector: "type!=Normal",
						Limit:         healthEventsPerNamespace,
					})
					return err
				})
				mu.Lock()
				if err != nil {
					failures++
				} else {
					events = append(events, list.Items...)
					truncated = truncated || list.GetContinue() != ""
				}
				mu.Unlock()
			}
			return nil
		})
	}
dispatch:
	for _, namespace := range namespaces {
		select {
		case jobs <- namespace:
		case <-sectionCtx.Done():
			truncated = true
			break dispatch
		}
	}
	close(jobs)
	_ = group.Wait()

	filtered := filterRecentEvents(events, time.Now().Add(-healthEventAge))
	diagnostics.mu.Lock()
	diagnostics.events = filtered
	diagnostics.mu.Unlock()

	section := healthSection{itemCount: len(filtered), truncated: truncated, details: details}
	if failures > 0 {
		section.err = fmt.Errorf("%d namespace event collection request(s) failed", failures)
		klogutil.LogWarn(logger, "Health check event collection was incomplete", klogutil.Err(section.err))
	}
	return section
}

func eventNamespaces(diagnostics *healthDiagnostics) ([]string, string, bool) {
	if diagnostics.scope.isNamespaceScoped() {
		return []string{diagnostics.scope.namespace}, "", false
	}

	namespaces := []string{"default", "kube-system"}
	seen := map[string]struct{}{"default": {}, "kube-system": {}}

	diagnostics.mu.Lock()
	namespaceList := diagnostics.namespaces
	diagnostics.mu.Unlock()
	if namespaceList == nil {
		return namespaces, "additional system namespaces could not be discovered", false
	}
	if namespaceSection := diagnostics.section("Namespaces"); namespaceSection.err != nil {
		return namespaces, "additional system namespaces could not be discovered", namespaceSection.truncated
	}

	var additional []string
	for _, namespace := range namespaceList.Items {
		name := namespace.Name
		if strings.HasSuffix(name, "-system") || strings.HasPrefix(name, "openshift-") {
			if _, ok := seen[name]; !ok {
				seen[name] = struct{}{}
				additional = append(additional, name)
			}
		}
	}
	sort.Strings(additional)
	namespaces = append(namespaces, additional...)
	if len(namespaces) <= healthEventNamespaces {
		return namespaces, "", diagnostics.section("Namespaces").truncated
	}
	omitted := len(namespaces) - healthEventNamespaces
	return namespaces[:healthEventNamespaces], fmt.Sprintf("%d eligible system namespace(s) were not queried", omitted), true
}

func filterRecentEvents(events []v1.Event, since time.Time) []v1.Event {
	filtered := make([]v1.Event, 0, len(events))
	for _, event := range events {
		if event.Type != v1.EventTypeWarning && event.Type != "Error" {
			continue
		}
		if eventTime(event).Before(since) {
			continue
		}
		filtered = append(filtered, event)
	}
	sort.Slice(filtered, func(i, j int) bool {
		left, right := eventTime(filtered[i]), eventTime(filtered[j])
		if !left.Equal(right) {
			return left.After(right)
		}
		return eventKey(filtered[i]) < eventKey(filtered[j])
	})
	return filtered
}

func eventTime(event v1.Event) time.Time {
	candidates := []time.Time{
		event.EventTime.Time,
		event.LastTimestamp.Time,
		event.CreationTimestamp.Time,
	}
	if event.Series != nil {
		candidates = append(candidates, event.Series.LastObservedTime.Time)
	}
	for _, candidate := range candidates {
		if !candidate.IsZero() {
			return candidate
		}
	}
	return time.Time{}
}

func eventKey(event v1.Event) string {
	return strings.Join([]string{event.Namespace, event.InvolvedObject.Kind, event.InvolvedObject.Name, event.Reason}, "/")
}

func formatHealthCheckPrompt(diagnostics *healthDiagnostics) string {
	var sb strings.Builder
	title := "Cluster"
	if diagnostics.scope.isNamespaceScoped() {
		title = "Namespace"
	}
	fmt.Fprintf(&sb, "# %s Health Check Diagnostic Data\n\n", title)
	fmt.Fprintf(&sb, "**Collection time:** %s\n", diagnostics.collectionTime.Format(time.RFC3339))
	if diagnostics.scope.isNamespaceScoped() {
		fmt.Fprintf(&sb, "**Scope:** Namespace `%s`\n\n", diagnostics.scope.namespace)
	} else {
		fmt.Fprintf(&sb, "**Scope:** Cluster-wide workloads, with bounded system event collection\n\n")
	}

	sb.WriteString("## Analysis Instructions\n\n")
	sb.WriteString("Assess the collected evidence as Healthy, Warning, or Critical. Call out immediate risks, separate confirmed findings from unavailable or incomplete sections, and recommend targeted follow-up checks. Do not infer health from a section that was not collected.\n\n")
	renderCollectionStatus(&sb, diagnostics)

	if !diagnostics.scope.isNamespaceScoped() {
		renderNodes(&sb, diagnostics.nodes, diagnostics.section("Nodes"))
	}
	renderPods(&sb, diagnostics.pods, diagnostics.section("Pods"))
	renderWorkloads(&sb, diagnostics)
	renderPVCs(&sb, diagnostics.pvcs, diagnostics.section("PersistentVolumeClaims"))
	renderEvents(&sb, diagnostics.events, diagnostics.section("Events"))

	return truncatePrompt(sb.String())
}

func renderCollectionStatus(sb *strings.Builder, diagnostics *healthDiagnostics) {
	sb.WriteString("## Collection Status\n\n")
	names := []string{"Namespace", "Namespaces", "Nodes", "Pods", "Deployments", "StatefulSets", "DaemonSets", "PersistentVolumeClaims", "Events"}
	for _, name := range names {
		if (name == "Namespace" && !diagnostics.scope.isNamespaceScoped()) || (name == "Namespaces" && diagnostics.scope.isNamespaceScoped()) || (name == "Nodes" && diagnostics.scope.isNamespaceScoped()) {
			continue
		}
		section := diagnostics.section(name)
		fmt.Fprintf(sb, "- **%s:** %s\n", name, healthSectionSummary(section))
	}
	sb.WriteString("\n")
}

func healthSectionSummary(section healthSection) string {
	if section.skipped != "" {
		return "not collected: " + section.skipped
	}
	parts := []string{fmt.Sprintf("collected %d object(s)", section.itemCount)}
	if section.truncated {
		parts = append(parts, "incomplete due to collection limit")
	}
	if section.err != nil {
		if errors.Is(section.err, context.DeadlineExceeded) {
			parts = append(parts, "incomplete because the time limit was reached")
		} else {
			parts = append(parts, "unavailable: "+truncateText(section.err.Error(), healthMessageRunes))
		}
	}
	if section.details != "" {
		parts = append(parts, section.details)
	}
	return strings.Join(parts, "; ")
}

func renderNodes(sb *strings.Builder, nodes *v1.NodeList, section healthSection) {
	sb.WriteString("## Nodes\n\n")
	if !renderSectionWarning(sb, section) || nodes == nil {
		return
	}
	issues := make([]healthIssue, 0)
	ready := 0
	for _, node := range nodes.Items {
		isReady := false
		readyFound := false
		var reasons []string
		for _, condition := range node.Status.Conditions {
			if condition.Type == v1.NodeReady {
				readyFound = true
				isReady = condition.Status == v1.ConditionTrue
				if !isReady {
					reasons = append(reasons, healthReason("not ready", condition.Message))
				}
			} else if condition.Status == v1.ConditionTrue {
				reasons = append(reasons, healthReason(string(condition.Type), condition.Message))
			}
		}
		if !readyFound {
			reasons = append(reasons, "Ready condition is missing")
		}
		if isReady {
			ready++
		}
		if len(reasons) > 0 {
			issues = append(issues, healthIssue{key: node.Name, text: fmt.Sprintf("**%s**: %s", node.Name, strings.Join(reasons, "; "))})
		}
	}
	sortHealthIssues(issues)
	fmt.Fprintf(sb, "**Collected:** %d | **Ready:** %d | **With issues:** %d\n\n", len(nodes.Items), ready, len(issues))
	renderHealthIssues(sb, issues, "No node issues detected")
}

func renderPods(sb *strings.Builder, pods *v1.PodList, section healthSection) {
	sb.WriteString("## Pods\n\n")
	if !renderSectionWarning(sb, section) || pods == nil {
		return
	}
	issues := make([]healthIssue, 0)
	for _, pod := range pods.Items {
		var reasons []string
		restarts := int32(0)
		ready := 0
		for _, status := range pod.Status.ContainerStatuses {
			if status.Ready {
				ready++
			}
			restarts += status.RestartCount
			if waiting := status.State.Waiting; waiting != nil && (waiting.Reason == "CrashLoopBackOff" || waiting.Reason == "ImagePullBackOff" || waiting.Reason == "ErrImagePull") {
				reasons = append(reasons, healthReason("container waiting "+waiting.Reason, waiting.Message))
			}
			if terminated := status.State.Terminated; terminated != nil && (terminated.Reason == "Error" || terminated.Reason == "OOMKilled") {
				reasons = append(reasons, "container terminated "+terminated.Reason)
			}
		}
		if pod.Status.Phase != v1.PodRunning && pod.Status.Phase != v1.PodSucceeded {
			reasons = append(reasons, "pod phase "+string(pod.Status.Phase))
		}
		if restarts > podHighRestartThreshold {
			reasons = append(reasons, fmt.Sprintf("%d container restart(s), threshold %d", restarts, podHighRestartThreshold))
		}
		if len(reasons) > 0 {
			key := namespacedName(pod.Namespace, pod.Name)
			issues = append(issues, healthIssue{key: key, text: fmt.Sprintf("**%s** (phase %s, ready %d/%d, restarts %d): %s", key, pod.Status.Phase, ready, len(pod.Status.ContainerStatuses), restarts, strings.Join(reasons, "; "))})
		}
	}
	sortHealthIssues(issues)
	fmt.Fprintf(sb, "**Collected:** %d | **With issues:** %d\n\n", len(pods.Items), len(issues))
	renderHealthIssues(sb, issues, "No pod issues detected")
}

func renderWorkloads(sb *strings.Builder, diagnostics *healthDiagnostics) {
	sb.WriteString("## Workload Controllers\n\n")
	renderDeployments(sb, diagnostics.deployments, diagnostics.section("Deployments"))
	renderStatefulSets(sb, diagnostics.statefulSets, diagnostics.section("StatefulSets"))
	renderDaemonSets(sb, diagnostics.daemonSets, diagnostics.section("DaemonSets"))
}

func renderDeployments(sb *strings.Builder, deployments *appsv1.DeploymentList, section healthSection) {
	sb.WriteString("### Deployments\n\n")
	if !renderSectionWarning(sb, section) || deployments == nil {
		return
	}
	issues := make([]healthIssue, 0)
	for _, deployment := range deployments.Items {
		if deployment.Status.UnavailableReplicas > 0 {
			key := namespacedName(deployment.Namespace, deployment.Name)
			issues = append(issues, healthIssue{key: key, text: fmt.Sprintf("**%s**: %d unavailable replica(s) (ready %d/%d)", key, deployment.Status.UnavailableReplicas, deployment.Status.ReadyReplicas, deployment.Status.Replicas)})
		}
	}
	sortHealthIssues(issues)
	fmt.Fprintf(sb, "**Collected:** %d | **With issues:** %d\n\n", len(deployments.Items), len(issues))
	renderHealthIssues(sb, issues, "No deployment issues detected")
}

func renderStatefulSets(sb *strings.Builder, statefulSets *appsv1.StatefulSetList, section healthSection) {
	sb.WriteString("### StatefulSets\n\n")
	if !renderSectionWarning(sb, section) || statefulSets == nil {
		return
	}
	issues := make([]healthIssue, 0)
	for _, statefulSet := range statefulSets.Items {
		desired := int32(1)
		if statefulSet.Spec.Replicas != nil {
			desired = *statefulSet.Spec.Replicas
		}
		if statefulSet.Status.ReadyReplicas < desired {
			key := namespacedName(statefulSet.Namespace, statefulSet.Name)
			issues = append(issues, healthIssue{key: key, text: fmt.Sprintf("**%s**: only %d/%d replica(s) ready", key, statefulSet.Status.ReadyReplicas, desired)})
		}
	}
	sortHealthIssues(issues)
	fmt.Fprintf(sb, "**Collected:** %d | **With issues:** %d\n\n", len(statefulSets.Items), len(issues))
	renderHealthIssues(sb, issues, "No StatefulSet issues detected")
}

func renderDaemonSets(sb *strings.Builder, daemonSets *appsv1.DaemonSetList, section healthSection) {
	sb.WriteString("### DaemonSets\n\n")
	if !renderSectionWarning(sb, section) || daemonSets == nil {
		return
	}
	issues := make([]healthIssue, 0)
	for _, daemonSet := range daemonSets.Items {
		if daemonSet.Status.NumberUnavailable > 0 {
			key := namespacedName(daemonSet.Namespace, daemonSet.Name)
			issues = append(issues, healthIssue{key: key, text: fmt.Sprintf("**%s**: %d unavailable pod(s) (ready %d/%d)", key, daemonSet.Status.NumberUnavailable, daemonSet.Status.NumberReady, daemonSet.Status.DesiredNumberScheduled)})
		}
	}
	sortHealthIssues(issues)
	fmt.Fprintf(sb, "**Collected:** %d | **With issues:** %d\n\n", len(daemonSets.Items), len(issues))
	renderHealthIssues(sb, issues, "No DaemonSet issues detected")
}

func renderPVCs(sb *strings.Builder, pvcs *v1.PersistentVolumeClaimList, section healthSection) {
	sb.WriteString("## Persistent Volume Claims\n\n")
	if !renderSectionWarning(sb, section) || pvcs == nil {
		return
	}
	issues := make([]healthIssue, 0)
	for _, pvc := range pvcs.Items {
		if pvc.Status.Phase != v1.ClaimBound {
			key := namespacedName(pvc.Namespace, pvc.Name)
			issues = append(issues, healthIssue{key: key, text: fmt.Sprintf("**%s**: claim phase %s", key, pvc.Status.Phase)})
		}
	}
	sortHealthIssues(issues)
	fmt.Fprintf(sb, "**Collected:** %d | **With issues:** %d\n\n", len(pvcs.Items), len(issues))
	renderHealthIssues(sb, issues, "No persistent volume claim issues detected")
}

func renderEvents(sb *strings.Builder, events []v1.Event, section healthSection) {
	sb.WriteString("## Recent Warning and Error Events\n\n")
	if !renderSectionWarning(sb, section) {
		return
	}
	warnings, errorsCount := 0, 0
	for _, event := range events {
		if event.Type == v1.EventTypeWarning {
			warnings++
		} else {
			errorsCount++
		}
	}
	fmt.Fprintf(sb, "**Warnings:** %d | **Errors:** %d | **Recent events:** %d\n\n", warnings, errorsCount, len(events))
	if len(events) == 0 {
		sb.WriteString("No recent warning or error events detected\n\n")
		return
	}
	limit := healthEventRows
	if len(events) < limit {
		limit = len(events)
	}
	for _, event := range events[:limit] {
		object := namespacedName(event.Namespace, event.InvolvedObject.Name)
		if event.InvolvedObject.Kind != "" {
			object = event.InvolvedObject.Kind + " " + object
		}
		fmt.Fprintf(sb, "- **%s** (%s, %s): %s\n", object, event.Reason, eventTime(event).Format(time.RFC3339), truncateText(event.Message, healthMessageRunes))
	}
	if omitted := len(events) - limit; omitted > 0 {
		fmt.Fprintf(sb, "- %d additional recent event(s) omitted from the report\n", omitted)
	}
	sb.WriteString("\n")
}

func renderSectionWarning(sb *strings.Builder, section healthSection) bool {
	if section.skipped != "" {
		fmt.Fprintf(sb, "Not collected: %s\n\n", section.skipped)
		return false
	}
	if section.err != nil && section.itemCount == 0 {
		fmt.Fprintf(sb, "Unavailable: %s\n\n", truncateText(section.err.Error(), healthMessageRunes))
		return false
	}
	if section.truncated || section.err != nil {
		sb.WriteString("Collection is incomplete; findings are limited to collected objects.\n\n")
	}
	return true
}

type healthIssue struct {
	key  string
	text string
}

func sortHealthIssues(issues []healthIssue) {
	sort.Slice(issues, func(i, j int) bool { return issues[i].key < issues[j].key })
}

func renderHealthIssues(sb *strings.Builder, issues []healthIssue, noIssues string) {
	if len(issues) == 0 {
		sb.WriteString(noIssues + "\n\n")
		return
	}
	limit := healthIssueRows
	if len(issues) < limit {
		limit = len(issues)
	}
	for _, issue := range issues[:limit] {
		fmt.Fprintf(sb, "- %s\n", issue.text)
	}
	if omitted := len(issues) - limit; omitted > 0 {
		fmt.Fprintf(sb, "- %d additional issue(s) omitted from the report\n", omitted)
	}
	sb.WriteString("\n")
}

func healthReason(prefix, message string) string {
	message = truncateText(message, healthMessageRunes)
	if message == "" {
		return prefix
	}
	return prefix + ": " + message
}

func namespacedName(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "/" + name
}

func truncateText(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}

func truncatePrompt(prompt string) string {
	runes := []rune(prompt)
	if len(runes) <= healthPromptRunes {
		return prompt
	}
	trailer := "\n\nReport output was truncated at the health-check size limit.\n"
	return string(runes[:healthPromptRunes-len([]rune(trailer))]) + trailer
}

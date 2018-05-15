package main

import (
	"regexp"

	"k8s.io/apimachinery/pkg/labels"

	"k8s.io/client-go/pkg/api/v1"
)

type Matcher interface {
	Match(interface{}) bool
}

type and []Matcher

func (m and) Match(value interface{}) bool {
	if len(m) == 0 {
		return true
	}
	for _, sm := range m {
		if !sm.Match(value) {
			return false
		}
	}
	return true
}

type or []Matcher

func (m or) Match(value interface{}) bool {
	if len(m) == 0 {
		return true
	}
	for _, sm := range m {
		if sm.Match(value) {
			return true
		}
	}
	return false
}

type not struct {
	matcher Matcher
}

func (m not) Match(value interface{}) bool {
	if m.matcher == nil {
		return true
	}
	v := !m.matcher.Match(value)
	return v
}

type regexMatcher struct {
	regexp *regexp.Regexp
}

func (m regexMatcher) Match(value interface{}) bool {
	var s string
	switch t := value.(type) {
	case *v1.Pod:
		return m.matchPod(t)
	case *v1.Container:
		s = t.Name
	default:
		return false
	}
	return m.regexp.MatchString(s)
}

func (m regexMatcher) matchPod(pod *v1.Pod) bool {
	if m.regexp.MatchString(pod.Name) {
		return true
	}
	for _, c := range pod.Spec.Containers {
		if m.Match(&c) {
			return true
		}
	}
	for _, c := range pod.Spec.InitContainers {
		if m.Match(&c) {
			return true
		}
	}
	return false
}

type labelSelectorMatcher struct {
	selector labels.Selector
}

func (m labelSelectorMatcher) Match(value interface{}) bool {
	switch t := value.(type) {
	case *v1.Pod:
		return m.selector.Matches(labels.Set(t.Labels))
	}
	return true
}

func buildMatcher(
	includeRegexps, excludeRegexps []*regexp.Regexp,
	labelSelector labels.Selector) Matcher {
	includes := make(or, len(includeRegexps))
	for i, r := range includeRegexps {
		includes[i] = regexMatcher{regexp: r}
	}

	var matcher Matcher = includes
	if len(excludeRegexps) > 0 {
		excludes := make(or, len(excludeRegexps))
		for i, r := range excludeRegexps {
			excludes[i] = regexMatcher{regexp: r}
		}
		matcher = and{matcher, not{matcher: excludes}}
	}

	return and{labelSelectorMatcher{labelSelector}, matcher}
}

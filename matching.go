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
		return false
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
		return false
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
		return false
	}
	v := !m.matcher.Match(value)
	return v
}

type regexMatcher struct {
	regexp *regexp.Regexp
}

func (m regexMatcher) Match(value interface{}) bool {
	switch t := value.(type) {
	case *v1.Pod:
		return m.regexp.MatchString(t.Name)
	case *v1.Container:
		return m.regexp.MatchString(t.Name)
	default:
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
	return false
}

type trueMatcher struct{}

func (trueMatcher) Match(value interface{}) bool {
	return true
}

type falseMatcher struct{}

func (falseMatcher) Match(value interface{}) bool {
	return false
}

func buildAnd(a, b Matcher) Matcher {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return and{a, b}
}

func buildMatcher(
	patterns []*regexp.Regexp,
	labelSelector labels.Selector,
	defaultMatch bool) Matcher {
	var matcher Matcher
	if len(patterns) > 0 {
		ors := make(or, len(patterns))
		for i, r := range patterns {
			ors[i] = regexMatcher{regexp: r}
		}
		matcher = ors
	} else if defaultMatch {
		matcher = trueMatcher{}
	} else {
		matcher = falseMatcher{}
	}
	if labelSelector != nil && !labelSelector.Empty() {
		matcher = and{labelSelectorMatcher{labelSelector}, matcher}
	}
	return matcher
}

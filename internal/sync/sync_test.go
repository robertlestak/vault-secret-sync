package sync

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsPathMatch tests the isPathMatch function
func TestIsPathMatch(t *testing.T) {
	cases := []struct {
		configPath string
		eventPath  string
		expected   bool
	}{
		{"secret/data/test", "secret/data/test", true},
		{"secret/data/test", "secret/data/other", false},
		{"secret/data/.*", "secret/data/test", true},
		{"secret/data/.*", "secret/data/other", true},
		{"secret/.*/test", "secret/data/test", true},
		{"secret/.*/test", "secret/other/test", true},
		{"secret/.*/test", "secret/other/notest", false},
		{"secret/.*", "secret/test", true},
		{"secret/.*", "secret/test/test", true},
		{"secret/.*", "secret", false},
		{"secret/.*test.*", "secret/test", true},
		{"secret/.*test.*", "secret/atestb", true},
		{"secret/.*test.*", "secret/a/b/test/c", true},
		{"secret/.*test.*", "secret/a/b/c", false},
		{"^secret/.*", "secret/test", true},
		{"^secret/.*", "notsecret/test", false},
		{"secret/[a-z]+/test", "secret/abc/test", true},
		{"secret/[a-z]+/test", "secret/ABC/test", false},
		{"secret/[a-z]+/test", "secret/abcd/test", true},
		{"secret/[a-z]+/test", "secret/abc123/test", false},
		{"secret/\\d+/test", "secret/123/test", true},
		{"secret/\\d+/test", "secret/abc/test", false},
		{"secret/\\d+/test", "secret/456/test", true},
		{"secret/(data|other)/test", "secret/data/test", true},
		{"secret/(data|other)/test", "secret/other/test", true},
		{"secret/(data|other)/test", "secret/else/test", false},
		{"secret/foo/[a-zA-Z]*", "secret/foo/something", true},
		{"secret/foo/[a-zA-Z]*", "secret/foo/123", false},
		{"secret/foo/[a-zA-Z]*", "secret/foo/bar", true},
		{"secret/[a-z]+/baz", "secret/abc/baz", true},
		{"secret/[a-z]+/baz", "secret/123/baz", false},
		{"secret/foo/[a-zA-Z]*", "secret/foo/123", false},
		{"secret/foo/[a-zA-Z]*", "secret/foo/bar", true},
		{"secret/[a-z]+/baz", "secret/abc/baz", true},
		{"secret/[a-z]+/baz", "secret/123/baz", false},
		{"secret/foo/[a-z]+", "secret/foo/bar", true},
		{"secret/foo/[a-z]+", "secret/foo/bar/", false},
		{"secret/foo/[a-z]+", "secret/foo/bar/baz", false},
		{"secret/foo/[a-z]+/baz", "secret/foo/bar/baz", true},
		{"secret/foo/[a-z]+/baz", "secret/foo/bar/baz/", false},
		{"secret/foo/.*", "secret/foo/bar/baz", true},
		{"secret/foo/.*", "secret/foo/bar/baz/qux", true},
		{"secret/foo/.*", "secret/foo/bar/baz/qux/", true},
	}

	for _, c := range cases {
		t.Run(c.configPath+"-"+c.eventPath, func(t *testing.T) {
			result := isPathMatch(c.configPath, c.eventPath)
			assert.Equal(t, c.expected, result)
		})
	}
}

// TestFindHighestNonRegexPath tests the findHighestNonRegexPath function
func TestFindHighestNonRegexPath(t *testing.T) {
	cases := []struct {
		path     string
		expected string
	}{
		{"secret/data/test/.*", "secret/data/test"},
		{"secret/data/.*", "secret/data"},
		{"secret/.*/test", "secret"},
		{"secret/data/test", "secret/data/test"},
		{"secret/data/test/.*", "secret/data/test"},
		{"secret/.*", "secret"},
		{"secret/test/.*", "secret/test"},
		{"secret/.*/.*/test", "secret"},
		{"secret/[a-z]+/test", "secret"},
		{"secret/\\d+/test", "secret"},
		{"secret/(data|other)/test", "secret"},
	}

	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			result := findHighestNonRegexPath(c.path)
			assert.Equal(t, c.expected, result)
		})
	}
}

// TestRegexRewrite tests the regex rewrite logic
func TestRegexRewrite(t *testing.T) {
	cases := []struct {
		configPath   string
		eventPath    string
		destPath     string
		expectedPath string
	}{
		{
			configPath:   "secret/data/(.*)",
			eventPath:    "secret/data/test",
			destPath:     "dest/$1",
			expectedPath: "dest/test",
		},
		{
			configPath:   "secret/(data|other)/(.*)",
			eventPath:    "secret/data/test",
			destPath:     "dest/$1/$2",
			expectedPath: "dest/data/test",
		},
		{
			configPath:   "secret/(.*)/test",
			eventPath:    "secret/data/test",
			destPath:     "dest/$1",
			expectedPath: "dest/data",
		},
		{
			configPath:   "secret/(data|other)/test/(.*)",
			eventPath:    "secret/data/test/abc",
			destPath:     "dest/$1/$2",
			expectedPath: "dest/data/abc",
		},
		{
			configPath:   "secret/(data|other)/(test|example)/(.*)",
			eventPath:    "secret/other/test/abc/def",
			destPath:     "dest/$1/$2/$3",
			expectedPath: "dest/other/test/abc/def",
		},
		{
			configPath:   "secret/(.*)",
			eventPath:    "secret/other/test/abc/def",
			destPath:     "dest/$1",
			expectedPath: "dest/other/test/abc/def",
		},
		{
			configPath:   "secret/foo/bar/(.*)",
			eventPath:    "secret/foo/bar/other/test/abc/def",
			destPath:     "dest/hello/world/$1",
			expectedPath: "dest/hello/world/other/test/abc/def",
		},
	}

	for _, c := range cases {
		t.Run(c.configPath+"-"+c.eventPath, func(t *testing.T) {
			rx, err := regexp.Compile(c.configPath)
			assert.NoError(t, err)

			matches := rx.FindStringSubmatch(c.eventPath)
			assert.NotNil(t, matches)

			rewritePath := c.destPath
			for i, match := range matches {
				groupName := fmt.Sprintf("$%d", i)
				rewritePath = strings.ReplaceAll(rewritePath, groupName, match)
			}

			assert.Equal(t, c.expectedPath, rewritePath)
		})
	}
}

// TestSyncPathValidation tests the synchronization path validation
func TestSyncPathValidation(t *testing.T) {
	cases := []struct {
		configPath   string
		eventPath    string
		destPath     string
		expectedPath string
	}{
		{
			configPath:   "secret/data/(.*)",
			eventPath:    "secret/data/test",
			destPath:     "dest/$1",
			expectedPath: "dest/test",
		},
		{
			configPath:   "secret/(data|other)/(.*)",
			eventPath:    "secret/data/test",
			destPath:     "dest/$1/$2",
			expectedPath: "dest/data/test",
		},
		{
			configPath:   "secret/(.*)/test",
			eventPath:    "secret/data/test",
			destPath:     "dest/$1",
			expectedPath: "dest/data",
		},
		{
			configPath:   "secret/(data|other)/test/(.*)",
			eventPath:    "secret/data/test/abc",
			destPath:     "dest/$1/$2",
			expectedPath: "dest/data/abc",
		},
		{
			configPath:   "secret/(data|other)/(test|example)/(.*)",
			eventPath:    "secret/other/test/abc/def",
			destPath:     "dest/$1/$2/$3",
			expectedPath: "dest/other/test/abc/def",
		},
		{
			configPath:   "secret/(.*)",
			eventPath:    "secret/other/test/abc/def",
			destPath:     "dest/$1",
			expectedPath: "dest/other/test/abc/def",
		},
		{
			configPath:   "secret/foo/bar/(.*)",
			eventPath:    "secret/foo/bar/other/test/abc/def",
			destPath:     "dest/hello/world/$1",
			expectedPath: "dest/hello/world/other/test/abc/def",
		},
		{
			configPath:   "secret/foo/bar",
			eventPath:    "secret/foo/bar/other/test/abc/def",
			destPath:     "dest/hello/world",
			expectedPath: "dest/hello/world",
		},
		{
			configPath:   "secret/foo/bar",
			eventPath:    "secret/foo/bar",
			destPath:     "dest/hello/world",
			expectedPath: "dest/hello/world",
		},
		{
			configPath:   "secret/foo/bar/(.*)/baz",
			eventPath:    "secret/foo/bar/other/baz",
			destPath:     "dest/$1",
			expectedPath: "dest/other",
		},
		{
			configPath:   "secret/foo/bar/(.*)",
			eventPath:    "secret/foo/bar/other/baz",
			destPath:     "dest/$1",
			expectedPath: "dest/other/baz",
		},
	}

	for _, c := range cases {
		t.Run(c.configPath+"-"+c.eventPath, func(t *testing.T) {
			rx, err := regexp.Compile(c.configPath)
			assert.NoError(t, err)

			if isPathMatch(c.configPath, c.eventPath) {
				matches := rx.FindStringSubmatch(c.eventPath)
				assert.NotNil(t, matches)

				rewritePath := c.destPath
				for i, match := range matches {
					groupName := fmt.Sprintf("$%d", i)
					rewritePath = strings.ReplaceAll(rewritePath, groupName, match)
				}

				assert.Equal(t, c.expectedPath, rewritePath)
			} else {
				// If the path doesn't match, the expected path should be the original destPath
				assert.Equal(t, c.destPath, c.expectedPath)
			}
		})
	}
}

func TestRegexPathMatching(t *testing.T) {
	cases := []struct {
		configPath string
		eventPath  string
		expected   bool
	}{
		{"secret/data/test", "secret/data/test", true},
		{"secret/data/test", "secret/data/other", false},
		{"secret/data/.*", "secret/data/test", true},
		{"secret/data/.*", "secret/data/other", true},
		{"secret/.*/test", "secret/data/test", true},
		{"secret/.*/test", "secret/other/test", true},
		{"secret/.*/test", "secret/other/notest", false},
	}

	for _, c := range cases {
		t.Run(c.configPath+"-"+c.eventPath, func(t *testing.T) {
			result := isPathMatch(c.configPath, c.eventPath)
			assert.Equal(t, c.expected, result)
		})
	}
}

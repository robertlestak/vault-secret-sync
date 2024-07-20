package transforms

import (
	"path"
	"regexp"
	"strings"

	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
	log "github.com/sirupsen/logrus"
)

func shouldFilterStringRegex(sc v1alpha1.VaultSecretSync, str string) bool {
	l := log.WithFields(log.Fields{
		"action": "shouldFilterStringRegex",
	})

	if sc.Spec.Filters == nil || sc.Spec.Filters.Regex == nil {
		return false
	}
	// if the exclude list is not empty, check if the string matches any of the exclude regexes
	if len(sc.Spec.Filters.Regex.Exclude) > 0 {
		for _, r := range sc.Spec.Filters.Regex.Exclude {
			if match, err := regexp.MatchString(r, str); err != nil {
				l.Error(err)
			} else if match {
				l.Debugf("string %s matches exclude regex %s", str, r)
				return true
			}
		}
	}
	// if the include list is not empty, check if the string matches any of the include regexes
	if len(sc.Spec.Filters.Regex.Include) > 0 {
		for _, r := range sc.Spec.Filters.Regex.Include {
			if match, err := regexp.MatchString(r, str); err != nil {
				l.Error(err)
			} else if match {
				l.Debugf("string %s matches include regex %s", str, r)
				return false
			}
		}
		// if the string didn't match any of the include regexes, filter it
		l.Debugf("string %s did not match any include regexes", str)
		return true
	}
	// if there are no include regexes, don't filter the string
	l.Debugf("no include regexes, not filtering string %s", str)
	return false
}

func VaultPathsFromPath(in string) (data string, metadata string) {
	// split the input on /
	parts := strings.Split(in, "/")
	if len(parts) < 2 {
		return in, in
	}
	// create two strings, one for the data path and one for the metadata path
	dp := path.Join(parts[0], "data", path.Join(parts[1:]...))
	mp := path.Join(parts[0], "metadata", path.Join(parts[1:]...))
	return dp, mp
}

func shouldFilterStringPath(sc v1alpha1.VaultSecretSync, str string) bool {
	l := log.WithFields(log.Fields{
		"action": "shouldFilterStringPath",
	})

	if sc.Spec.Filters == nil || sc.Spec.Filters.Path == nil {
		return false
	}
	// if the exclude list is not empty, check if the string matches any of the exclude paths
	if len(sc.Spec.Filters.Path.Exclude) > 0 {
		for _, p := range sc.Spec.Filters.Path.Exclude {
			dp, mp := VaultPathsFromPath(p)
			if str == dp || str == mp || str == p {
				l.Debugf("string %s matches exclude path %s", str, p)
				return true
			}
		}
	}
	// if the include list is not empty, check if the string matches any of the include paths
	if len(sc.Spec.Filters.Path.Include) > 0 {
		for _, p := range sc.Spec.Filters.Path.Include {
			dp, mp := VaultPathsFromPath(p)
			if str == dp || str == mp || str == p {
				l.Debugf("string %s matches include path %s", str, p)
				return false
			}
		}
		// if the string didn't match any of the include paths, filter it
		l.Debugf("string %s did not match any include paths", str)
		return true
	}
	// if there are no include paths, don't filter the string
	l.Debugf("no include paths, not filtering string %s", str)
	return false
}

func ShouldFilterString(sc v1alpha1.VaultSecretSync, str string) bool {
	if sc.Spec.Filters == nil {
		return false
	}
	if sc.Spec.Filters.Regex != nil {
		if shouldFilterStringRegex(sc, str) {
			return true
		}
	}
	if sc.Spec.Filters.Path != nil {
		if shouldFilterStringPath(sc, str) {
			return true
		}
	}
	return false
}

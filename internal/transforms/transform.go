package transforms

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"text/template"

	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
)

func ExecuteTransformTemplate(sc v1alpha1.VaultSecretSync, secret []byte) ([]byte, error) {
	if sc.Spec.Transforms == nil || sc.Spec.Transforms.Template == nil || *sc.Spec.Transforms.Template == "" {
		return secret, nil
	}
	t, err := template.New("transform").Funcs(template.FuncMap{
		"json": func(v interface{}) string {
			// Convert the value to JSON for more complex structures
			bytes, err := json.Marshal(v)
			if err != nil {
				return ""
			}
			return string(bytes)
		},
		"string": func(v interface{}) string {
			// Convert the value to a string
			return fmt.Sprintf("%v", v)
		},
		"int": func(v interface{}) int {
			// Convert the value to an int
			return v.(int)
		},
	}).Parse(strings.TrimSpace(*sc.Spec.Transforms.Template))
	if err != nil {
		return secret, err
	}
	var buf bytes.Buffer
	var secretData map[string]any
	err = json.Unmarshal(secret, &secretData)
	if err != nil {
		return secret, nil
	}
	err = t.Execute(&buf, secretData)
	if err != nil {
		return secret, err
	}
	return buf.Bytes(), nil
}

func ExecuteRenameTransforms(sc v1alpha1.VaultSecretSync, secret []byte) ([]byte, error) {
	if sc.Spec.Transforms == nil || sc.Spec.Transforms.Rename == nil {
		return secret, nil
	}
	newSecret := make(map[string]any)
	secretData := make(map[string]any)
	if err := json.Unmarshal(secret, &secretData); err != nil {
		return secret, nil
	}
	for k, v := range secretData {
		newKey := k
		for _, r := range sc.Spec.Transforms.Rename {
			if r.From == k {
				newKey = r.To
			}
		}
		newSecret[newKey] = v
	}
	jd, err := json.Marshal(newSecret)
	if err != nil {
		return secret, nil
	}
	return jd, nil
}

// isRegex determines if the provided string is a regex or a literal string
func isRegex(path string) bool {
	if !strings.ContainsAny(path, "[](){}+*?|") {
		return false
	}
	_, err := regexp.Compile(path)
	return err == nil
}

func ExecuteIncludeTransforms(sc v1alpha1.VaultSecretSync, secret []byte) ([]byte, error) {
	if sc.Spec.Transforms == nil || sc.Spec.Transforms.Include == nil {
		return secret, nil
	}
	newSecret := make(map[string]any)
	secretData := make(map[string]any)
	if err := json.Unmarshal(secret, &secretData); err != nil {
		return secret, nil
	}
	for _, i := range sc.Spec.Transforms.Include {
		// if i is a regex, check regex match
		// if not a regex, check for key match
		if isRegex(i) {
			re, err := regexp.Compile(i)
			if err != nil {
				continue
			}
			for k, v := range secretData {
				if re.MatchString(k) {
					newSecret[k] = v
				}
			}
			continue
		} else {
			if v, ok := secretData[i]; ok {
				newSecret[i] = v
			}
		}
	}
	jd, err := json.Marshal(newSecret)
	if err != nil {
		return secret, nil
	}
	return jd, nil
}

func ExecuteExcludeTransforms(sc v1alpha1.VaultSecretSync, secret []byte) ([]byte, error) {
	if sc.Spec.Transforms == nil || sc.Spec.Transforms.Exclude == nil {
		return secret, nil
	}
	newSecret := make(map[string]any)
	secretData := make(map[string]any)
	if err := json.Unmarshal(secret, &secretData); err != nil {
		return secret, nil
	}
	for k, v := range secretData {
		include := true
		for _, e := range sc.Spec.Transforms.Exclude {
			if isRegex(e) {
				re, err := regexp.Compile(e)
				if err != nil {
					continue
				}
				if re.MatchString(k) {
					include = false
					break
				}
			} else {
				if e == k {
					include = false
					break
				}
			}
		}
		if include {
			newSecret[k] = v
		}
	}
	jd, err := json.Marshal(newSecret)
	if err != nil {
		return secret, nil
	}
	return jd, nil
}

func ExecuteTransforms(sc v1alpha1.VaultSecretSync, secret []byte) ([]byte, error) {
	if sc.Spec.Transforms == nil {
		return secret, nil
	}
	ns := secret
	var err error
	ns, err = ExecuteExcludeTransforms(sc, ns)
	if err != nil {
		return secret, err
	}
	ns, err = ExecuteIncludeTransforms(sc, ns)
	if err != nil {
		return secret, err
	}
	ns, err = ExecuteRenameTransforms(sc, ns)
	if err != nil {
		return secret, err
	}
	ns, err = ExecuteTransformTemplate(sc, ns)
	if err != nil {
		return secret, err
	}
	return ns, nil
}

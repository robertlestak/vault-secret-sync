package transforms

import (
	"bytes"
	"encoding/json"
	"text/template"

	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
)

func ExecuteTransformTemplate(sc v1alpha1.VaultSecretSync, secret map[string]any) (map[string]any, error) {
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
			return v.(string)
		},
		"int": func(v interface{}) int {
			// Convert the value to an int
			return v.(int)
		},
	}).Parse(*sc.Spec.Transforms.Template)
	if err != nil {
		return secret, err
	}
	var buf bytes.Buffer
	err = t.Execute(&buf, secret)
	if err != nil {
		return secret, err
	}
	newSecret := make(map[string]any)
	err = json.Unmarshal(buf.Bytes(), &newSecret)
	if err != nil {
		return secret, err
	}
	return newSecret, nil
}

func ExecuteRenameTransforms(sc v1alpha1.VaultSecretSync, secret map[string]any) (map[string]any, error) {
	if sc.Spec.Transforms == nil || sc.Spec.Transforms.Rename == nil {
		return secret, nil
	}
	newSecret := make(map[string]any)
	for k, v := range secret {
		newKey := k
		for _, r := range sc.Spec.Transforms.Rename {
			if r.From == k {
				newKey = r.To
			}
		}
		newSecret[newKey] = v
	}
	return newSecret, nil
}

func ExecuteIncludeTransforms(sc v1alpha1.VaultSecretSync, secret map[string]any) (map[string]any, error) {
	if sc.Spec.Transforms == nil || sc.Spec.Transforms.Include == nil {
		return secret, nil
	}
	newSecret := make(map[string]any)
	for _, i := range sc.Spec.Transforms.Include {
		if v, ok := secret[i]; ok {
			newSecret[i] = v
		}
	}
	return newSecret, nil
}

func ExecuteExcludeTransforms(sc v1alpha1.VaultSecretSync, secret map[string]any) (map[string]any, error) {
	if sc.Spec.Transforms == nil || sc.Spec.Transforms.Exclude == nil {
		return secret, nil
	}
	newSecret := make(map[string]any)
	for k, v := range secret {
		include := true
		for _, e := range sc.Spec.Transforms.Exclude {
			if e == k {
				include = false
				break
			}
		}
		if include {
			newSecret[k] = v
		}
	}
	return newSecret, nil
}

func ExecuteTransforms(sc v1alpha1.VaultSecretSync, secret map[string]any) (map[string]any, error) {
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

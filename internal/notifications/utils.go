package notifications

import (
	"bytes"
	"encoding/json"
	"fmt"
	"text/template"

	"github.com/robertlestak/vault-secret-sync/api/v1alpha1"
	log "github.com/sirupsen/logrus"
)

// renderTemplate renders the given template with the provided data
func renderTemplate(tmplString string, data v1alpha1.NotificationMessage) (string, error) {
	var tplOutput bytes.Buffer

	// Create a new template and register custom functions
	tmpl, err := template.New("webhookPayload").Funcs(template.FuncMap{
		"json": func(v interface{}) string {
			// Convert the value to JSON for more complex structures
			bytes, err := json.Marshal(v)
			if err != nil {
				return fmt.Sprintf("error marshaling JSON: %v", err)
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
	}).Parse(tmplString)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %v", err)
	}

	err = tmpl.Execute(&tplOutput, data)
	if err != nil {
		return "", fmt.Errorf("failed to execute template: %v", err)
	}
	return tplOutput.String(), nil
}

func messagePayload(message v1alpha1.NotificationMessage, body string) string {
	l := log.WithFields(log.Fields{"action": "messagePayload"})
	l.Trace("start")
	defer l.Trace("end")
	l.Debugf("message: %v, body: %s", message, body)
	if body != "" {
		l.Debugf("using custom body: %s", body)
		payload, err := renderTemplate(body, message)
		if err != nil {
			l.Warnf("failed to render custom body: %v", err)
			// add the error message to the body so the user can see what went wrong
			// without having to dig through the logs
			body += fmt.Sprintf("\n\nError rendering custom body: %v", err)
			return body
		}
		l.Debugf("custom body: %s", payload)
		return payload
	} else {
		l.Debug("using default body")
		// Marshal the data to JSON
		payload, err := json.Marshal(message)
		if err != nil {
			return ""
		}
		l.Debugf("default body: %s", string(payload))
		return string(payload)
	}
}

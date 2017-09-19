package main

import (
	"fmt"
	"html/template"
	"io"
	"path/filepath"

	cfclient "github.com/cloudfoundry-community/go-cfclient"
)

const (
	notifyTemplate = "NOTIFY_TEMPLATE"
)

// Templates serve as a mapping to various templates.
// Each entry can be a compliation of multiple files mapped to a string entry.
// This works if we ever want to use the .define blocks which are good for
// creating a main template with swappable content.
// Similar to https://hackernoon.com/golang-template-2-template-composition-and-how-to-organize-template-files-4cb40bcdf8f6
type Templates struct {
	templates map[string]*template.Template
}

// initTemplates will try to parse the templates.
func initTemplates() (*Templates, error) {
	templates := make(map[string]*template.Template)
	for templateName, templatePath := range findTemplates() {
		tpl, err := template.ParseFiles(templatePath...)
		if err != nil {
			return nil, err
		}
		templates[templateName] = tpl
	}
	return &Templates{templates}, nil
}

// findTemplates will try to construct to final path of where to find templates
// given the basePath of where to look.
func findTemplates() map[string][]string {
	return map[string][]string{
		notifyTemplate: []string{filepath.Join("templates", "mail", "notify.tmpl")},
	}
}

func (t *Templates) getTemplate(templateKey string) (*template.Template, error) {
	if template, ok := t.templates[templateKey]; ok {
		return template, nil
	}
	return nil, fmt.Errorf("unable to find template with key %s", templateKey)
}

// notifyEmail provides struct for the templates/mail/notify.tmpl
type notifyEmail struct {
	Username string
	Apps     []cfclient.App
	AppNoun  string
}

// getNotifyEmail gets the filled in notify email template.
func (t *Templates) getNotifyEmail(rw io.Writer, email notifyEmail) error {
	tpl, err := t.getTemplate(notifyTemplate)
	if err != nil {
		return err
	}
	return tpl.Execute(rw, email)
}

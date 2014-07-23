package inspeqtor_test

import (
	"bytes"
	"fmt"
	"inspeqtor"
	"inspeqtor/core"
	"log"
	"testing"
)

func TestGmailNotifier(t *testing.T) {
	i, err := inspeqtor.SetupNotification("gmail", map[string]string{
		"username": "mike",
		"password": "fuzzbucket",
		"to":       "mike@example.org",
	})
	ok(t, err)
	assert(t, i != nil, "Expecting valid notifier")
}

func TestEmailNotifier(t *testing.T) {
	i, err := inspeqtor.SetupNotification("email", map[string]string{
		"username": "mike",
		"password": "fuzzbucket",
		"hostname": "smtp.example.com",
		"to":       "mike@example.org",
	})
	ok(t, err)
	assert(t, i != nil, "Expecting valid notifier")
}

func TestMissingEmailNotifier(t *testing.T) {
	i, err := inspeqtor.SetupNotification("email", map[string]string{
		"username": "mike",
		"password": "fuzzbucket",
		"to":       "mike@example.org",
	})
	assert(t, err != nil, "Missing data should cause error")
	log.Printf("%v", i)
	assert(t, i == nil, "Missing data should not return notifier")
}

func TestEmailTrigger(t *testing.T) {
	alert := &core.Alert{
		&core.Service{"mysql", nil, nil},
		&core.Rule{"rss", core.GT, 64 * 1024 * 1024, 1, core.Ok, nil},
	}
	err := validEmailSetup().TriggerEmail(alert, acceptEmail)
	ok(t, err)
}

func acceptEmail(e *inspeqtor.EmailConfig, doc bytes.Buffer) error {
	fmt.Println(string(doc.Bytes()))
	return nil
}

func validEmailSetup() *inspeqtor.EmailConfig {
	return &inspeqtor.EmailConfig{
		"mike", "fuzzbucket", "smtp.gmail.com", "mike@example.org", "mperham@gmail.com"}
}

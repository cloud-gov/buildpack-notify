package main

import (
	"net/smtp"

	"github.com/jordan-wright/email"
)

// Mailer is a interface that any mailer should implement.
type Mailer interface {
	SendEmail(emailAddress string, subject string, body []byte) error
}

// InitSMTPMailer creates a new SMTP Mailer
func InitSMTPMailer(config emailConfig) Mailer {
	return &smtpMailer{
		smtpHost: config.host,
		smtpPort: config.port,
		smtpUser: config.user,
		smtpPass: config.password,
		smtpFrom: config.from,
	}
}

type smtpMailer struct {
	smtpHost string
	smtpPort string
	smtpUser string
	smtpPass string
	smtpFrom string
}

func (s *smtpMailer) SendEmail(emailAddress, subject string, body []byte) error {
	e := email.NewEmail()
	e.From = "cloud.gov <" + s.smtpFrom + ">"
	e.To = []string{" <" + emailAddress + ">"}
	e.Text = body
	e.Subject = subject
	return e.Send(s.smtpHost+":"+s.smtpPort, smtp.PlainAuth("", s.smtpUser, s.smtpPass, s.smtpHost))
}

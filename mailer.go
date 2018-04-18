package main

import (
	"crypto/tls"
	"crypto/x509"
	"net/smtp"

	"github.com/jordan-wright/email"
)

// Mailer is a interface that any mailer should implement.
type Mailer interface {
	SendEmail(emailAddress string, subject string, body []byte) error
}

// InitSMTPMailer creates a new SMTP Mailer
func InitSMTPMailer(config EmailConfig) Mailer {
	var tlsConfig *tls.Config
	if config.Cert != "" {
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM([]byte(config.Cert))
		tlsConfig = &tls.Config{
			ServerName: config.Host,
			RootCAs:    pool,
		}

	}
	return &smtpMailer{
		smtpHost:  config.Host,
		smtpPort:  config.Port,
		smtpUser:  config.User,
		smtpPass:  config.Password,
		smtpFrom:  config.From,
		tlsConfig: tlsConfig,
	}
}

type smtpMailer struct {
	smtpHost  string
	smtpPort  string
	smtpUser  string
	smtpPass  string
	smtpFrom  string
	tlsConfig *tls.Config
}

func (s *smtpMailer) SendEmail(emailAddress, subject string, body []byte) error {
	e := email.NewEmail()
	e.From = "cloud.gov <" + s.smtpFrom + ">"
	e.To = []string{" <" + emailAddress + ">"}
	e.Text = body
	e.Subject = subject

	addr := s.smtpHost + ":" + s.smtpPort
	auth := smtp.PlainAuth("", s.smtpUser, s.smtpPass, s.smtpHost)

	if s.tlsConfig != nil {
		return e.SendWithTLS(addr, auth, s.tlsConfig)
	}
	return e.Send(addr, auth)
}

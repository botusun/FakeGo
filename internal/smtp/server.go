package smtp

import (
	"fakego/internal/mail"
	"time"

	gosmtp "github.com/emersion/go-smtp"
)

// NewServer builds and configures an SMTP server that accepts all mail.
func NewServer(addr string, saver *mail.Saver, relayDomains []string) *gosmtp.Server {
	be := &backend{saver: saver, relayDomains: relayDomains}
	srv := gosmtp.NewServer(be)
	srv.Addr = addr
	srv.Domain = "localhost"
	srv.ReadTimeout = 10 * time.Second
	srv.WriteTimeout = 10 * time.Second
	srv.MaxMessageBytes = 25 * 1024 * 1024 // 25 MB
	srv.MaxRecipients = 50
	srv.AllowInsecureAuth = true // allow AUTH without TLS
	return srv
}

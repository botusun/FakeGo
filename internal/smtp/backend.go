package smtp

import (
	"bytes"
	"fakego/internal/mail"
	"io"
	"log"
	"strings"

	gosmtp "github.com/emersion/go-smtp"
)

type backend struct {
	saver        *mail.Saver
	relayDomains []string
}

func (b *backend) NewSession(_ *gosmtp.Conn) (gosmtp.Session, error) {
	return &session{b: b}, nil
}

type session struct {
	b          *backend
	from       string
	recipients []string
}

// AuthPlain accepts any credentials — this is a fake server for testing.
func (s *session) AuthPlain(_, _ string) error {
	return nil
}

func (s *session) Mail(from string, _ *gosmtp.MailOptions) error {
	s.from = from
	return nil
}

func (s *session) Rcpt(to string, _ *gosmtp.RcptOptions) error {
	if len(s.b.relayDomains) > 0 {
		for _, domain := range s.b.relayDomains {
			if strings.HasSuffix(to, domain) {
				s.recipients = append(s.recipients, to)
				return nil
			}
		}
		log.Printf("Rejecting recipient %s: not in relay domains", to)
		return &gosmtp.SMTPError{
			Code:         550,
			EnhancedCode: gosmtp.EnhancedCode{5, 7, 1},
			Message:      "relay access denied",
		}
	}
	s.recipients = append(s.recipients, to)
	return nil
}

// Data is called once per message with all accepted recipients already set.
func (s *session) Data(r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	for _, to := range s.recipients {
		if _, err := s.b.saver.Save(s.from, to, bytes.NewReader(data)); err != nil {
			log.Printf("Error saving email for %s: %v", to, err)
		}
	}
	return nil
}

func (s *session) Reset() {
	s.from = ""
	s.recipients = nil
}

func (s *session) Logout() error {
	return nil
}

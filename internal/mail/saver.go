package mail

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	subjectRe = regexp.MustCompile(`(?m)^Subject: (.*)$`)
	fromRe    = regexp.MustCompile(`(?m)^From: (.*)$`)
	toRe      = regexp.MustCompile(`(?m)^To: (.*)$`)
)

// Email is a received message stored in memory (and optionally on disk).
type Email struct {
	ID         int       `json:"id"`
	From       string    `json:"from"`
	To         string    `json:"to"`
	Subject    string    `json:"subject"`
	ReceivedAt time.Time `json:"received_at"`
	Content    string    `json:"content,omitempty"`
	filePath   string
}

// Event is pushed to SSE subscribers.
type Event struct {
	Type       string     `json:"type"`
	ID         int        `json:"id,omitempty"`
	From       string     `json:"from,omitempty"`
	To         string     `json:"to,omitempty"`
	Subject    string     `json:"subject,omitempty"`
	ReceivedAt *time.Time `json:"received_at,omitempty"`
}

// Saver saves incoming emails and notifies SSE subscribers.
type Saver struct {
	outputDir  string
	memoryMode bool
	mu         sync.RWMutex
	emails     []*Email
	nextID     int
	subs       []chan Event
}

func NewSaver(outputDir string, memoryMode bool) *Saver {
	return &Saver{outputDir: outputDir, memoryMode: memoryMode, nextID: 1}
}

// LoadFromDir reads existing .eml files from the output directory into memory.
// Called once at startup; does not trigger SSE notifications.
func (s *Saver) LoadFromDir() {
	if s.memoryMode || s.outputDir == "" {
		return
	}

	entries, err := os.ReadDir(s.outputDir)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Warning: could not read output dir: %v", err)
		}
		return
	}

	type emlFile struct {
		path    string
		modTime time.Time
	}
	var files []emlFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".eml") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, emlFile{
			path:    filepath.Join(s.outputDir, entry.Name()),
			modTime: info.ModTime(),
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.Before(files[j].modTime)
	})

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, f := range files {
		data, err := os.ReadFile(f.path)
		if err != nil {
			log.Printf("Warning: could not read %s: %v", f.path, err)
			continue
		}
		content := string(data)
		s.emails = append(s.emails, &Email{
			ID:         s.nextID,
			From:       extractHeader(fromRe, content),
			To:         extractHeader(toRe, content),
			Subject:    extractHeader(subjectRe, content),
			ReceivedAt: f.modTime,
			Content:    content,
			filePath:   f.path,
		})
		s.nextID++
	}

	if len(files) > 0 {
		log.Printf("Loaded %d existing email(s) from %s", len(files), s.outputDir)
	}
}

func (s *Saver) Save(from, to string, r io.Reader) (*Email, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading message data: %w", err)
	}

	body := string(data)

	s.mu.Lock()
	defer s.mu.Unlock()

	email := &Email{
		ID:         s.nextID,
		From:       from,
		To:         to,
		Subject:    extractHeader(subjectRe, body),
		Content:    body,
		ReceivedAt: time.Now(),
	}
	s.nextID++

	log.Printf("[%d] From: %s  To: %s  Subject: %s", email.ID, from, to, email.Subject)

	if !s.memoryMode {
		path, err := s.writeToFile(body)
		if err != nil {
			log.Printf("[%d] Warning: could not save to disk: %v", email.ID, err)
		} else {
			email.filePath = path
			log.Printf("[%d] Saved: %s", email.ID, path)
		}
	}

	s.emails = append(s.emails, email)
	t := email.ReceivedAt
	s.broadcast(Event{
		Type:       "email",
		ID:         email.ID,
		From:       email.From,
		To:         email.To,
		Subject:    email.Subject,
		ReceivedAt: &t,
	})

	return email, nil
}

// List returns summaries (no Content) newest-first.
func (s *Saver) List() []Email {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Email, len(s.emails))
	for i, e := range s.emails {
		out[len(s.emails)-1-i] = Email{
			ID:         e.ID,
			From:       e.From,
			To:         e.To,
			Subject:    e.Subject,
			ReceivedAt: e.ReceivedAt,
		}
	}
	return out
}

// Get returns the full email including Content, or nil if not found.
func (s *Saver) Get(id int) *Email {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, e := range s.emails {
		if e.ID == id {
			cp := *e
			return &cp
		}
	}
	return nil
}

// Clear deletes all emails from memory and (if applicable) from disk.
func (s *Saver) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.memoryMode {
		for _, e := range s.emails {
			if e.filePath == "" {
				continue
			}
			if err := os.Remove(e.filePath); err != nil && !os.IsNotExist(err) {
				log.Printf("Warning: could not delete %s: %v", e.filePath, err)
			}
		}
	}

	s.emails = nil
	s.broadcast(Event{Type: "clear"})
	log.Println("All emails cleared")
}

// Subscribe returns a channel that receives SSE events.
func (s *Saver) Subscribe() chan Event {
	ch := make(chan Event, 32)
	s.mu.Lock()
	s.subs = append(s.subs, ch)
	s.mu.Unlock()
	return ch
}

// Unsubscribe removes ch from the subscriber list and closes it.
func (s *Saver) Unsubscribe(ch chan Event) {
	s.mu.Lock()
	for i, sub := range s.subs {
		if sub == ch {
			s.subs = append(s.subs[:i], s.subs[i+1:]...)
			break
		}
	}
	s.mu.Unlock()
	close(ch)
}

// broadcast sends e to all subscribers without blocking.
// Must be called with s.mu held.
func (s *Saver) broadcast(e Event) {
	for _, ch := range s.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

func (s *Saver) writeToFile(content string) (string, error) {
	base := time.Now().Format("020106150405.000")
	base = strings.ReplaceAll(base, ".", "")

	for i := 0; ; i++ {
		suffix := ""
		if i > 0 {
			suffix = fmt.Sprintf("%d", i)
		}
		name := filepath.Join(s.outputDir, base+suffix+".eml")
		f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.WriteString(content)
		f.Close()
		return name, werr
	}
}

// extractHeader searches only the header section (before the first blank line)
// so body content cannot accidentally match.
func extractHeader(re *regexp.Regexp, content string) string {
	end := strings.Index(content, "\r\n\r\n")
	if end < 0 {
		end = strings.Index(content, "\n\n")
	}
	if end >= 0 {
		content = content[:end]
	}
	if m := re.FindStringSubmatch(content); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

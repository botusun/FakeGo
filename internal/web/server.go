package web

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/http"
	"net/mail"
	"strconv"
	"strings"

	mailpkg "fakego/internal/mail"
)

//go:embed static/index.html
var indexHTML []byte

// Server serves the web UI and REST/SSE API.
type Server struct {
	smtpAddr string
	saver    *mailpkg.Saver
	mux      *http.ServeMux
}

func NewServer(smtpAddr string, saver *mailpkg.Saver) *Server {
	s := &Server{smtpAddr: smtpAddr, saver: saver}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/emails", s.handleEmailsList)
	mux.HandleFunc("DELETE /api/emails", s.handleEmailsClear)
	mux.HandleFunc("GET /api/emails/{id}", s.handleEmailByID)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("/", s.handleIndex)
	s.mux = mux
	return s
}

func (s *Server) ListenAndServe(addr string) error {
	return http.ListenAndServe(addr, s.mux)
}

// emailDetailResponse is the shape returned by GET /api/emails/{id}.
type emailDetailResponse struct {
	*mailpkg.Email
	HTMLBody string `json:"html_body,omitempty"`
}

// --- handlers ---

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexHTML)
}

func (s *Server) handleEmailsList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.saver.List())
}

func (s *Server) handleEmailsClear(w http.ResponseWriter, r *http.Request) {
	s.saver.Clear()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleEmailByID(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	email := s.saver.Get(id)
	if email == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, emailDetailResponse{
		Email:    email,
		HTMLBody: extractHTMLBody(email.Content),
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"smtp_addr": s.smtpAddr})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	fmt.Fprintf(w, "data: {\"type\":\"ping\"}\n\n")
	flusher.Flush()

	ch := s.saver.Subscribe()
	defer s.saver.Unsubscribe(ch)

	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// extractHTMLBody walks the MIME tree and returns the first text/html part,
// decoded for quoted-printable and base64 transfer encodings.
func extractHTMLBody(rawEmail string) string {
	msg, err := mail.ReadMessage(strings.NewReader(rawEmail))
	if err != nil {
		return ""
	}
	ct := msg.Header.Get("Content-Type")
	if ct == "" {
		ct = "text/plain"
	}
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil {
		return ""
	}
	return findHTML(msg.Body, mediaType, msg.Header.Get("Content-Transfer-Encoding"), params)
}

// findHTML walks MIME parts recursively, returning the first text/html body.
func findHTML(r io.Reader, mediaType, encoding string, params map[string]string) string {
	if strings.HasPrefix(mediaType, "text/html") {
		body, _ := decodePart(r, encoding)
		return body
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		mr := multipart.NewReader(r, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			ct := p.Header.Get("Content-Type")
			if ct == "" {
				ct = "text/plain"
			}
			mt, pp, err := mime.ParseMediaType(ct)
			if err != nil {
				continue
			}
			if result := findHTML(p, mt, p.Header.Get("Content-Transfer-Encoding"), pp); result != "" {
				return result
			}
		}
	}
	return ""
}

func decodePart(r io.Reader, encoding string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "quoted-printable":
		data, err := io.ReadAll(quotedprintable.NewReader(r))
		return string(data), err
	case "base64":
		raw, err := io.ReadAll(r)
		if err != nil {
			return "", err
		}
		// MIME base64 has CRLF line breaks; strip all whitespace before decoding.
		clean := strings.Map(func(r rune) rune {
			if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, string(raw))
		data, err := base64.StdEncoding.DecodeString(clean)
		return string(data), err
	default: // 7bit, 8bit, binary
		data, err := io.ReadAll(r)
		return string(data), err
	}
}

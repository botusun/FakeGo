package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	flag "github.com/spf13/pflag"

	"fakego/internal/mail"
	"fakego/internal/smtp"
	"fakego/internal/web"
)

func main() {
	var (
		port         int
		bindAddr     string
		outputDir    string
		relayDomains string
		memoryMode   bool
		webPort      int
	)

	flag.IntVarP(&port, "port", "p", 25, "SMTP port number")
	flag.StringVarP(&bindAddr, "bind-address", "a", "", "IP address or hostname to bind to (all interfaces if not specified)")
	flag.StringVarP(&outputDir, "output-dir", "o", "received-emails", "directory where received emails are saved")
	flag.StringVarP(&relayDomains, "relay-domains", "r", "", "comma-separated domains to accept relay for (all domains if not specified)")
	flag.BoolVarP(&memoryMode, "memory-mode", "m", false, "disable saving emails to disk")
	flag.IntVarP(&webPort, "web-port", "w", 1080, "port for the web UI")
	flag.Parse()

	var domains []string
	if relayDomains != "" {
		for _, d := range strings.Split(relayDomains, ",") {
			if s := strings.TrimSpace(d); s != "" {
				domains = append(domains, s)
			}
		}
	}

	if !memoryMode {
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			log.Fatalf("failed to create output directory %q: %v", outputDir, err)
		}
	}

	saver := mail.NewSaver(outputDir, memoryMode)
	saver.LoadFromDir()

	smtpAddr := fmt.Sprintf("%s:%d", bindAddr, port)
	smtpSrv := smtp.NewServer(smtpAddr, saver, domains)

	webAddr := fmt.Sprintf(":%d", webPort)
	webSrv := web.NewServer(smtpAddr, saver)

	log.Printf("SMTP server starting on %s", smtpAddr)
	if memoryMode {
		log.Println("Memory mode: emails will not be saved to disk")
	} else {
		log.Printf("Emails saved to: %s", outputDir)
	}
	if len(domains) > 0 {
		log.Printf("Relay domains: %s", strings.Join(domains, ", "))
	}
	log.Printf("Web UI: http://localhost%s", webAddr)

	go func() {
		if err := smtpSrv.ListenAndServe(); err != nil {
			log.Fatalf("SMTP server error: %v", err)
		}
	}()

	go func() {
		if err := webSrv.ListenAndServe(webAddr); err != nil {
			log.Fatalf("web server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Printf("Received %s, shutting down", sig)
	smtpSrv.Close()
}

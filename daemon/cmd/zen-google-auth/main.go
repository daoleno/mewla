// zen-google-auth is a publisher deployment artifact, never a user-daemon role.
package main

import (
	"encoding/base64"
	"github.com/daoleno/zen/daemon/googleauth"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	key, err := base64.RawURLEncoding.DecodeString(os.Getenv("ZEN_GOOGLE_RECEIPT_KEY"))
	if err != nil {
		log.Fatal("invalid receipt key")
	}
	exchange, err := googleauth.New(googleauth.Config{Origin: os.Getenv("ZEN_GOOGLE_AUTH_ORIGIN"), ClientID: os.Getenv("ZEN_GOOGLE_CLIENT_ID"), ClientSecret: os.Getenv("ZEN_GOOGLE_CLIENT_SECRET"), ReceiptKey: key})
	if err != nil {
		log.Fatal(err)
	}
	address := os.Getenv("ZEN_GOOGLE_AUTH_LISTEN")
	if address == "" {
		address = "127.0.0.1:8098"
	}
	server := http.Server{Addr: address, Handler: exchange, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 25 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	log.Fatal(server.ListenAndServe())
}

package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	target := flag.String("url", "http://127.0.0.1:8080/ready", "health endpoint URL")
	flag.Parse()

	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(*target)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		fmt.Fprintf(os.Stderr, "health endpoint returned HTTP %d\n", response.StatusCode)
		os.Exit(1)
	}
}

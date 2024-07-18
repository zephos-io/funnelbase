package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
)

func handler(w http.ResponseWriter, r *http.Request) {
	log.Println("got /test/api")
	_, err := io.WriteString(w, "OK")
	if err != nil {
		return
	}
}

func main() {
	http.HandleFunc("/v1/test", handler)

	err := http.ListenAndServe(":3333", nil)
	if errors.Is(err, http.ErrServerClosed) {
		fmt.Printf("server closed\n")
	} else if err != nil {
		fmt.Printf("error starting server: %v\n", err)
		os.Exit(1)
	}
}

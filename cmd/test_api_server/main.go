package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
)

var count = 0

func handler(w http.ResponseWriter, r *http.Request) {
	count++

	log.Println("got /test/api")

	//if count%20 == 0 {
	//	w.Header().Set("Retry-After", "30")
	//	w.WriteHeader(429)
	//	return
	//}

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

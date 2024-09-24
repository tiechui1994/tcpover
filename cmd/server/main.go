package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
)

func main() {
	version := flag.String("v", "", "version")
	pwd := flag.String("p", "", "install dir")
	flag.Parse()

	http.HandleFunc("/", func(res http.ResponseWriter, req *http.Request) {
		fmt.Fprintln(res, fmt.Sprintf(`{"pwd":"%v", "version":"%v"}`, *version, *pwd))
	})

	fmt.Println("listening...")

	err := http.ListenAndServe(":"+os.Getenv("PORT"), nil)
	if err != nil {
		panic(err)
	}
}

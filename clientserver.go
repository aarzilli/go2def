package main

import (
	"net"
	"log"
	"fmt"
	"os"
)

const Address = "/tmp/go2def"

func startServer() {
	lst, err := net.Listen("unix", Address)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	if verbose {
		log.Printf("daemon started")
	}
	for {
		conn, err := lst.Accept()
		if err != nil {
			log.Fatalf("accept: %v", err)
		}
		close := serve(conn)
		if close {
			lst.Close()
			break
		}
	}
}

func connect() net.Conn {
	conn, err := net.Dial("unix", Address)
	if err != nil {
		fmt.Printf("dial: %v\n", err)
		os.Exit(1)
	}
	return conn
}

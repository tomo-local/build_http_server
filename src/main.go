package main

import (
	"fmt"
	"log"
	"net"
)

func main() {
	ln, err := net.Listen("tcp", ":8080")
	if err != nil {
		log.Fatal(err)
	}

	defer ln.Close()

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Fatal(err)
		}

		handleConnection(conn)
	}

}

func handleConnection(conn net.Conn) {
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println(string(buf[:n]))

	if _, err := conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 12\r\n\r\nHello World!")); err != nil {
		fmt.Println(err)
		return
	}
}

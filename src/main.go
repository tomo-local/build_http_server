package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
)

type Request struct {
	Method  string
	Path    string
	Version string
	Headers map[string]string
	Body    []byte
}

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

		go handleConnection(conn)
	}

}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	req, err := parseRequest(conn)
	if err != nil {
		fmt.Println(err)
	}

	fmt.Printf("Method: %s, Path: %s, Version: %s\n", req.Method, req.Path, req.Version)
	fmt.Printf("Headers: %v\n", req.Headers)

	body := "Hello, World!"
	response := "HTTP/1.1 200 OK\r\nContent-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body
	conn.Write([]byte(response))
}

func parseRequest(conn net.Conn) (*Request, error) {
	render := bufio.NewReader(conn)

	line, err := render.ReadString('\n')
	if err != nil {
		return nil, err
	}
	parts := strings.Fields(strings.TrimSpace(line))

	req := &Request{
		Method:  parts[0],
		Path:    parts[1],
		Version: parts[2],
		Headers: make(map[string]string),
	}

	for {
		line, err := render.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)

		if line == "" {
			break
		}

		idx := strings.Index(line, ":")
		if idx == -1 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		req.Headers[key] = value
	}

	if cl, ok := req.Headers["Content-Length"]; ok {
		length, err := strconv.Atoi(cl)
		if err != nil {
			return nil, err
		}
		req.Body = make([]byte, length)
		_, err = render.Read(req.Body)
		if err != nil {
			return nil, err
		}
	}

	return req, nil
}

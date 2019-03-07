package main

import (
	"fileTransferring/shared"
	"fmt"
	"net"
	"time"
)

func main() {
	ServerAddr, err := net.ResolveUDPAddr("udp", ":8274")
	shared.ErrorValidation(err)
	conn, err := net.ListenUDP("udp", ServerAddr)
	shared.ErrorValidation(err)

	defer conn.Close()

	message := make([]byte, 1024)

	fmt.Println("Server started...")

	time.After()
}

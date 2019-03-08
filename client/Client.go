package main

import (
	"bufio"
	"fileTransferring/shared"
	"fmt"
	"github.com/pkg/errors"
	"log"
	"math"
	"net"
	"os"
	"path/filepath"
	"tideWatchAPI/utils"
)

func main() {
	var serverAddress string

	fmt.Print("Server address: ")
	_, _ = fmt.Scanf("%s", &serverAddress)

	service := serverAddress + ":8274"

	remoteAddr, err := net.ResolveUDPAddr("udp", service)

	conn, connError := net.DialUDP("udp", nil, remoteAddr)
	shared.ErrorValidation(connError)

	var filePath string
	fmt.Print("Enter full file path: ")
	_, _ = fmt.Scanf("%s", &filePath)

	file, fileError := os.Open(filePath)
	shared.ErrorValidation(fileError)

	sendWRQPacket(conn, filepath.Base(file.Name()))
	_ = receivePacket(conn, [] byte{0, 0})

	if err != nil {
		fmt.Println("Cannot send packets...")
	} else {
		fmt.Println("Can start sending packets...")
	}

	readFile(conn, file)
}

func readFile(conn *net.UDPConn, file *os.File) {
	scanner := bufio.NewScanner(file)

	var message = make([]byte, 0, 512)
	var currentPacket int
	for scanner.Scan() {
		for _, character := range scanner.Bytes() {
			addToArray(conn, &message, character, &currentPacket)
		}
		addToArray(conn, &message, '\n', &currentPacket)
	}
	sendDataPacket(conn, &message, &currentPacket)
	fmt.Println("Done reading and sending file...")
}

func addToArray(conn *net.UDPConn, message *[] byte, nextByteToAppend byte, currentPacket *int) {
	if checkMessageLength(message) {
		sendDataPacket(conn, message, currentPacket)
		*message = make([]byte, 0, 512)
	}
	*message = append(*message, nextByteToAppend)
}

func checkMessageLength(message *[] byte) bool {
	if len(*message) == 512 {
		return true
	}
	return false
}

func sendWRQPacket(conn *net.UDPConn, fileName string) {
	wPacket := shared.CreateRRQWRQPacket(false)
	wPacket.Filename = fileName

	// TODO: Send packet
	//fmt.Println(shared.CreateRRQWRQPacketByteArray(wPacket))
	go conn.Write(shared.CreateRRQWRQPacketByteArray(wPacket))
	// TODO: Receive packet (and handle error)
	//_ = receivePacket(conn, []byte{0, 0})
}

func sendDataPacket(conn *net.UDPConn, data *[] byte, currentPacket *int) {
	dataPacket := shared.CreateDataPacket()
	dataPacket.BlockNumber = createBlockNumber(currentPacket)
	dataPacket.Data = *data

	// TODO: Send packet
	go conn.Write(shared.CreateDataPacketByteArray(dataPacket))
	// TODO: Receive packet (and handle error)
	_ = receivePacket(conn, dataPacket.BlockNumber)
}

func receivePacket(conn *net.UDPConn, blockNumber [] byte) error {
	// TODO: When an error occurs here, send an error packet back (possibly if it is the client)

	data := make([]byte, 1024)
	_, _, err := conn.ReadFromUDP(data)
	data = shared.StripOffExtraneousBytes(data)
	utils.ErrorCheck(err)
	t := shared.DeterminePacketType(data)

	switch t {
	case 4:
		ack, _ := shared.ReadACKPacket(data)
		if shared.CheckByteArrayEquality(ack.BlockNumber, blockNumber) {
			return nil
		}
		// TODO: Do something with this on top of just throwing an error
		return errors.New("Block numbers do not match...")
	case 5:
		e, _ := shared.ReadErrorPacket(data)
		return errors.New(fmt.Sprintf("Error code: %d\nError Message: %s", e.ErrorCode[1], e.ErrorMessage))
	default:
		log.Fatal("Client can only read Opcodes of 4 and 5...not: ", t)
	}

	return nil
}

func createBlockNumber(currentPacketNumber *int) [] byte {
	*currentPacketNumber++
	if *currentPacketNumber < 256 {
		return [] byte{0, byte(*currentPacketNumber)}
	}
	leadingByte := math.Floor(float64(*currentPacketNumber / 256))
	return [] byte{byte(leadingByte), byte(*currentPacketNumber % 256)}

}

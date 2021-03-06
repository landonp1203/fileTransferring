package main

import (
	"fileTransferring/shared"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

var filename string // this server will only handle one connection at a time, so we just set this variable each time a new WRQ packet comes int

// Sliding window data
var sw bool
var lastSeenBlockNumber = [] byte{0, 0}
var windowSize = 1 // packets must be in order
var finishedTransferring bool
var addrOfReceiver *net.UDPAddr

var startTime int64
var endTime int64
var rttCalculated bool

var ipv6, _, _ = shared.GetCMDArgs(os.Args, false)

func main() {
	var ServerAddr *net.UDPAddr
	var err error

	if ipv6 {
		ServerAddr, err = net.ResolveUDPAddr("udp6", shared.PORT)
	} else {
		ServerAddr, err = net.ResolveUDPAddr("udp4", shared.PORT)
	}

	shared.ErrorValidation(err)
	conn, err := net.ListenUDP("udp", ServerAddr)
	shared.ErrorValidation(err)

	defer conn.Close()

	fmt.Println("Server started...")

	displayExternalIP()

	for {
		if sw {
			readSlidingWindow(conn)
		} else {
			readPacket(conn)
		}
	}
}

func readSlidingWindow(conn *net.UDPConn) {
	data := make([]byte, 516)

	if !rttCalculated {
		_ = conn.SetReadDeadline(time.Now().Add(time.Millisecond * 60000))
	} else {
		var rtt = (endTime - startTime) * 2 // multiply by 2 so that we have some room
		_ = conn.SetReadDeadline(time.Now().Add(time.Nanosecond * time.Duration(rtt)))
	}
	amountOfBytes, addr, err := conn.ReadFromUDP(data)

	if err != nil {
		fmt.Println("Timed out...")
		ack := shared.CreateACKPacket()
		ack.BlockNumber = lastSeenBlockNumber
		sendPacketToClient(conn, addrOfReceiver, ack.ByteArray())

		return
	}

	data = data[:amountOfBytes]

	switch data[1] { // check the opcode of the packet
	case 3:
		d, _ := shared.ReadDataPacket(data)

		if !rttCalculated {
			if shared.BlockNumberChecker(d.BlockNumber, [] byte{0, 1}) {
				endTime = time.Now().UnixNano()
				rttCalculated = true
			}
		}

		validWindow := checkSequentialBlockNumbers(lastSeenBlockNumber, d.BlockNumber)

		if validWindow { // packet is in order
			lastSeenBlockNumber = d.BlockNumber
			_, _ = writeToFile(filename, d.Data)
			ack := shared.CreateACKPacket()
			ack.BlockNumber = lastSeenBlockNumber
			sendPacketToClient(conn, addr, ack.ByteArray())

			if checkEndOfTransfer(d.Data) {
				_, _ = writeToFile(filename, d.Data)

				endSlidingWindow := shared.CreateSlidingWindowPacket()
				sendPacketToClient(conn, addr, endSlidingWindow.ByteArray())
				os.Exit(0)
			}
		}
	default:
		sendPacketToClient(conn, addr, createErrorPacket(shared.Error0, fmt.Sprintf("Server can only read Opcode 3 in Sliding Window Mode...not: %d", data[1])))
	}
}

func checkSequentialBlockNumbers(lastSeen [] byte, receivedBlockNumber [] byte) bool {
	if lastSeen[0] == receivedBlockNumber[0] { // leading bytes are the same, now we need to check trailing
		if lastSeen[1]+1 == receivedBlockNumber[1] {
			return true
		}
	} else { // leading bytes are different, need to check them now
		if lastSeen[0]+1 == receivedBlockNumber[0] {
			if lastSeen[1]+1 == receivedBlockNumber[1] {
				return true
			}
			return false
		}
		return false
	}

	return false
}

// Reads the incoming packet and performs operations based on the packet received
func readPacket(conn *net.UDPConn) {
	data := make([]byte, 516)

	amountOfBytes, addr, err := conn.ReadFromUDP(data)
	addrOfReceiver = addr
	shared.ErrorValidation(err)
	data = data[:amountOfBytes]
	ack := shared.CreateACKPacket()

	switch data[1] { // check the opcode of the packet
	case 2:
		fmt.Println("WRQ packet has been received...")
		w, _ := shared.ReadRRQWRQPacket(data)
		filename = w.Filename

		if w.Options != nil {
			ack.IsOACK = true
			ack.Opcode = [] byte{0, 6}
			ack.Options = parseOptions(w.Options)
		}

		if !ack.IsOACK {
			if strings.ToLower(w.Mode) != "octet" {
				sendPacketToClient(conn, addr, createErrorPacket(shared.Error0, "This server only supports octet mode, not: "+w.Mode))
				return
			}
		}

		errorPacket, hasError := checkFileExists(filename)

		if hasError {
			sendPacketToClient(conn, addr, errorPacket)
			return
		} else {
			ack.BlockNumber = []byte{0, 0}
			startTime = time.Now().UnixNano()
		}
	case 3:
		d, _ := shared.ReadDataPacket(data)
		errorPacket, hasError := writeToFile(filename, d.Data)
		if hasError {
			sendPacketToClient(conn, addr, errorPacket)
			os.Exit(0)
			return
		} else {
			checkEndOfTransfer(d.Data)
			ack.BlockNumber = d.BlockNumber
		}
	default:
		sendPacketToClient(conn, addr, createErrorPacket(shared.Error0, fmt.Sprintf("Server only supports Opcodes of 2,3, 5, and 6...not: %d", data[1])))
	}

	sendPacketToClient(conn, addr, ack.ByteArray())

	if finishedTransferring {
		os.Exit(0)
	}
}

// Sends the packet to the client
func sendPacketToClient(conn *net.UDPConn, addr *net.UDPAddr, data [] byte) {
	_, _ = conn.WriteToUDP(data, addr)
}

// Checks if a file exists and returns an packetLost if so
func checkFileExists(fileName string) (ePacket [] byte, hasError bool) {
	_, err := os.Stat(fileName)

	if os.IsNotExist(err) {
		return nil, false
	}

	return createErrorPacket(shared.Error6, shared.Error6Message), true
}

// Writes to a file and returns an packetLost if it cannot write to that specific file
func writeToFile(fileName string, data []byte) (eData [] byte, hasError bool) {
	f, err := os.OpenFile(fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)

	if err != nil {
		return createErrorPacket(shared.Error0, err.Error()), true
	}
	if _, err := f.Write(data); err != nil {
		return createErrorPacket(shared.Error0, err.Error()), true
	}
	if err := f.Close(); err != nil {
		return createErrorPacket(shared.Error0, err.Error()), true
	}

	return nil, false
}

// Checks the end of the file transfer based on the data portion of the packet
func checkEndOfTransfer(data [] byte) bool {
	if len(data) < 512 { // although the packet is 516, 512 is the max for the data portion...anything smaller and we know it is the end of the file
		fmt.Println("File has been fully transferred")
		finishedTransferring = true
		return true
	}

	return false
}

// Helper to create an packetLost packet
func createErrorPacket(errorCode [] byte, errorMessage string) [] byte {
	ePacket := shared.CreateErrorPacket(errorCode, errorMessage)
	return ePacket.ByteArray()
}

// Displays the external IP of the running server
func displayExternalIP() {
	resp, err := http.Get("http://myexternalip.com/raw")

	defer resp.Body.Close()

	shared.ErrorValidation(err)
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	shared.ErrorValidation(err)
	bodyString := string(bodyBytes)
	fmt.Println("External IP: " + bodyString)
}

func parseOptions(oackPacketOptions map[string]string) map[string]string {
	var supportedOptions = make(map[string]string)

	if oackPacketOptions["sendingMode"] == "" {
		return nil
	}
	supportedOptions["sendingMode"] = oackPacketOptions["sendingMode"]
	sw = true

	return supportedOptions
}

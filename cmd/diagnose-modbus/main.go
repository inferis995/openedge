package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"time"

	"github.com/ralph/industrial-edge-middleware/internal/modbus"
)

func main() {
	fmt.Println("Starting Modbus Diagnostic Tool...")
	fmt.Println("Target: 127.0.0.1:502 (Modbus TCP)")

	// Configure client
	config := modbus.Config{
		Host:    "127.0.0.1",
		Port:    502,
		SlaveID: 1,
		Timeout: 2 * time.Second,
	}

	client := modbus.NewClient(config)

	// Connect
	fmt.Println("Connecting...")
	if err := client.Connect(); err != nil {
		log.Fatalf("Failed to connect: %v\nCheck if the PLC simulator is running and accessible.", err)
	}
	defer client.Disconnect()
	fmt.Println("Connected successfully!")

	// Scan standard Holding Registers (40001-40020)
	// Internally ReadHoldingRegisters uses 0-based offset usually,
	// but let's check what ReadHoldingRegisters logic expects.
	// In the modbus library, ReadHoldingRegister(address) usually takes the raw 0-based offset.
	// Standard Modbus: 40001 is offset 0.

	startAddr := uint16(0) // 40001
	count := uint16(20)

	fmt.Printf("\nScanning Holding Registers (40001 - %d)...\n", 40001+count-1)

	// Read raw bytes
	data, err := client.ReadHoldingRegisters(startAddr, count)
	if err != nil {
		log.Fatalf("Error reading registers: %v", err)
	}

	fmt.Println("\nOffset | Register | Value (UINT) | Value (Hex)")
	fmt.Println("-------|----------|--------------|------------")

	for i := 0; i < int(count); i++ {
		byteOffset := i * 2
		if byteOffset+2 > len(data) {
			break
		}
		val := binary.BigEndian.Uint16(data[byteOffset : byteOffset+2])
		fmt.Printf("%6d |    %5d | %12d | 0x%04X\n", i, 40001+i, val, val)
	}
}

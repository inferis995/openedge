package main

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	// Generate hash for "supersecret"
	hash, _ := bcrypt.GenerateFromPassword([]byte("supersecret"), bcrypt.DefaultCost)
	fmt.Println(string(hash))
}

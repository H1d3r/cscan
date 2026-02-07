package main

import (
	"fmt"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	password := "123456"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Println("Password:", password)
	fmt.Println("Bcrypt Hash:", string(hash))

	// 验证
	err = bcrypt.CompareHashAndPassword(hash, []byte(password))
	if err == nil {
		fmt.Println("Verification: OK")
	} else {
		fmt.Println("Verification: FAILED")
	}
}

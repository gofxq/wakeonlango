package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
)

func main() {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("private_key_base64=", base64.StdEncoding.EncodeToString(priv))
	fmt.Println("public_key_base64=", base64.StdEncoding.EncodeToString(pub))
}
